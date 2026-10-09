package httpapi

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sumonmselim/scholia-aws/internal/auth"
)

type memoryBox struct {
	blobs map[string][]byte
}

func (b *memoryBox) Seal(_ context.Context, userID string, plaintext []byte) ([]byte, error) {
	if b.blobs == nil {
		b.blobs = map[string][]byte{}
	}
	sealed := append([]byte("v1:"+userID+":"), bytes.Repeat([]byte{1}, 8)...)
	b.blobs[userID] = append([]byte(nil), plaintext...)
	return sealed, nil
}

func (b *memoryBox) Open(_ context.Context, userID string, ciphertext []byte) ([]byte, error) {
	if !bytes.HasPrefix(ciphertext, []byte("v1:"+userID+":")) {
		return nil, errOpen
	}
	return append([]byte(nil), b.blobs[userID]...), nil
}

var errOpen = errString("context mismatch")

type errString string

func (e errString) Error() string { return string(e) }

type memoryKeys struct {
	userID string
	keys   map[string][]byte
	err    error
}

func (m *memoryKeys) PutProviderKey(_ context.Context, userID, provider string, ciphertext []byte) error {
	if m.err != nil {
		return m.err
	}
	if m.keys == nil {
		m.keys = map[string][]byte{}
	}
	m.userID = userID
	m.keys[provider] = append([]byte(nil), ciphertext...)
	return nil
}

func (m *memoryKeys) DeleteProviderKey(_ context.Context, _, provider string) error {
	if m.err != nil {
		return m.err
	}
	delete(m.keys, provider)
	return nil
}

func (m *memoryKeys) ListProviderKeys(context.Context, string) ([]string, error) {
	if m.err != nil {
		return nil, m.err
	}
	var out []string
	for p := range m.keys {
		out = append(out, p)
	}
	return out, nil
}

func TestKeyIsWriteOnly(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	token, err := auth.Sign(sessionSecret, "owner", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	keys := &memoryKeys{}
	h := New(Options{
		Logger:        slog.New(slog.NewJSONHandler(&logs, nil)),
		Sessions:      &auth.Fake{Subject: "owner"},
		SessionSecret: sessionSecret,
		Now:           func() time.Time { return now },
		Keys:          keys,
		Box:           &memoryBox{},
		Settings:      &memorySettings{},
	})
	secret := "sk-live-secret"
	body := `{"api_key":"` + secret + `"}`
	req := httptest.NewRequest(http.MethodPut, "/api/account/keys/openai", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("put status %d body %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), secret) || strings.Contains(logs.String(), secret) {
		t.Fatalf("raw key leaked response %q logs %q", rec.Body.String(), logs.String())
	}
	if bytes.Contains(keys.keys["openai"], []byte(secret)) || keys.userID != "owner" || len(keys.keys) != 1 {
		t.Fatalf("stored %+v", keys)
	}

	got := httptest.NewRequest(http.MethodGet, "/api/account/settings", nil)
	got.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, got)
	want := `{"default_model":"","providers":[{"provider":"openai","connected":true},{"provider":"bedrock","connected":false}]}`
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != want {
		t.Fatalf("settings status %d body %s", rec.Code, rec.Body)
	}

	del := httptest.NewRequest(http.MethodDelete, "/api/account/keys/openai", nil)
	del.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, del)
	if rec.Code != http.StatusNoContent || len(keys.keys) != 0 {
		t.Fatalf("delete status %d keys %v", rec.Code, keys.keys)
	}

	anon := httptest.NewRecorder()
	h.ServeHTTP(anon, httptest.NewRequest(http.MethodPut, "/api/account/keys/openai", strings.NewReader(body)))
	if anon.Code != http.StatusUnauthorized || strings.Contains(anon.Body.String(), secret) {
		t.Fatalf("anon status %d body %s", anon.Code, anon.Body)
	}
}

// leakyBox returns the plaintext, which the handler must refuse to store.
type leakyBox struct{}

func (leakyBox) Seal(_ context.Context, _ string, plaintext []byte) ([]byte, error) {
	return plaintext, nil
}

func (leakyBox) Open(_ context.Context, _ string, ciphertext []byte) ([]byte, error) {
	return ciphertext, nil
}

