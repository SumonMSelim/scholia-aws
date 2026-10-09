package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/sumonmselim/scholia-aws/internal/auth"
	"github.com/sumonmselim/scholia-aws/internal/domain"
)

type startSessionRequest struct {
	Email string `json:"email"`
}

type startSessionResponse struct {
	Session string `json:"session"`
}

type confirmSessionRequest struct {
	Email   string `json:"email"`
	Session string `json:"session"`
	Code    string `json:"code"`
}

type confirmSessionResponse struct {
	Token string `json:"token"`
}

func (s *server) startSession(w http.ResponseWriter, r *http.Request) {
	if s.opts.Sessions == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "sign-in is not configured")
		return
	}
	var req startSessionRequest
	if !decodeJSON(w, r, 1024, &req) {
		return
	}
	email := strings.TrimSpace(req.Email)
	if !s.allowCalls(w, r, domain.UsageCodes, "Too many sign-in codes requested. Please try again tomorrow.",
		dailyCap{meterPrefix + clientIP(r), codesPerAddress}, dailyCap{emailMeter(email), codesPerEmail}) {
		return
	}
	session, err := s.opts.Sessions.Start(r.Context(), email)
	if err != nil {
		// The provider error can contain the address. Keep it out of the log and the response.
		s.opts.Logger.ErrorContext(r.Context(), "start session")
		writeError(w, http.StatusBadRequest, "invalid", "could not send a code")
		return
	}
	writeJSON(w, http.StatusOK, startSessionResponse{Session: session})
}

func (s *server) confirmSession(w http.ResponseWriter, r *http.Request) {
	if s.opts.Sessions == nil || s.opts.SessionSecret == "" {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "sign-in is not configured")
		return
	}
	var req confirmSessionRequest
	if !decodeJSON(w, r, 8192, &req) {
		return
	}
	code := strings.TrimSpace(req.Code)
	if utf8.RuneCountInString(code) < 4 || utf8.RuneCountInString(code) > 8 {
		writeError(w, http.StatusBadRequest, "invalid", "code was not accepted")
		return
	}
	id, err := s.opts.Sessions.Confirm(r.Context(), strings.TrimSpace(req.Email), req.Session, code)
	if err != nil || id.Subject == "" {
		s.opts.Logger.ErrorContext(r.Context(), "confirm session")
		writeError(w, http.StatusBadRequest, "invalid", "code was not accepted")
		return
	}
	token, err := auth.Sign(s.opts.SessionSecret, id.Subject, s.opts.Now().Add(auth.SessionTTL))
	if err != nil {
		s.opts.Logger.ErrorContext(r.Context(), "sign session")
		writeError(w, http.StatusInternalServerError, "internal", "could not sign in")
		return
	}
	writeJSON(w, http.StatusOK, confirmSessionResponse{Token: token})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, max int64, dest any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, max))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		writeError(w, http.StatusBadRequest, "invalid", "request body is not valid")
		return false
	}
	return true
}
