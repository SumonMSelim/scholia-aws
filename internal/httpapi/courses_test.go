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

func TestListAndCreateCourse(t *testing.T) {
	sources := &fakeSources{}
	h := New(Options{Sources: sources, NewID: func() (string, error) { return "c1", nil }})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/courses", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"courses":[]`) {
		t.Fatalf("empty list status %d body %s", rec.Code, rec.Body)
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/courses", strings.NewReader(`{"title":" Networks "}`))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"id":"c1"`) || !strings.Contains(rec.Body.String(), `"title":"Networks"`) {
		t.Fatalf("create status %d body %s", rec.Code, rec.Body)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/courses", nil))
	if !strings.Contains(rec.Body.String(), `"title":"Networks"`) {
		t.Fatalf("list body %s", rec.Body)
	}
}

func TestCreateCourseRejects(t *testing.T) {
	tests := []struct {
		name   string
		opts   Options
		body   string
		status int
	}{
		{name: "unconfigured", opts: Options{}, body: `{"title":"Nets"}`, status: http.StatusServiceUnavailable},
		{name: "empty title", opts: Options{Sources: &fakeSources{}}, body: `{"title":"  "}`, status: http.StatusBadRequest},
		{name: "long title", opts: Options{Sources: &fakeSources{}}, body: `{"title":"` + strings.Repeat("a", 201) + `"}`, status: http.StatusBadRequest},
		{name: "bad json", opts: Options{Sources: &fakeSources{}}, body: `{`, status: http.StatusBadRequest},
		{name: "id error", opts: Options{Sources: &fakeSources{}, NewID: func() (string, error) { return "", errors.New("id") }}, body: `{"title":"Nets"}`, status: http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := New(tt.opts)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/courses", strings.NewReader(tt.body)))
			if rec.Code != tt.status {
				t.Fatalf("status %d body %s", rec.Code, rec.Body)
			}
		})
	}
}

func TestListSources(t *testing.T) {
	sources := &fakeSources{course: domain.Course{ID: "c1", Title: "Nets"}}
	sources.sources = map[string]domain.Source{
		"s1": {ID: "s1", CourseID: "c1", Name: "notes.txt", ContentType: "text/plain", Status: domain.SourceFailed, FailureReason: "bad magic"},
		"s2": {ID: "s2", CourseID: "c1", Name: "Lecture 1.vtt", ContentType: "text/vtt", Status: domain.SourceReady, YouTubeID: "ZA-tUyM_y7s"},
	}
	h := New(Options{Sources: sources})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/courses/c1/sources", nil))
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `"status":"failed"`) || !strings.Contains(body, "bad magic") {
		t.Fatalf("status %d body %s", rec.Code, body)
	}
	if !strings.Contains(body, `"youtube_id":"ZA-tUyM_y7s"`) || strings.Count(body, "youtube_id") != 1 {
		t.Fatalf("youtube_id should appear only on the lecture: %s", body)
	}
}

func TestListSourcesStoreError(t *testing.T) {
	h := New(Options{Sources: &fakeSources{getErr: errors.New("down")}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/courses/c1/sources", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d", rec.Code)
	}
	h = New(Options{})
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/courses/c1/sources", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured status %d", rec.Code)
	}
}

func TestListSourcesMissingCourse(t *testing.T) {
	h := New(Options{Sources: &fakeSources{getErr: store.ErrNotFound}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/courses/missing/sources", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestListCoursesUnavailable(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/courses", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d", rec.Code)
	}
}

type errSources struct{ fakeSources }

func (e *errSources) PutCourse(context.Context, domain.Course) error {
	return errors.New("down")
}

func (e *errSources) ListCourses(context.Context) ([]domain.Course, error) {
	return nil, errors.New("down")
}

func (e *errSources) ListSources(context.Context, string) ([]domain.Source, error) {
	return nil, errors.New("down")
}

func TestCourseStoreErrors(t *testing.T) {
	h := New(Options{Sources: &errSources{fakeSources: fakeSources{course: domain.Course{ID: "c1", Title: "Nets"}}}})
	for _, path := range []string{"/api/courses", "/api/courses/c1/sources"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("%s status %d", path, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/courses", strings.NewReader(`{"title":"Nets"}`)))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("create status %d", rec.Code)
	}
}
