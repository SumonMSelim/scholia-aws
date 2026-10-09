package httpapi

import (
	"bytes"
	"log/slog"
	"net/http"
	"strings"

	"github.com/sumonmselim/scholia-aws/internal/domain"
)

const maxKeyBytes = 512

// providers are the key slots a user can fill, in the order settings lists them.
var providers = []string{"openai", "bedrock"}

type putKeyRequest struct {
	APIKey string `json:"api_key"`
}

type settingsRequest struct {
	DefaultModel string `json:"default_model"`
}

type providerBody struct {
	Provider  string `json:"provider"`
	Connected bool   `json:"connected"`
}

type settingsResponse struct {
	DefaultModel string         `json:"default_model"`
	Providers    []providerBody `json:"providers"`
}

// signedIn writes 401 and returns false when the caller has no session.
func (s *server) signedIn(w http.ResponseWriter, r *http.Request, action string) (string, bool) {
	who := s.caller(r)
	if !who.enforced || who.subject == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", "sign in to "+action)
		return "", false
	}
	return who.subject, true
}

func knownProvider(p string) bool {
	for _, known := range providers {
		if p == known {
			return true
		}
	}
	return false
}

func (s *server) getSettings(w http.ResponseWriter, r *http.Request) {
	if s.opts.Settings == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "settings are not configured")
		return
	}
	userID, ok := s.signedIn(w, r, "read settings")
	if !ok {
		return
	}
	settings, err := s.opts.Settings.GetSettings(r.Context(), userID)
	if err != nil {
		s.opts.Logger.ErrorContext(r.Context(), "get settings", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal", "could not read settings")
		return
	}
	s.writeSettings(w, r, userID, settings)
}

func (s *server) putSettings(w http.ResponseWriter, r *http.Request) {
	if s.opts.Settings == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "settings are not configured")
		return
	}
	userID, ok := s.signedIn(w, r, "change settings")
	if !ok {
		return
	}
	var req settingsRequest
	if !decodeJSON(w, r, 4096, &req) {
		return
	}
	settings := domain.Settings{DefaultModel: strings.TrimSpace(req.DefaultModel), ExpiresAt: s.expiry(s.caller(r))}
	if !modelID(settings.DefaultModel) {
		writeError(w, http.StatusBadRequest, "invalid", "model id is not valid")
		return
	}
	if err := s.opts.Settings.PutSettings(r.Context(), userID, settings); err != nil {
		s.opts.Logger.ErrorContext(r.Context(), "put settings", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal", "could not save settings")
		return
	}
	s.writeSettings(w, r, userID, settings)
}

func (s *server) writeSettings(w http.ResponseWriter, r *http.Request, userID string, settings domain.Settings) {
	connected := map[string]bool{}
	if s.opts.Keys != nil {
		stored, err := s.opts.Keys.ListProviderKeys(r.Context(), userID)
		if err != nil {
			s.opts.Logger.ErrorContext(r.Context(), "list keys", slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal", "could not read settings")
			return
		}
		for _, p := range stored {
			connected[p] = true
		}
	}
	out := settingsResponse{DefaultModel: settings.DefaultModel, Providers: make([]providerBody, len(providers))}
	for i, p := range providers {
		out.Providers[i] = providerBody{Provider: p, Connected: connected[p]}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) putKey(w http.ResponseWriter, r *http.Request) {
	if s.opts.Keys == nil || s.opts.Box == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "key storage is not configured")
		return
	}
	userID, ok := s.signedIn(w, r, "store a key")
	if !ok {
		return
	}
	// A guest's session is throwaway, so it never holds a sealed secret.
	if s.caller(r).guest() {
		writeError(w, http.StatusForbidden, "forbidden", "Guests cannot store API keys. Sign in with email to use your own key.")
		return
	}
	provider := r.PathValue("provider")
	if !knownProvider(provider) {
		writeError(w, http.StatusBadRequest, "invalid", "provider must be bedrock or openai")
		return
	}
	var req putKeyRequest
	if !decodeJSON(w, r, 4096, &req) {
		return
	}
	key := strings.TrimSpace(req.APIKey)
	if key == "" || len(key) > maxKeyBytes || strings.ContainsAny(key, "\r\n") {
		writeError(w, http.StatusBadRequest, "invalid", "api key is required")
		return
	}
	sealed, err := s.opts.Box.Seal(r.Context(), userID, []byte(key))
	// A box that returns the plaintext is treated as a failure, so the key never reaches the table.
	if err != nil || bytes.Contains(sealed, []byte(key)) {
		s.opts.Logger.ErrorContext(r.Context(), "seal key")
		writeError(w, http.StatusBadGateway, "unavailable", "could not store the key")
		return
	}
	if err := s.opts.Keys.PutProviderKey(r.Context(), userID, provider, sealed); err != nil {
		s.opts.Logger.ErrorContext(r.Context(), "put key")
		writeError(w, http.StatusInternalServerError, "internal", "could not store the key")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) deleteKey(w http.ResponseWriter, r *http.Request) {
	if s.opts.Keys == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "key storage is not configured")
		return
	}
	userID, ok := s.signedIn(w, r, "remove a key")
	if !ok {
		return
	}
	provider := r.PathValue("provider")
	if !knownProvider(provider) {
		writeError(w, http.StatusBadRequest, "invalid", "provider must be bedrock or openai")
		return
	}
	if err := s.opts.Keys.DeleteProviderKey(r.Context(), userID, provider); err != nil {
		s.opts.Logger.ErrorContext(r.Context(), "delete key")
		writeError(w, http.StatusInternalServerError, "internal", "could not remove the key")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
