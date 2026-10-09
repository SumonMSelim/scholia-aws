package ingest

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/sumonmselim/scholia-aws/internal/domain"
	"github.com/sumonmselim/scholia-aws/internal/locator"
	"github.com/sumonmselim/scholia-aws/internal/pdfdoc"
	"github.com/sumonmselim/scholia-aws/internal/pptx"
	"github.com/sumonmselim/scholia-aws/internal/store"
	"github.com/sumonmselim/scholia-aws/internal/transcript"
	"github.com/sumonmselim/scholia-aws/internal/vision"
)

type memStore struct {
	src    domain.Source
	err    error
	chunks []domain.Chunk
}

func (m *memStore) GetSource(context.Context, string, string) (domain.Source, error) {
	if m.err != nil {
		return domain.Source{}, m.err
	}
	return m.src, nil
}

func (m *memStore) PutSource(_ context.Context, src domain.Source) error {
	m.src = src
	return nil
}

func (m *memStore) PutChunk(_ context.Context, chunk domain.Chunk) error {
	m.chunks = append(m.chunks, chunk)
	return nil
}

type memObjects struct {
	size    int64
	prefix  []byte
	body    []byte
	statErr error
	getErr  error
	putKey  string
	putTag  string
	putBody []byte
	puts    map[string][]byte
	deleted []string
}

func (m *memObjects) Delete(_ context.Context, _, key string) error {
	m.deleted = append(m.deleted, key)
	return nil
}

func (m *memObjects) Stat(context.Context, string, string) (int64, error) {
	return m.size, m.statErr
}

func (m *memObjects) Prefix(context.Context, string, string, int) ([]byte, error) {
	return m.prefix, nil
}

func (m *memObjects) Get(context.Context, string, string) ([]byte, error) {
	return m.body, m.getErr
}

func (m *memObjects) Put(_ context.Context, _, key, _, tagging string, body []byte) error {
	m.putKey = key
	m.putTag = tagging
	m.putBody = append([]byte(nil), body...)
	if m.puts == nil {
		m.puts = map[string][]byte{}
	}
	m.puts[key] = append([]byte(nil), body...)
	return nil
}

