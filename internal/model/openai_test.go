package model

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCompatibleContract(t *testing.T) {
	const key = "sk-test-secret"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+key {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		text := replyFor(string(body))
		if strings.Contains(string(body), `"stream":true`) {
			w.Header().Set("Content-Type", "text/event-stream")
			mid := len(text) / 2
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":"+jsonString(text[:mid])+"}}]}\n\n")
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":"+jsonString(text[mid:])+"}}]}\n\n")
			_, _ = io.WriteString(w, "data: [DONE]\n")
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":`+jsonString(text)+`}}]}`)
	}))
	t.Cleanup(srv.Close)
	client, err := OpenCompatible(srv.URL+"/v1", "gpt-test", key)
	if err != nil {
		t.Fatal(err)
	}
	exercise(t, client)
}

func TestCompatibleOmitsKey(t *testing.T) {
	const key = "sk-test-secret"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "echo "+key, http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)
	client, err := OpenCompatible(srv.URL, "gpt-test", key)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Text: "the key is " + key}},
	})
	if err == nil || strings.Contains(err.Error(), key) {
		t.Fatalf("err %v", err)
	}
}

func TestCompatibleRejectsBase(t *testing.T) {
	cases := []struct {
		base, model, key string
	}{
		{base: "http://example.com/v1", model: "m", key: "sk"},
		{base: "https://example.com/v1", model: "m", key: ""},
		{base: "https://user:sk@example.com/v1", model: "m", key: "sk"},
		{base: "not a url", model: "m", key: "sk"},
	}
	for _, tc := range cases {
		if _, err := OpenCompatible(tc.base, tc.model, tc.key); err == nil {
			t.Fatalf("accepted %s", tc.base)
		}
	}
}

func jsonString(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

func TestCompatibleHasTimeout(t *testing.T) {
	client, err := OpenCompatible("https://api.example.test/v1", "gpt-test", "sk-test")
	if err != nil {
		t.Fatal(err)
	}
	// A stalled provider must end before the API Lambda's 60 second timeout.
	if client.http.Timeout <= 0 || client.http.Timeout >= 60*time.Second {
		t.Fatalf("timeout = %v, want between 0 and 60s", client.http.Timeout)
	}
}
