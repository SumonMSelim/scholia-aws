package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sumonmselim/scholia-aws/internal/answer"
	"github.com/sumonmselim/scholia-aws/internal/domain"
	"github.com/sumonmselim/scholia-aws/internal/locator"
	"github.com/sumonmselim/scholia-aws/internal/model"
)

// streamAnswer runs one question through the SSE writer the chat route uses.
func streamAnswer(w http.ResponseWriter, runner Answerer, question string) {
	s := &server{opts: Options{Logger: slog.New(slog.DiscardHandler)}}
	r := httptest.NewRequest(http.MethodPost, "/api/chats/h1/messages", nil)
	s.stream(w, r, runner, answer.Request{CourseID: "nets", Question: question})
}

func TestAnswerDeltasPrecedeCitations(t *testing.T) {
	parent := domain.Chunk{
		ID: "p-route", CourseID: "nets", SourceID: "s-nets",
		Text: "Routing picks the next hop.",
		Locators: []locator.Locator{
			{Kind: locator.KindSlide, Slide: 4},
			{Kind: locator.KindPage, Page: 2, BBox: &locator.BBox{X0: 1, Y0: 2, X1: 3, Y1: 4}},
			{Kind: locator.KindTime, StartMS: 0, EndMS: 900},
			{Kind: locator.KindText, Start: 1, End: 8},
		},
	}
	rec := httptest.NewRecorder()
	streamAnswer(rec, &answer.Service{Search: fixedSearch{chunks: []domain.Chunk{parent}}, Model: model.Mock{}}, "next hop")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q", ct)
	}
	events := parseSSE(t, rec.Body.String())
	if len(events) < 2 || events[len(events)-1].name != "citation" {
		t.Fatalf("events = %+v", events)
	}
	for _, ev := range events[:len(events)-1] {
		if ev.name != "delta" {
			t.Fatalf("event %q arrived before the citation", ev.name)
		}
	}
	var cited citationEvent
	if err := json.Unmarshal([]byte(events[len(events)-1].data), &cited); err != nil {
		t.Fatal(err)
	}
	if len(cited.Citations) != 1 || cited.Citations[0].ChunkID != "p-route" {
		t.Fatalf("citations = %+v", cited.Citations)
	}
	got := cited.Citations[0].Locators
	if len(got) != 4 || got[0].Slide != 4 || got[1].Page != 2 || got[2].EndMS != 900 || got[3].End != 8 {
		t.Fatalf("locators = %+v", got)
	}
}

func TestAnswerRefusesWhenNothingMatches(t *testing.T) {
	rec := httptest.NewRecorder()
	streamAnswer(rec, &answer.Service{Search: fixedSearch{}, Model: model.Mock{}}, "nothing")
	events := parseSSE(t, rec.Body.String())
	if len(events) != 1 || events[0].name != "refusal" {
		t.Fatalf("events = %+v", events)
	}
	if strings.Contains(rec.Body.String(), "event: delta") || strings.Contains(rec.Body.String(), "event: citation") {
		t.Fatalf("refusal included a model answer: %s", rec.Body.String())
	}
	var body refusalEvent
	if err := json.Unmarshal([]byte(events[0].data), &body); err != nil {
		t.Fatal(err)
	}
	if body.Text != answer.RefusalText || body.Reason != domain.RefusalNoMaterial {
		t.Fatalf("refusal = %+v", body)
	}
}

type sseEvent struct {
	name string
	data string
}

func parseSSE(t *testing.T, raw string) []sseEvent {
	t.Helper()
	var out []sseEvent
	for _, block := range strings.Split(raw, "\n\n") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		var ev sseEvent
		for _, line := range strings.Split(block, "\n") {
			switch {
			case strings.HasPrefix(line, "event: "):
				ev.name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				ev.data = strings.TrimPrefix(line, "data: ")
			}
		}
		out = append(out, ev)
	}
	return out
}

func TestTutorCitesTheLectureWithoutASolution(t *testing.T) {
	lecture := domain.Chunk{
		ID: "p-hop", CourseID: "nets", SourceID: "s-lec",
		Text: "Packets are forwarded hop by hop.",
		Locators: []locator.Locator{
			{Kind: locator.KindTime, StartMS: 1200, EndMS: 4000},
			{Kind: locator.KindSlide, Slide: 3},
		},
	}
	tutor := answer.TutorMode{Bound: answer.Bound{Service: &answer.Service{
		Search: fixedSearch{chunks: []domain.Chunk{lecture}},
		Model:  model.Mock{},
	}}}
	rec := httptest.NewRecorder()
	streamAnswer(rec, tutor, "How does forwarding work?")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "worked final answer") || strings.Contains(body, "assignment solution") {
		t.Fatalf("tutor wrote a solution: %s", body)
	}
	events := parseSSE(t, body)
	var explained strings.Builder
	for _, ev := range events {
		if ev.name != "delta" {
			continue
		}
		var delta deltaEvent
		if err := json.Unmarshal([]byte(ev.data), &delta); err != nil {
			t.Fatal(err)
		}
		explained.WriteString(delta.Text)
	}
	if !strings.Contains(explained.String(), lecture.Text) {
		t.Fatalf("missing explanation: %s", explained.String())
	}
	var cited citationEvent
	if err := json.Unmarshal([]byte(events[len(events)-1].data), &cited); err != nil {
		t.Fatal(err)
	}
	locs := cited.Citations[0].Locators
	if locs[0].Kind != locator.KindTime || locs[1].Slide != 3 {
		t.Fatalf("locators = %+v", locs)
	}
}

type fixedSearch struct {
	chunks []domain.Chunk
}

func (f fixedSearch) Search(context.Context, string, string, string, int) ([]domain.Chunk, error) {
	return f.chunks, nil
}

// noFlushWriter is a ResponseWriter without Flush, like the Lambda function URL writer.
type noFlushWriter struct {
	header http.Header
	code   int
	body   strings.Builder
}

func (w *noFlushWriter) Header() http.Header         { return w.header }
func (w *noFlushWriter) Write(p []byte) (int, error) { return w.body.Write(p) }
func (w *noFlushWriter) WriteHeader(code int)        { w.code = code }

func TestAnswerStreamsWithoutFlush(t *testing.T) {
	parent := domain.Chunk{ID: "p-route", CourseID: "nets", SourceID: "s-nets", Text: "Routing picks the next hop."}
	w := &noFlushWriter{header: http.Header{}}
	streamAnswer(w, &answer.Service{Search: fixedSearch{chunks: []domain.Chunk{parent}}, Model: model.Mock{}}, "next hop")

	if w.code != http.StatusOK {
		t.Fatalf("status = %d body = %s", w.code, w.body.String())
	}
	events := parseSSE(t, w.body.String())
	if len(events) < 2 || events[len(events)-1].name != "citation" {
		t.Fatalf("events = %+v", events)
	}
}
