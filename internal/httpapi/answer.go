package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/sumonmselim/scholia-aws/internal/answer"
	"github.com/sumonmselim/scholia-aws/internal/domain"
	"github.com/sumonmselim/scholia-aws/internal/killswitch"
	"github.com/sumonmselim/scholia-aws/internal/limit"
	"github.com/sumonmselim/scholia-aws/internal/store"
)

const maxQuestionBytes = 8192

type deltaEvent struct {
	Text string `json:"text"`
}

// citationEvent closes a grounded answer. Web is always an array.
type citationEvent struct {
	Citations []answer.Citation `json:"citations"`
	Web       []webBody         `json:"web"`
}

type webBody struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

// refusalEvent replaces the answer. Reason is no_material, off_topic or unsafe.
type refusalEvent struct {
	Text   string `json:"text"`
	Reason string `json:"reason"`
}

// reply is what a finished stream produced. ok is false when the answer failed.
type reply struct {
	text    string
	outcome answer.Outcome
	ok      bool
}

// stream writes one answer as server-sent events: deltas, then a citation or a refusal.
// A failure before the first byte is a JSON error; after it, an error event.
func (s *server) stream(w http.ResponseWriter, r *http.Request, runner Answerer, req answer.Request) reply {
	started := false
	start := func() error {
		if started {
			return nil
		}
		h := w.Header()
		h.Set("Content-Type", "text/event-stream; charset=utf-8")
		h.Set("Cache-Control", "no-store")
		h.Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		started = true
		return flush(w)
	}

	var text strings.Builder
	out, err := runner.Answer(r.Context(), req, func(delta string) error {
		if err := start(); err != nil {
			return err
		}
		text.WriteString(delta)
		return writeEvent(w, "delta", deltaEvent{Text: delta})
	})
	if err != nil {
		if started {
			s.opts.Logger.ErrorContext(r.Context(), "answer", slog.String("err", err.Error()))
			_ = writeEvent(w, "error", errorDetail{Code: "internal", Message: "answer failed"})
			return reply{}
		}
		if errors.Is(err, killswitch.ErrPaused) {
			writeError(w, http.StatusServiceUnavailable, "paused", pausedMessage)
			return reply{}
		}
		s.opts.Logger.ErrorContext(r.Context(), "answer", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal", "could not answer")
		return reply{}
	}
	if err := start(); err != nil {
		return reply{}
	}
	if out.Refused {
		if out.Text == "" {
			out.Text = answer.RefusalText
		}
		if out.Reason == "" {
			out.Reason = domain.RefusalNoMaterial
		}
		s.opts.Logger.InfoContext(r.Context(), "refused", slog.String("reason", out.Reason), slog.String("detail", out.Detail), slog.String("task", string(out.Task)))
		_ = writeEvent(w, "refusal", refusalEvent{Text: out.Text, Reason: out.Reason})
	} else {
		citations := out.Citations
		if citations == nil {
			citations = []answer.Citation{}
		}
		_ = writeEvent(w, "citation", citationEvent{Citations: citations, Web: webJSON(out.Web)})
	}
	return reply{text: text.String(), outcome: out, ok: true}
}

func webJSON(rows []domain.WebResult) []webBody {
	out := make([]webBody, len(rows))
	for i, row := range rows {
		out[i] = webBody{Title: row.Title, URL: row.URL}
	}
	return out
}

func writeEvent(w http.ResponseWriter, name string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, raw); err != nil {
		return err
	}
	return flush(w)
}

// flush sends buffered events now. The Lambda function URL writer has no Flush, but it
// writes into a pipe that streams each event as it is written, so that is not an error.
func flush(w http.ResponseWriter) error {
	if err := http.NewResponseController(w).Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	return nil
}

// allowUse checks course visibility when sign-in is on, then the rate and spend gates.
// fail is the message when the course record itself cannot be read.
// It writes the error response and returns false so the handler can stop before any body byte.
func (s *server) allowUse(w http.ResponseWriter, r *http.Request, courseID, fail string) bool {
	who := s.caller(r)
	if who.enforced {
		if s.opts.Sources == nil {
			writeError(w, http.StatusServiceUnavailable, "unavailable", "courses are not configured")
			return false
		}
		course, err := s.opts.Sources.GetCourse(r.Context(), courseID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not_found", "course not found")
				return false
			}
			s.opts.Logger.ErrorContext(r.Context(), "get course", slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal", fail)
			return false
		}
		if !who.canRead(course) {
			deny(w, who, false)
			return false
		}
	}
	if err := s.opts.Gate.Allow(clientIP(r), s.opts.Now(), who.anonymous()); err != nil {
		msg := limit.ErrRateLimited.Error()
		if errors.Is(err, limit.ErrSpendCap) {
			msg = err.Error()
		}
		writeError(w, http.StatusTooManyRequests, "rate_limited", msg)
		return false
	}
	return s.charge(w, r, who, domain.UsageMessages)
}
