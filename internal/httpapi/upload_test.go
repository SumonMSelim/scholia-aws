package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sumonmselim/scholia-aws/internal/domain"
	"github.com/sumonmselim/scholia-aws/internal/store"
)

type fakeSources struct {
	course  domain.Course
	getErr  error
	sources map[string]domain.Source
	deleted []string
}

func (f *fakeSources) GetCourse(context.Context, string) (domain.Course, error) {
	if f.getErr != nil {
		return domain.Course{}, f.getErr
	}
	if f.course.ID == "" {
		return domain.Course{}, store.ErrNotFound
	}
	return f.course, nil
}

func (f *fakeSources) PutCourse(_ context.Context, course domain.Course) error {
	f.course = course
	return nil
}

func (f *fakeSources) ListCourses(context.Context) ([]domain.Course, error) {
	if f.course.ID == "" {
		return []domain.Course{}, nil
	}
	return []domain.Course{f.course}, nil
}

func (f *fakeSources) ListSources(context.Context, string) ([]domain.Source, error) {
	if f.sources == nil {
		return []domain.Source{}, nil
	}
	out := make([]domain.Source, 0, len(f.sources))
	for _, src := range f.sources {
		out = append(out, src)
	}
	return out, nil
}

func (f *fakeSources) PutSource(_ context.Context, src domain.Source) error {
	if f.sources == nil {
		f.sources = map[string]domain.Source{}
	}
	f.sources[src.ID] = src
	return nil
}

func (f *fakeSources) DeleteSource(_ context.Context, _, sourceID string) error {
	f.deleted = append(f.deleted, sourceID)
	delete(f.sources, sourceID)
	return nil
}

type fakePresign struct {
	url     string
	err     error
	tagging string
	size    int64
}

func (f *fakePresign) PresignPutSized(_ context.Context, _, _, _, tagging string, size int64) (string, error) {
	f.tagging = tagging
	f.size = size
	return f.url, f.err
}

func TestCreateUpload(t *testing.T) {
	sources := &fakeSources{course: domain.Course{ID: "c1", Title: "Nets"}}
	presign := &fakePresign{url: "https://uploads.example/put"}
	h := New(Options{
		Sources: sources, Presign: presign,
		UploadsBucket: "bucket", NewID: func() (string, error) { return "s1", nil },
	})
	body := `{"name":"notes.pdf","content_type":"application/pdf","byte_size":12}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/courses/c1/sources", strings.NewReader(body))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"source_id":"s1"`) || !strings.Contains(rec.Body.String(), "https://uploads.example/put") {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	if sources.sources["s1"].Status != domain.SourceQueued {
		t.Fatalf("stored %+v", sources.sources["s1"])
	}
	if presign.size != 12 {
		t.Fatalf("signed size %d, want the declared 12", presign.size)
	}
}

func TestCreateUploadRejects(t *testing.T) {
	ok := `{"name":"notes.pdf","content_type":"application/pdf","byte_size":12}`
	tests := []struct {
		name   string
		opts   Options
		body   string
		status int
	}{
		{"unconfigured", Options{}, ok, http.StatusServiceUnavailable},
		{"bad json", Options{Sources: &fakeSources{course: domain.Course{ID: "c1"}}, Presign: &fakePresign{}, UploadsBucket: "b"}, `{`, http.StatusBadRequest},
		{"oversize", Options{Sources: &fakeSources{course: domain.Course{ID: "c1"}}, Presign: &fakePresign{}, UploadsBucket: "b"}, `{"name":"notes.pdf","content_type":"application/pdf","byte_size":9999999999}`, http.StatusBadRequest},
		{"missing course", Options{Sources: &fakeSources{}, Presign: &fakePresign{}, UploadsBucket: "b"}, ok, http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := New(tt.opts)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/courses/c1/sources", strings.NewReader(tt.body)))
			if rec.Code != tt.status {
				t.Fatalf("status %d body %s", rec.Code, rec.Body)
			}
		})
	}
}

func TestCreateUploadPresignFailureDeletesSource(t *testing.T) {
	sources := &fakeSources{course: domain.Course{ID: "c1", Title: "Nets"}}
	h := New(Options{
		Sources: sources, Presign: &fakePresign{err: errors.New("sign")},
		UploadsBucket: "b", NewID: func() (string, error) { return "s1", nil },
	})
	rec := httptest.NewRecorder()
	body := `{"name":"notes.pdf","content_type":"application/pdf","byte_size":12}`
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/courses/c1/sources", strings.NewReader(body)))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d", rec.Code)
	}
	if len(sources.deleted) != 1 || len(sources.sources) != 0 {
		t.Fatalf("deleted %v remaining %v", sources.deleted, sources.sources)
	}
}
