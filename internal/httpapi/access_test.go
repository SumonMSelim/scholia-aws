package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sumonmselim/scholia-aws/internal/answer"
	"github.com/sumonmselim/scholia-aws/internal/auth"
	"github.com/sumonmselim/scholia-aws/internal/domain"
	"github.com/sumonmselim/scholia-aws/internal/limit"
	"github.com/sumonmselim/scholia-aws/internal/model"
)

const sessionSecret = "session-secret-16"

func signedHandler(t *testing.T, sources *fakeSources, gate *limit.Gate) (http.Handler, string) {
	t.Helper()
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	h := New(Options{
		Sources: sources, Presign: &fakePresign{url: "https://uploads.example/put"},
		UploadsBucket: "bucket",
		NewID:         func() (string, error) { return "c1", nil },
		Sessions:      &auth.Fake{Subject: "owner"},
		SessionSecret: sessionSecret,
		Gate:          gate,
		Now:           func() time.Time { return now },
		Answers: &answer.Service{
			Search: fixedSearch{chunks: []domain.Chunk{{
				ID: "p1", CourseID: "c1", SourceID: "s1", Text: "Routing picks the next hop.",
			}}},
			Model: model.Mock{},
		},
	})
	token, err := auth.Sign(sessionSecret, "owner", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return h, token
}

func TestAnonymousCannotWriteAndOwnerUploads(t *testing.T) {
	sources := &fakeSources{course: domain.Course{ID: "c1", Title: "Networks", OwnerID: "owner", Public: true}}
	h, token := signedHandler(t, sources, limit.New(20, 1))

	write := httptest.NewRequest(http.MethodPost, "/api/courses", strings.NewReader(`{"title":"Signals"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, write)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous create status %d body %s", rec.Code, rec.Body)
	}

	upload := httptest.NewRequest(http.MethodPost, "/api/courses/c1/sources", strings.NewReader(`{"name":"notes.pdf","content_type":"application/pdf","byte_size":12}`))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, upload)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous upload status %d body %s", rec.Code, rec.Body)
	}

	owned := httptest.NewRequest(http.MethodPost, "/api/courses/c1/sources", strings.NewReader(`{"name":"notes.pdf","content_type":"application/pdf","byte_size":12}`))
	owned.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, owned)
	if rec.Code != http.StatusCreated || sources.sources["c1"].Status != domain.SourceQueued {
		t.Fatalf("owner upload status %d body %s stored %+v", rec.Code, rec.Body, sources.sources)
	}
}

func TestPrivateCourseIsHiddenFromOthers(t *testing.T) {
	sources := &fakeSources{}
	h, token := signedHandler(t, sources, limit.New(1, 50))

	create := httptest.NewRequest(http.MethodPost, "/api/courses", strings.NewReader(`{"title":"Signals","public":false}`))
	create.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, create)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"public":false`) {
		t.Fatalf("create status %d body %s", rec.Code, rec.Body)
	}

	list := httptest.NewRecorder()
	h.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/courses", nil))
	if strings.Contains(list.Body.String(), "Signals") {
		t.Fatalf("anonymous list leaked a private course: %s", list.Body)
	}

	ownerList := httptest.NewRequest(http.MethodGet, "/api/courses", nil)
	ownerList.Header.Set("Authorization", "Bearer "+token)
	list = httptest.NewRecorder()
	h.ServeHTTP(list, ownerList)
	if !strings.Contains(list.Body.String(), "Signals") || !strings.Contains(list.Body.String(), `"mine":true`) {
		t.Fatalf("owner list %s", list.Body)
	}

	other, err := auth.Sign(sessionSecret, "other", time.Date(2026, 9, 28, 13, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	foreign := httptest.NewRequest(http.MethodPost, "/api/courses/c1/sources", strings.NewReader(`{"name":"notes.pdf","content_type":"application/pdf","byte_size":12}`))
	foreign.Header.Set("Authorization", "Bearer "+other)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, foreign)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("foreign upload status %d body %s", rec.Code, rec.Body)
	}
}

func TestSessionRoundTrip(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	h := New(Options{
		Sessions:      &auth.Fake{Subject: "owner", Code: "123456"},
		SessionSecret: sessionSecret,
		Now:           func() time.Time { return now },
	})
	off := httptest.NewRecorder()
	New(Options{}).ServeHTTP(off, httptest.NewRequest(http.MethodPost, "/api/session", strings.NewReader(`{"email":"ada@example.com"}`)))
	if off.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured status %d", off.Code)
	}

	start := httptest.NewRecorder()
	h.ServeHTTP(start, httptest.NewRequest(http.MethodPost, "/api/session", strings.NewReader(`{"email":"ada@example.com"}`)))
	if start.Code != http.StatusOK || !strings.Contains(start.Body.String(), `"session"`) {
		t.Fatalf("start status %d body %s", start.Code, start.Body)
	}
	confirm := httptest.NewRecorder()
	h.ServeHTTP(confirm, httptest.NewRequest(http.MethodPost, "/api/session/confirm", strings.NewReader(`{"email":"ada@example.com","session":"sess","code":"123456"}`)))
	if confirm.Code != http.StatusOK || !strings.Contains(confirm.Body.String(), `"token"`) {
		t.Fatalf("confirm status %d body %s", confirm.Code, confirm.Body)
	}
	bad := httptest.NewRecorder()
	h.ServeHTTP(bad, httptest.NewRequest(http.MethodPost, "/api/session/confirm", strings.NewReader(`{"email":"ada@example.com","session":"sess","code":"000000"}`)))
	if bad.Code != http.StatusBadRequest || strings.Contains(bad.Body.String(), "@") {
		t.Fatalf("bad code status %d body %s", bad.Code, bad.Body)
	}
}