func TestHandleUpload(t *testing.T) {
	key := ObjectKey("c1", "s1", "notes.txt")
	queued := domain.Source{ID: "s1", CourseID: "c1", Name: "notes.txt", ContentType: "text/plain", Status: domain.SourceQueued}

	t.Run("ready", func(t *testing.T) {
		mem := &memStore{src: queued}
		p := &Processor{Store: mem, Objects: &memObjects{size: 8, prefix: []byte("hello\n")}}
		if err := p.HandleUpload(context.Background(), "b", key); err != nil {
			t.Fatal(err)
		}
		if mem.src.Status != domain.SourceReady {
			t.Fatalf("status %s", mem.src.Status)
		}
	})

	t.Run("magic mismatch", func(t *testing.T) {
		mem := &memStore{src: queued}
		p := &Processor{Store: mem, Objects: &memObjects{size: 5, prefix: []byte("%PDF-")}}
		if err := p.HandleUpload(context.Background(), "b", key); err != nil {
			t.Fatal(err)
		}
		if mem.src.Status != domain.SourceFailed || mem.src.FailureReason == "" {
			t.Fatalf("source %+v", mem.src)
		}
	})

	t.Run("oversize", func(t *testing.T) {
		mem := &memStore{src: queued}
		objects := &memObjects{size: MaxUploadBytes + 1, prefix: []byte("%PDF")}
		p := &Processor{Store: mem, Objects: objects}
		if err := p.HandleUpload(context.Background(), "b", key); err != nil {
			t.Fatal(err)
		}
		if mem.src.Status != domain.SourceFailed || mem.src.FailureReason != reasonSize {
			t.Fatalf("source %+v", mem.src)
		}
		// A refused object is removed rather than left in the bucket.
		if len(objects.deleted) != 1 || objects.deleted[0] != key {
			t.Fatalf("deleted %v", objects.deleted)
		}
	})

	t.Run("already ready", func(t *testing.T) {
		done := queued
		done.Status = domain.SourceReady
		mem := &memStore{src: done}
		p := &Processor{Store: mem, Objects: &memObjects{statErr: errors.New("should not stat")}}
		if err := p.HandleUpload(context.Background(), "b", key); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("missing source", func(t *testing.T) {
		p := &Processor{Store: &memStore{err: store.ErrNotFound}, Objects: &memObjects{}}
		if err := p.HandleUpload(context.Background(), "b", key); err == nil {
			t.Fatal("missing source was ignored")
		}
	})

	t.Run("foreign key", func(t *testing.T) {
		p := &Processor{Store: &memStore{err: errors.New("should not read")}, Objects: &memObjects{}}
		if err := p.HandleUpload(context.Background(), "b", AttachmentKey("h1", "a1", "hw.pdf")); err != nil {
			t.Fatalf("chat attachment: %v", err)
		}
		if err := p.HandleUpload(context.Background(), "b", "elsewhere"); err != nil {
			t.Fatal(err)
		}
	})
}

func TestHandleMessage(t *testing.T) {
	mem := &memStore{src: domain.Source{
		ID: "s1", CourseID: "c1", Name: "notes.txt", ContentType: "text/plain", Status: domain.SourceQueued,
	}}
	p := &Processor{Store: mem, Objects: &memObjects{size: 8, prefix: []byte("hello\n")}}
	body := `{"Records":[{"s3":{"bucket":{"name":"b"},"object":{"key":"courses/c1/sources/s1/notes.txt"}}}]}`
	if err := p.HandleMessage(context.Background(), body); err != nil {
		t.Fatal(err)
	}
	if mem.src.Status != domain.SourceReady {
		t.Fatalf("status %s", mem.src.Status)
	}
	if err := p.HandleMessage(context.Background(), `{"Records":[]}`); err == nil {
		t.Fatal("empty event was accepted")
	}
	if err := p.HandleMessage(context.Background(), `{`); err == nil {
		t.Fatal("broken json was accepted")
	}
	// The body S3 sends once when the notification is created; retrying it only fills the DLQ.
	test := `{"Service":"Amazon S3","Event":"s3:TestEvent","Time":"2026-10-01T00:00:00.000Z","Bucket":"b","RequestId":"r","HostId":"h"}`
	if err := p.HandleMessage(context.Background(), test); err != nil {
		t.Fatalf("test event: %v", err)
	}
}

// TestImportTranscribeJSON imports an Amazon Transcribe result. A guest source
// (expires set) tags its derived JSON and stamps its chunks with the same expiry.
func TestImportTranscribeJSON(t *testing.T) {
	body := []byte(`{"results":{"items":[
		{"start_time":"0.0","end_time":"0.3","type":"pronunciation","alternatives":[{"content":"Hello"}]},
		{"start_time":"0.4","end_time":"0.9","type":"pronunciation","alternatives":[{"content":"world"}]},
		{"type":"punctuation","alternatives":[{"content":"."}]}
	],"speaker_labels":{"segments":[{"items":[
		{"start_time":"0.0","speaker_label":"spk_0"},
		{"start_time":"0.4","speaker_label":"spk_0"}
	]}]}}}`)
	for _, expires := range []int64{0, 1790000000} {
		mem := &memStore{src: domain.Source{
			ID: "s1", CourseID: "c1", Name: "lecture.json", ContentType: "application/json", Status: domain.SourceQueued, ExpiresAt: expires,
		}}
		objects := &memObjects{size: int64(len(body)), prefix: body, body: body}
		p := &Processor{Store: mem, Objects: objects, UploadsBucket: "uploads"}
		if err := p.HandleUpload(context.Background(), "uploads", ObjectKey("c1", "s1", "lecture.json")); err != nil {
			t.Fatal(err)
		}
		if mem.src.Status != domain.SourceReady {
			t.Fatalf("status %s reason %s", mem.src.Status, mem.src.FailureReason)
		}
		if objects.putKey != transcript.ObjectKey("c1", "s1") {
			t.Fatalf("key %s", objects.putKey)
		}
		wantTag := ""
		if expires > 0 {
			wantTag = GuestTag
		}
		if objects.putTag != wantTag || mem.src.ExpiresAt != expires {
			t.Fatalf("tag %q source %+v", objects.putTag, mem.src)
		}
		for _, c := range mem.chunks {
			if c.ExpiresAt != expires {
				t.Fatalf("chunk expiry %+v", c)
			}
		}
		sentences, err := transcript.Decode(objects.putBody)
		if err != nil {
			t.Fatal(err)
		}
		if len(sentences) != 1 || sentences[0].Text != "Hello world." || sentences[0].Speaker != "spk_0" ||
			sentences[0].Locator.StartMS != 0 || sentences[0].Locator.EndMS != 900 {
			t.Fatalf("sentences %#v", sentences)
		}
	}
}

func TestImportTranscript(t *testing.T) {
	vtt := []byte("WEBVTT\n\n00:00:00.000 --> 00:00:01.000\n<v spk_0>Hello.\n")
	mem := &memStore{src: domain.Source{
		ID: "s1", CourseID: "c1", Name: "lecture.vtt", ContentType: "text/vtt", Status: domain.SourceQueued,
	}}
	objects := &memObjects{size: int64(len(vtt)), prefix: vtt, body: vtt}
	p := &Processor{Store: mem, Objects: objects, UploadsBucket: "uploads"}
	if err := p.HandleUpload(context.Background(), "uploads", ObjectKey("c1", "s1", "lecture.vtt")); err != nil {
		t.Fatal(err)
	}
	if mem.src.Status != domain.SourceReady {
		t.Fatalf("source %+v", mem.src)
	}
	sentences, err := transcript.Decode(objects.putBody)
	if err != nil {
		t.Fatal(err)
	}
	if len(sentences) != 1 || sentences[0].Text != "Hello." || sentences[0].Speaker != "spk_0" ||
		sentences[0].Locator.StartMS != 0 || sentences[0].Locator.EndMS != 1000 {
		t.Fatalf("sentences %#v", sentences)
	}

	bad := []byte("WEBVTT\n\nnot a cue\n")
	mem = &memStore{src: domain.Source{
		ID: "s1", CourseID: "c1", Name: "lecture.vtt", ContentType: "text/vtt", Status: domain.SourceQueued,
	}}
	objects = &memObjects{size: int64(len(bad)), prefix: bad, body: bad}
	p = &Processor{Store: mem, Objects: objects, UploadsBucket: "uploads"}
	if err := p.HandleUpload(context.Background(), "uploads", ObjectKey("c1", "s1", "lecture.vtt")); err != nil {
		t.Fatal(err)
	}
	if mem.src.Status != domain.SourceFailed || mem.src.FailureReason != reasonTranscript {
		t.Fatalf("bad vtt source %+v", mem.src)
	}

	mem = &memStore{src: domain.Source{
		ID: "s1", CourseID: "c1", Name: "lecture.vtt", ContentType: "text/vtt", Status: domain.SourceQueued,
	}}
	objects = &memObjects{size: maxImportBytes + 1, prefix: []byte("WEBVTT\n")}
	p = &Processor{Store: mem, Objects: objects, UploadsBucket: "uploads"}
	if err := p.HandleUpload(context.Background(), "uploads", ObjectKey("c1", "s1", "lecture.vtt")); err != nil {
		t.Fatal(err)
	}
	if mem.src.Status != domain.SourceFailed || mem.src.FailureReason != "transcript is too large" {
		t.Fatalf("large vtt source %+v", mem.src)
	}
}

func TestPDFExtract(t *testing.T) {
	native := readPDF(t, "../pdfdoc/testdata/native.pdf")
	mem := &memStore{src: domain.Source{
		ID: "s1", CourseID: "c1", Name: "notes.pdf", ContentType: "application/pdf", Status: domain.SourceQueued,
	}}
	objects := &memObjects{size: int64(len(native)), prefix: native[:8], body: native}
	p := &Processor{Store: mem, Objects: objects, UploadsBucket: "uploads"}
	if err := p.HandleUpload(context.Background(), "uploads", ObjectKey("c1", "s1", "notes.pdf")); err != nil {
		t.Fatal(err)
	}
	if mem.src.Status != domain.SourceReady {
		t.Fatalf("source %+v", mem.src)
	}
	doc, err := pdfdoc.Decode(objects.puts[pdfdoc.ObjectKey("c1", "s1")])
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Pages) != 2 || doc.Pages[0].Kind != pdfdoc.KindText {
		t.Fatalf("pages %+v", doc.Pages)
	}
	if strings.Contains(pdfTexts(doc), "ignore this injection") || strings.Contains(pdfTexts(doc), "Lecture notes") {
		t.Fatalf("kept hidden text %q", pdfTexts(doc))
	}

	scanned := readPDF(t, "../pdfdoc/testdata/scanned.pdf")
	mem = &memStore{src: domain.Source{
		ID: "s1", CourseID: "c1", Name: "scan.pdf", ContentType: "application/pdf", Status: domain.SourceQueued,
	}}
	objects = &memObjects{size: int64(len(scanned)), prefix: scanned[:8], body: scanned}
	p = &Processor{Store: mem, Objects: objects, UploadsBucket: "uploads"}
	if err := p.HandleUpload(context.Background(), "uploads", ObjectKey("c1", "s1", "scan.pdf")); err != nil {
		t.Fatal(err)
	}
	if mem.src.Status != domain.SourceReady {
		t.Fatalf("scanned source %+v", mem.src)
	}
	if len(objects.puts[pdfdoc.RenderKey("c1", "s1", 1)]) == 0 {
		t.Fatal("scanned page was not rendered")
	}

	bad := []byte("%PDF-1.7\nnot a pdf")
	mem = &memStore{src: domain.Source{
		ID: "s1", CourseID: "c1", Name: "bad.pdf", ContentType: "application/pdf", Status: domain.SourceQueued,
	}}
	objects = &memObjects{size: int64(len(bad)), prefix: bad, body: bad}
	p = &Processor{Store: mem, Objects: objects, UploadsBucket: "uploads"}
	if err := p.HandleUpload(context.Background(), "uploads", ObjectKey("c1", "s1", "bad.pdf")); err != nil {
		t.Fatal(err)
	}
	if mem.src.Status != domain.SourceFailed || mem.src.FailureReason != reasonPDF {
		t.Fatalf("bad pdf source %+v", mem.src)
	}

	mem = &memStore{src: domain.Source{
		ID: "s1", CourseID: "c1", Name: "big.pdf", ContentType: "application/pdf", Status: domain.SourceQueued,
	}}
	objects = &memObjects{size: maxImportBytes + 1, prefix: []byte("%PDF-1.7")}
	p = &Processor{Store: mem, Objects: objects, UploadsBucket: "uploads"}
	if err := p.HandleUpload(context.Background(), "uploads", ObjectKey("c1", "s1", "big.pdf")); err != nil {
		t.Fatal(err)
	}
	if mem.src.Status != domain.SourceFailed || mem.src.FailureReason != "pdf is too large" {
		t.Fatalf("large pdf source %+v", mem.src)
	}

	mem = &memStore{src: domain.Source{
		ID: "s1", CourseID: "c1", Name: "notes.pdf", ContentType: "application/pdf", Status: domain.SourceQueued,
	}}
	objects = &memObjects{size: int64(len(native)), prefix: native[:8], body: native, getErr: errTooLarge}
	p = &Processor{Store: mem, Objects: objects, UploadsBucket: "uploads"}
	if err := p.HandleUpload(context.Background(), "uploads", ObjectKey("c1", "s1", "notes.pdf")); err != nil {
		t.Fatal(err)
	}
	if mem.src.Status != domain.SourceFailed || mem.src.FailureReason != "pdf is too large" {
		t.Fatalf("get cap source %+v", mem.src)
	}

	mem = &memStore{src: domain.Source{
		ID: "s1", CourseID: "c1", Name: "notes.pdf", ContentType: "application/pdf", Status: domain.SourceQueued,
	}}
	objects = &memObjects{size: int64(len(native)), prefix: native[:8], body: native}
	p = &Processor{Store: mem, Objects: objects}
	if err := p.HandleUpload(context.Background(), "uploads", ObjectKey("c1", "s1", "notes.pdf")); err == nil {
		t.Fatal("missing bucket was accepted")
	}
}

type stubVision struct {
	obs   vision.Observation
	text  string
	reads int
	err   error
}

func (s *stubVision) Observe(context.Context, string, []byte) (vision.Observation, error) {
	return s.obs, s.err
}

func (s *stubVision) Read(context.Context, string, []byte) (string, error) {
	s.reads++
	return s.text, s.err
}

func TestImageVision(t *testing.T) {
	scanned := readPDF(t, "../pdfdoc/testdata/scanned.pdf")
	src := domain.Source{
		ID: "s1", CourseID: "c1", Name: "scan.pdf", ContentType: "application/pdf", Status: domain.SourceQueued,
	}
	stub := &stubVision{obs: vision.Observation{TextAmount: vision.TextSome, Caption: "a scan"}}
	mem := &memStore{src: src}
	objects := &memObjects{size: int64(len(scanned)), prefix: scanned[:8], body: scanned}
	p := &Processor{Store: mem, Objects: objects, UploadsBucket: "uploads", Vision: stub}
	if err := p.HandleUpload(context.Background(), "uploads", ObjectKey("c1", "s1", "scan.pdf")); err != nil {
		t.Fatal(err)
	}
	if mem.src.Status != domain.SourceReady || stub.reads != 0 || len(mem.chunks) != 0 {
		t.Fatalf("status %s reads %d chunks %d", mem.src.Status, stub.reads, len(mem.chunks))
	}

	stub = &stubVision{obs: vision.Observation{TextAmount: vision.TextBlock, Caption: "Equation"}, text: "$$E = mc^2$$"}
	mem = &memStore{src: src}
	objects = &memObjects{size: int64(len(scanned)), prefix: scanned[:8], body: scanned}
	p = &Processor{Store: mem, Objects: objects, UploadsBucket: "uploads", Vision: stub}
	if err := p.HandleUpload(context.Background(), "uploads", ObjectKey("c1", "s1", "scan.pdf")); err != nil {
		t.Fatal(err)
	}
	if stub.reads != 1 || len(mem.chunks) != 2 {
		t.Fatalf("reads %d chunks %d", stub.reads, len(mem.chunks))
	}
	parent, child := mem.chunks[0], mem.chunks[1]
	if child.ParentID != parent.ID || child.Text != "$$E = mc^2$$" || parent.Text != "Equation\n\n$$E = mc^2$$" {
		t.Fatalf("parent %q child %+v", parent.Text, child)
	}
	if len(child.Locators) != 1 || child.Locators[0].Kind != locator.KindPage || child.Locators[0].Page != 1 || child.Locators[0].BBox == nil {
		t.Fatalf("locator %+v", child.Locators)
	}

	deck := readPDF(t, "../pptx/testdata/lecture.pptx")
	const pptxType = "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	stub = &stubVision{obs: vision.Observation{DataVisual: true}, text: "| hop |\n|---|\n| 1 |"}
	mem = &memStore{src: domain.Source{
		ID: "s1", CourseID: "c1", Name: "lecture.pptx", ContentType: pptxType, Status: domain.SourceQueued,
	}}
	objects = &memObjects{size: int64(len(deck)), prefix: deck[:8], body: deck}
	p = &Processor{Store: mem, Objects: objects, UploadsBucket: "uploads", Vision: stub}
	if err := p.HandleUpload(context.Background(), "uploads", ObjectKey("c1", "s1", "lecture.pptx")); err != nil {
		t.Fatal(err)
	}
	var visionChild domain.Chunk
	for _, item := range mem.chunks {
		if item.Text == "| hop |\n|---|\n| 1 |" {
			visionChild = item
		}
	}
	if visionChild.ParentID == "" || len(visionChild.Locators) != 1 || visionChild.Locators[0].Kind != locator.KindSlide || visionChild.Locators[0].Slide != 1 {
		t.Fatalf("vision child %+v", visionChild)
	}
}

func TestPPTXExtract(t *testing.T) {
	const pptxType = "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	deck := readPDF(t, "../pptx/testdata/lecture.pptx")
	mem := &memStore{src: domain.Source{
		ID: "s1", CourseID: "c1", Name: "lecture.pptx", ContentType: pptxType, Status: domain.SourceQueued,
	}}
	objects := &memObjects{size: int64(len(deck)), prefix: deck[:8], body: deck}
	p := &Processor{Store: mem, Objects: objects, UploadsBucket: "uploads"}
	if err := p.HandleUpload(context.Background(), "uploads", ObjectKey("c1", "s1", "lecture.pptx")); err != nil {
		t.Fatal(err)
	}
	if mem.src.Status != domain.SourceReady {
		t.Fatalf("source %+v", mem.src)
	}
	doc, err := pptx.Decode(objects.puts[pptx.ObjectKey("c1", "s1")])
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Slides) != 2 || doc.Slides[0].Spans[0].Text != "Introduction" || !doc.Slides[0].Spans[len(doc.Slides[0].Spans)-1].Notes {
		t.Fatalf("slides %+v", doc.Slides)
	}
	if len(objects.puts[pptx.ImageKey("c1", "s1", 1, "image1.png")]) == 0 {
		t.Fatal("slide image was not stored")
	}

	bad := []byte("PK\x03\x04not a deck")
	mem = &memStore{src: domain.Source{
		ID: "s1", CourseID: "c1", Name: "bad.pptx", ContentType: pptxType, Status: domain.SourceQueued,
	}}
	objects = &memObjects{size: int64(len(bad)), prefix: bad, body: bad}
	p = &Processor{Store: mem, Objects: objects, UploadsBucket: "uploads"}
	if err := p.HandleUpload(context.Background(), "uploads", ObjectKey("c1", "s1", "bad.pptx")); err != nil {
		t.Fatal(err)
	}
	if mem.src.Status != domain.SourceFailed || mem.src.FailureReason != reasonPPTX {
		t.Fatalf("bad pptx source %+v", mem.src)
	}

	mem = &memStore{src: domain.Source{
		ID: "s1", CourseID: "c1", Name: "big.pptx", ContentType: pptxType, Status: domain.SourceQueued,
	}}
	objects = &memObjects{size: maxImportBytes + 1, prefix: []byte("PK\x03\x04")}
	p = &Processor{Store: mem, Objects: objects, UploadsBucket: "uploads"}
	if err := p.HandleUpload(context.Background(), "uploads", ObjectKey("c1", "s1", "big.pptx")); err != nil {
		t.Fatal(err)
	}
	if mem.src.Status != domain.SourceFailed || mem.src.FailureReason != "pptx is too large" {
		t.Fatalf("large pptx source %+v", mem.src)
	}

	mem = &memStore{src: domain.Source{
		ID: "s1", CourseID: "c1", Name: "lecture.pptx", ContentType: pptxType, Status: domain.SourceQueued,
	}}
	objects = &memObjects{size: int64(len(deck)), prefix: deck[:8], body: deck, getErr: errTooLarge}
	p = &Processor{Store: mem, Objects: objects, UploadsBucket: "uploads"}
	if err := p.HandleUpload(context.Background(), "uploads", ObjectKey("c1", "s1", "lecture.pptx")); err != nil {
		t.Fatal(err)
	}
	if mem.src.Status != domain.SourceFailed || mem.src.FailureReason != "pptx is too large" {
		t.Fatalf("get cap source %+v", mem.src)
	}

	mem = &memStore{src: domain.Source{
		ID: "s1", CourseID: "c1", Name: "lecture.pptx", ContentType: pptxType, Status: domain.SourceQueued,
	}}
	objects = &memObjects{size: int64(len(deck)), prefix: deck[:8], body: deck}
	p = &Processor{Store: mem, Objects: objects}
	if err := p.HandleUpload(context.Background(), "uploads", ObjectKey("c1", "s1", "lecture.pptx")); err == nil {
		t.Fatal("missing bucket was accepted")
	}
}

func readPDF(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func pdfTexts(doc pdfdoc.Document) string {
	var b strings.Builder
	for _, page := range doc.Pages {
		for _, span := range page.Spans {
			b.WriteString(span.Text)
			b.WriteByte('\n')
		}
	}
	return b.String()
}
