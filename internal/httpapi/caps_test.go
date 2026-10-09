package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sumonmselim/scholia-aws/internal/auth"
	"github.com/sumonmselim/scholia-aws/internal/domain"
)

func TestClientIP(t *testing.T) {
	tests := []struct {
		name, remote, viewer, forwarded, want string
	}{
		{"viewer ipv4", "10.0.0.1:1234", "198.51.100.7:443", "", "198.51.100.7"},
		{"viewer ipv6", "10.0.0.1:1234", "2001:db8::1:46532", "", "2001:db8::1"},
		{"viewer bracketed ipv6", "10.0.0.1:1234", "[2001:db8::2]:443", "", "2001:db8::2"},
		{"forwarded-for is ignored", "10.0.0.1:1234", "198.51.100.7:443", "203.0.113.99", "198.51.100.7"},
		{"forwarded-for alone is ignored", "10.0.0.1:1234", "", "203.0.113.99, 198.51.100.1", "10.0.0.1"},
		{"malformed viewer uses the connection", "10.0.0.1:1234", "not-an-ip", "", "10.0.0.1"},
		{"viewer without a port uses the connection", "10.0.0.1:1234", "198.51.100.7", "", "10.0.0.1"},
		{"raw remote", "203.0.113.4", "", "", "203.0.113.4"},
		{"no address", "", "", "", "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tt.remote
			if tt.viewer != "" {
				r.Header.Set(viewerHeader, tt.viewer)
			}
			if tt.forwarded != "" {
				r.Header.Set("X-Forwarded-For", tt.forwarded)
			}
			if got := clientIP(r); got != tt.want {
				t.Fatalf("clientIP = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSessionTokenHeader(t *testing.T) {
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	s := &server{opts: Options{Sessions: &auth.Fake{}, SessionSecret: sessionSecret, Now: func() time.Time { return now }}}
	token, err := auth.Sign(sessionSecret, "owner", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		header map[string]string
		want   string
	}{
		{"token header", map[string]string{TokenHeader: token}, "owner"},
		{"bearer for direct callers", map[string]string{"Authorization": "Bearer " + token}, "owner"},
		{"token header wins over a CloudFront signature", map[string]string{TokenHeader: token, "Authorization": "AWS4-HMAC-SHA256 Credential=x"}, "owner"},
		{"a bad token header does not fall back to bearer", map[string]string{TokenHeader: "forged", "Authorization": "Bearer " + token}, ""},
		{"token header with a space", map[string]string{TokenHeader: token + " x"}, ""},
		{"blank token header", map[string]string{TokenHeader: "  "}, ""},
		{"sigv4 only", map[string]string{"Authorization": "AWS4-HMAC-SHA256 Credential=x"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			for k, v := range tt.header {
				r.Header.Set(k, v)
			}
			if got := s.caller(r).subject; got != tt.want {
				t.Fatalf("subject = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGuestDailyCaps(t *testing.T) {
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	usage := &memUsage{}
	c := newChatHarness(t, func(o *Options) {
		o.Now = func() time.Time { return now }
		o.Guests = true
		o.Usage = usage
	})
	start := func(viewer, spoof string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/session/guest", nil)
		req.Header.Set(viewerHeader, viewer+":443")
		req.Header.Set("X-Forwarded-For", spoof)
		rec := httptest.NewRecorder()
		c.h.ServeHTTP(rec, req)
		return rec
	}
	// Rotating X-Forwarded-For must not buy fresh sessions.
	for i := range guestsPerAddress {
		if rec := start("198.51.100.7", "203.0.113."+string(rune('a'+i))); rec.Code != http.StatusCreated {
			t.Fatalf("guest %d status %d body %s", i, rec.Code, rec.Body)
		}
	}
	rec := start("198.51.100.7", "203.0.113.250")
	if rec.Code != http.StatusTooManyRequests || errorCode(t, rec.Body.String()) != "rate_limited" {
		t.Fatalf("over address cap status %d body %s", rec.Code, rec.Body)
	}
	if rec := start("198.51.100.8", ""); rec.Code != http.StatusCreated {
		t.Fatalf("other address status %d", rec.Code)
	}

	usage.counts[deploymentMeter+"/"+domain.UsageGuests] = guestsPerDay
	if rec := start("198.51.100.9", ""); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("over deployment cap status %d", rec.Code)
	}

	usage.err = errors.New("table down")
	if rec := start("198.51.100.10", ""); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("store failure must fail closed, status %d", rec.Code)
	}
}

func TestSignInCodeCaps(t *testing.T) {
	usage := &memUsage{}
	c := newChatHarness(t, func(o *Options) { o.Usage = usage })
	send := func(email, viewer string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/session", strings.NewReader(`{"email":"`+email+`"}`))
		req.Header.Set(viewerHeader, viewer+":443")
		rec := httptest.NewRecorder()
		c.h.ServeHTTP(rec, req)
		return rec.Code
	}
	// One address is capped per email, whichever network asks.
	for i := range codesPerEmail {
		if code := send("ada@example.com", "198.51.100."+string(rune('1'+i))); code != http.StatusOK {
			t.Fatalf("code %d status %d", i, code)
		}
	}
	if code := send(" ADA@example.com ", "198.51.100.200"); code != http.StatusTooManyRequests {
		t.Fatalf("over email cap status %d", code)
	}
	for key := range usage.counts {
		if strings.Contains(key, "ada") {
			t.Fatalf("meter %q stores the email address", key)
		}
	}

	// One network is capped across emails.
	for i := range codesPerAddress {
		if code := send("user"+string(rune('a'+i))+"@example.com", "203.0.113.5"); code != http.StatusOK {
			t.Fatalf("address code %d status %d", i, code)
		}
	}
	if code := send("late@example.com", "203.0.113.5"); code != http.StatusTooManyRequests {
		t.Fatalf("over address cap status %d", code)
	}
}
