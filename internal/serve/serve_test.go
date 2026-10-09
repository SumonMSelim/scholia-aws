package serve

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestInLambda(t *testing.T) {
	if InLambda(func(string) string { return "" }) {
		t.Fatal("expected false without runtime API")
	}
	if !InLambda(func(k string) string {
		if k == "AWS_LAMBDA_RUNTIME_API" {
			return "127.0.0.1:9001"
		}
		return ""
	}) {
		t.Fatal("expected true with runtime API")
	}
}

func TestHTTPServesAndShutsDown(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "pong") })
	go func() { done <- serveListener(ctx, slog.New(slog.DiscardHandler), ln, h) }()

	resp, err := http.Get("http://" + ln.Addr().String())
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != "pong" {
		t.Fatalf("body = %q", body)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down")
	}
}

func TestHTTPListenError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	if err := HTTP(context.Background(), slog.New(slog.DiscardHandler), ln.Addr().String(), http.NotFoundHandler()); err == nil {
		t.Fatal("expected error for address in use")
	}
}

func TestHTTPServeErrorReturned(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_ = ln.Close()
	if err := serveListener(context.Background(), slog.New(slog.DiscardHandler), ln, http.NotFoundHandler()); err == nil {
		t.Fatal("expected error from closed listener")
	}
}
