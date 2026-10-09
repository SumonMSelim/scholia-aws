package httpapi

import (
	"net"
	"net/http"
	"strings"

	"github.com/sumonmselim/scholia-aws/internal/auth"
	"github.com/sumonmselim/scholia-aws/internal/domain"
)

type caller struct {
	enforced bool
	subject  string
}

func (s *server) caller(r *http.Request) caller {
	if s.opts.Sessions == nil {
		return caller{}
	}
	token, ok := sessionToken(r)
	if !ok {
		return caller{enforced: true}
	}
	subject, err := auth.Verify(s.opts.SessionSecret, token, s.opts.Now())
	if err != nil {
		return caller{enforced: true}
	}
	return caller{enforced: true, subject: subject}
}

// TokenHeader carries the session token. CloudFront's origin access control signs
// every request to the function URL and overwrites Authorization with that
// signature, so a browser behind CloudFront cannot use a bearer token.
const TokenHeader = "X-Scholia-Token" // #nosec G101 -- a header name, not a credential

// sessionToken reads TokenHeader, then a bearer token for callers that reach the
// API directly (local compose, scripts).
func sessionToken(r *http.Request) (string, bool) {
	if v := r.Header.Get(TokenHeader); v != "" {
		token := strings.TrimSpace(v)
		return token, token != "" && !strings.Contains(token, " ")
	}
	return bearer(r.Header.Get("Authorization"))
}

func bearer(header string) (string, bool) {
	token, ok := strings.CutPrefix(header, "Bearer ")
	if !ok || strings.TrimSpace(token) == "" || strings.Contains(token, " ") {
		return "", false
	}
	return token, true
}

func (c caller) canRead(course domain.Course) bool {
	if !c.enforced {
		return true
	}
	return course.Public || (c.subject != "" && course.OwnerID == c.subject)
}

func (c caller) canWrite(course domain.Course) bool {
	if !c.enforced {
		return true
	}
	return c.subject != "" && course.OwnerID == c.subject
}

// guest reports a guest session: lower allowances, and every record it creates expires.
func (c caller) guest() bool {
	return auth.IsGuest(c.subject)
}

func (c caller) anonymous() bool {
	return c.enforced && c.subject == ""
}

func deny(w http.ResponseWriter, c caller, write bool) {
	if c.anonymous() {
		msg := "sign in to view this course"
		if write {
			msg = "sign in to change a course"
		}
		writeError(w, http.StatusUnauthorized, "unauthorized", msg)
		return
	}
	writeError(w, http.StatusForbidden, "forbidden", "you do not own this course")
}

// viewerHeader is set by CloudFront to the viewer's "address:port" (IPv6 unbracketed).
const viewerHeader = "CloudFront-Viewer-Address"

// clientIP is the address the per-address limits count. X-Forwarded-For is never
// used: its first hop is whatever the client sent. Behind CloudFront it is the
// viewer address CloudFront sets; direct callers (local compose, tests) use the
// connection's address.
func clientIP(r *http.Request) string {
	if v := r.Header.Get(viewerHeader); v != "" {
		if i := strings.LastIndexByte(v, ':'); i > 0 {
			if ip := net.ParseIP(strings.Trim(v[:i], "[]")); ip != nil {
				return ip.String()
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		if r.RemoteAddr != "" {
			return r.RemoteAddr
		}
		return "unknown"
	}
	return host
}
