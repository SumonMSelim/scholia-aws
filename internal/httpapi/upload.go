package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/sumonmselim/scholia-aws/internal/domain"
	"github.com/sumonmselim/scholia-aws/internal/ingest"
	"github.com/sumonmselim/scholia-aws/internal/store"
)

type uploadRequest struct {
	Name        string `json:"name"`
	ContentType string `json:"content_type"`
	ByteSize    int64  `json:"byte_size"`
}

type uploadResponse struct {
	SourceID  string `json:"source_id"`
	Status    string `json:"status"`
	Method    string `json:"method"`
	UploadURL string `json:"upload_url"`
	Key       string `json:"key"`
	// Headers must be sent with the PUT. It is set for a guest's tagged upload.
	Headers map[string]string `json:"headers,omitempty"`
}

func (s *server) createUpload(w http.ResponseWriter, r *http.Request) {
	if s.opts.Sources == nil || s.opts.Presign == nil || s.opts.UploadsBucket == "" {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "uploads are not configured")
		return
	}
	var req uploadRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid", "request body is not valid")
		return
	}
	if err := ingest.ValidateUpload(req.Name, req.ContentType, req.ByteSize); err != nil {
		writeError(w, http.StatusBadRequest, "invalid", err.Error())
		return
	}
	courseID := r.PathValue("courseID")
	course, err := s.opts.Sources.GetCourse(r.Context(), courseID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "course not found")
			return
		}
		s.opts.Logger.ErrorContext(r.Context(), "get course", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal", "could not start upload")
		return
	}
	who := s.caller(r)
	if !who.canWrite(course) {
		deny(w, who, true)
		return
	}
	if s.uploadsPaused(w, r) || !s.charge(w, r, who, domain.UsageUploads) {
		return
	}
	newID := s.opts.NewID
	if newID == nil {
		newID = ingest.NewID
	}
	sourceID, err := newID()
	if err != nil {
		s.opts.Logger.ErrorContext(r.Context(), "new id", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal", "could not start upload")
		return
	}
	key := ingest.ObjectKey(courseID, sourceID, req.Name)
	src := domain.Source{
		ID:          sourceID,
		CourseID:    courseID,
		Name:        req.Name,
		ContentType: req.ContentType,
		Status:      domain.SourceQueued,
		ExpiresAt:   s.expiry(who),
	}
	if err := s.opts.Sources.PutSource(r.Context(), src); err != nil {
		s.opts.Logger.ErrorContext(r.Context(), "put source", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal", "could not start upload")
		return
	}
	url, err := s.opts.Presign.PresignPutSized(r.Context(), s.opts.UploadsBucket, key, req.ContentType, tagging(who), req.ByteSize)
	if err != nil {
		if delErr := s.opts.Sources.DeleteSource(r.Context(), courseID, sourceID); delErr != nil {
			s.opts.Logger.ErrorContext(r.Context(), "delete source after presign failure", slog.String("err", delErr.Error()))
		}
		s.opts.Logger.ErrorContext(r.Context(), "presign", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal", "could not start upload")
		return
	}
	writeJSON(w, http.StatusCreated, uploadResponse{
		SourceID:  sourceID,
		Status:    string(domain.SourceQueued),
		Method:    http.MethodPut,
		UploadURL: url,
		Key:       key,
		Headers:   uploadHeaders(who),
	})
}