func TestKeyAndModelRejects(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	token, err := auth.Sign(sessionSecret, "owner", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	failing := errString("table down")
	tests := []struct {
		name   string
		method string
		path   string
		body   string
		opts   func(*Options)
		status int
	}{
		{"no box", http.MethodPut, "/api/account/keys/openai", `{"api_key":"sk-live-secret"}`, func(o *Options) { o.Box = nil }, http.StatusServiceUnavailable},
		{"no keys on delete", http.MethodDelete, "/api/account/keys/openai", "", func(o *Options) { o.Keys = nil }, http.StatusServiceUnavailable},
		{"bad provider", http.MethodPut, "/api/account/keys/other", `{"api_key":"sk-live-secret"}`, nil, http.StatusBadRequest},
		{"bad provider on delete", http.MethodDelete, "/api/account/keys/other", "", nil, http.StatusBadRequest},
		{"missing key", http.MethodPut, "/api/account/keys/openai", `{"api_key":" "}`, nil, http.StatusBadRequest},
		{"key with newline", http.MethodPut, "/api/account/keys/bedrock", `{"api_key":"a\nb"}`, nil, http.StatusBadRequest},
		{"unknown field", http.MethodPut, "/api/account/keys/openai", `{"provider":"openai","api_key":"k"}`, nil, http.StatusBadRequest},
		{"box leaks", http.MethodPut, "/api/account/keys/openai", `{"api_key":"sk-live-secret"}`, func(o *Options) { o.Box = leakyBox{} }, http.StatusBadGateway},
		{"store fails", http.MethodPut, "/api/account/keys/openai", `{"api_key":"sk-live-secret"}`, func(o *Options) { o.Keys = &memoryKeys{err: failing} }, http.StatusInternalServerError},
		{"delete fails", http.MethodDelete, "/api/account/keys/openai", "", func(o *Options) { o.Keys = &memoryKeys{err: failing} }, http.StatusInternalServerError},
		{"no settings", http.MethodGet, "/api/account/settings", "", func(o *Options) { o.Settings = nil }, http.StatusServiceUnavailable},
		{"no settings on put", http.MethodPut, "/api/account/settings", `{"default_model":""}`, func(o *Options) { o.Settings = nil }, http.StatusServiceUnavailable},
		{"settings read fails", http.MethodGet, "/api/account/settings", "", func(o *Options) { o.Settings = &memorySettings{err: failing} }, http.StatusInternalServerError},
		{"settings write fails", http.MethodPut, "/api/account/settings", `{"default_model":"gpt-4.1"}`, func(o *Options) { o.Settings = &memorySettings{err: failing} }, http.StatusInternalServerError},
		{"keys list fails", http.MethodGet, "/api/account/settings", "", func(o *Options) { o.Keys = &memoryKeys{err: failing} }, http.StatusInternalServerError},
		{"bad default model", http.MethodPut, "/api/account/settings", `{"default_model":"has space"}`, nil, http.StatusBadRequest},
		{"bad settings body", http.MethodPut, "/api/account/settings", `{"model":"x"}`, nil, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := Options{
				Sessions: &auth.Fake{}, SessionSecret: sessionSecret, Now: func() time.Time { return now },
				Sources: &fakeSources{}, Keys: &memoryKeys{}, Box: &memoryBox{}, Settings: &memorySettings{},
			}
			if tt.opts != nil {
				tt.opts(&opts)
			}
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Authorization", "Bearer "+token)
			New(opts).ServeHTTP(rec, req)
			if rec.Code != tt.status || strings.Contains(rec.Body.String(), "sk-live-secret") {
				t.Fatalf("status %d body %s", rec.Code, rec.Body)
			}
		})
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	token, err := auth.Sign(sessionSecret, "owner", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	settings := &memorySettings{}
	h := New(Options{
		Sessions: &auth.Fake{}, SessionSecret: sessionSecret, Now: func() time.Time { return now },
		Settings: settings,
	})
	put := httptest.NewRequest(http.MethodPut, "/api/account/settings", strings.NewReader(`{"default_model":" gpt-4.1-mini "}`))
	put.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, put)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"default_model":"gpt-4.1-mini"`) {
		t.Fatalf("put status %d body %s", rec.Code, rec.Body)
	}
	if settings.byUser["owner"].DefaultModel != "gpt-4.1-mini" {
		t.Fatalf("stored %+v", settings.byUser)
	}
	// No key store means every provider reads as disconnected.
	if !strings.Contains(rec.Body.String(), `{"provider":"openai","connected":false}`) {
		t.Fatalf("providers %s", rec.Body)
	}
	anon := httptest.NewRecorder()
	h.ServeHTTP(anon, httptest.NewRequest(http.MethodGet, "/api/account/settings", nil))
	if anon.Code != http.StatusUnauthorized {
		t.Fatalf("anon status %d", anon.Code)
	}
}
