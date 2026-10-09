package websearch

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func tavilyAt(t *testing.T, h http.HandlerFunc) *Tavily {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	tv := OpenTavily(" tvly-test ")
	tv.endpoint = srv.URL
	return tv
}

func TestOpenTavilyOffWithoutKey(t *testing.T) {
	if OpenTavily("  ") != nil {
		t.Fatal("an empty key turned web search on")
	}
}

func TestTavilySearch(t *testing.T) {
	var got tavilyRequest
	var auth string
	tv := tavilyAt(t, func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Error(err)
		}
		_, _ = io.WriteString(w, `{"results":[
			{"title":"TCP","url":"https://example.test/tcp","content":"`+strings.Repeat("x", 2000)+`"},
			{"title":"bad","url":"javascript:alert(1)","content":"x"},
			{"title":"","url":"http://example.test/udp","content":"UDP"},
			{"title":"creds","url":"https://u:p@example.test/","content":"x"}
		]}`)
	})
	results, err := tv.Search(context.Background(), "  Networks:\n what is   TCP? ")
	if err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer tvly-test" || got.Query != "Networks: what is TCP?" || got.SearchDepth != "basic" || got.MaxResults != 5 || got.IncludeAnswer {
		t.Fatalf("auth=%q request=%+v", auth, got)
	}
	if len(results) != 2 || results[0].URL != "https://example.test/tcp" || len([]rune(results[0].Snippet)) != maxSnippetRunes {
		t.Fatalf("results = %+v", results)
	}
	if results[1].Title != "http://example.test/udp" {
		t.Fatalf("untitled result = %+v", results[1])
	}
}

func TestTavilyFailures(t *testing.T) {
	tests := []struct {
		name  string
		h     http.HandlerFunc
		query string
	}{
		{"status", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "tvly-test leaked", http.StatusUnauthorized)
		}, "q"},
		{"not json", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "<html>") }, "q"},
		{"empty query", func(http.ResponseWriter, *http.Request) {}, "   "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tavilyAt(t, tt.h).Search(context.Background(), tt.query)
			if err == nil || strings.Contains(err.Error(), "tvly-test") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestTavilyHonoursTheDeadline(t *testing.T) {
	release := make(chan struct{})
	tv := tavilyAt(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := tv.Search(ctx, "q"); err == nil {
		t.Fatal("a stalled search succeeded")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("search ignored the deadline")
	}
}

func TestTavilyUnreachable(t *testing.T) {
	tv := OpenTavily("tvly-test")
	tv.endpoint = "http://127.0.0.1:1"
	if _, err := tv.Search(context.Background(), "q"); err == nil || strings.Contains(err.Error(), "tvly") {
		t.Fatalf("err = %v", err)
	}
}

func TestFake(t *testing.T) {
	f := &Fake{Results: []Result{{Title: "a", URL: "https://a.test"}}}
	got, err := f.Search(context.Background(), "q1")
	if err != nil || len(got) != 1 || f.Queries[0] != "q1" {
		t.Fatalf("got=%v err=%v", got, err)
	}
	f.Err = errors.New("down")
	if _, err := f.Search(context.Background(), "q2"); err == nil || len(f.Queries) != 2 {
		t.Fatal("fake error was not returned")
	}
}
