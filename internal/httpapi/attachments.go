package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/sumonmselim/scholia-aws/internal/attach"
	"github.com/sumonmselim/scholia-aws/internal/domain"
	"github.com/sumonmselim/scholia-aws/internal/ingest"
	"github.com/sumonmselim/scholia-aws/internal/store"
)

const maxAttachmentIDRunes = 64

type attachmentRequest struct {
	Name        string `json:"name"`
	ContentType string `json:"content_type"`
	ByteSize    int64  `json:"byte_size"`
}

type attachmentBody struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ContentType string `json:"content_type"`
	ByteSize    int64  `json:"byte_size"`
}

type attachmentResponse struct {
	Attachment attachmentBody `json:"attachment"`
	UploadURL  string         `json:"upload_url"`
	// Headers must be sent with the PUT. It is set for a guest's tagged upload.
	Headers map[string]string `json:"headers,omitempty"`
}

// createAttachment signs an upload for one chat-only file. The object key sits
// outside courses/, so the ingest worker ignores it and course knowledge never sees it.
func (s *server) createAttachment(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.chatUser(w, r)
	if !ok {
		return
	}
	if s.opts.Attachments == nil || s.opts.Presign == nil || s.opts.UploadsBucket == "" {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "attachments are not configured")
		return
	}
	var req attachmentRequest
	if !decodeJSON(w, r, 4096, &req) {
		return
	}
	if err := attach.Validate(req.Name, req.ContentType, req.ByteSize); err != nil {
		writeError(w, http.StatusBadRequest, "invalid", err.Error())
		return
	}
	chat, ok := s.loadChat(w, r, userID)
	if !ok {
		return
	}
	who := s.caller(r)
	if s.uploadsPaused(w, r) || !s.charge(w, r, who, domain.UsageUploads) {
		return
	}
	id, err := s.newID()
	if err != nil {
		s.opts.Logger.ErrorContext(r.Context(), "new id", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal", "could not start the upload")
		return
	}
	att := domain.Attachment{
		ID: id, ChatID: chat.ID, Name: req.Name, ContentType: strings.TrimSpace(req.ContentType),
		ByteSize: req.ByteSize, Key: ingest.AttachmentKey(chat.ID, id, req.Name), CreatedAt: s.opts.Now().UTC(),
		ExpiresAt: s.expiry(who),
	}
	if err := s.opts.Attachments.PutAttachment(r.Context(), att); err != nil {
		s.opts.Logger.ErrorContext(r.Context(), "put attachment", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal", "could not start the upload")
		return
	}
	url, err := s.opts.Presign.PresignPutSized(r.Context(), s.opts.UploadsBucket, att.Key, att.ContentType, tagging(who), att.ByteSize)
	if err != nil {
		if delErr := s.opts.Attachments.DeleteAttachment(r.Context(), chat.ID, id); delErr != nil {
			s.opts.Logger.ErrorContext(r.Context(), "delete attachment after presign failure", slog.String("err", delErr.Error()))
		}
		s.opts.Logger.ErrorContext(r.Context(), "presign", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal", "could not start the upload")
		return
	}
	writeJSON(w, http.StatusCreated, attachmentResponse{
		Attachment: attachmentBody{ID: att.ID, Name: att.Name, ContentType: att.ContentType, ByteSize: att.ByteSize},
		UploadURL:  url,
		Headers:    uploadHeaders(who),
	})
}

func (s *server) deleteAttachment(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.chatUser(w, r)
	if !ok {
		return
	}
	if s.opts.Attachments == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "attachments are not configured")
		return
	}
	chat, ok := s.loadChat(w, r, userID)
	if !ok {
		return
	}
	att, ok := s.loadAttachment(w, r, chat.ID, r.PathValue("attachmentID"))
	if !ok {
		return
	}
	if s.opts.Objects != nil {
		if err := s.opts.Objects.Delete(r.Context(), s.opts.UploadsBucket, att.Key); err != nil {
			s.opts.Logger.ErrorContext(r.Context(), "delete attachment object", slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal", "could not remove the attachment")
			return
		}
	}
	if err := s.opts.Attachments.DeleteAttachment(r.Context(), chat.ID, att.ID); err != nil {
		s.opts.Logger.ErrorContext(r.Context(), "delete attachment", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal", "could not remove the attachment")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) loadAttachment(w http.ResponseWriter, r *http.Request, chatID, id string) (domain.Attachment, bool) {
	att, err := s.opts.Attachments.GetAttachment(r.Context(), chatID, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "attachment not found")
			return domain.Attachment{}, false
		}
		s.opts.Logger.ErrorContext(r.Context(), "get attachment", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal", "could not read the attachment")
		return domain.Attachment{}, false
	}
	return att, true
}

// readAttachments loads this message's files. Every id must belong to the chat,
// and each object must be uploaded and match its declared type.
func (s *server) readAttachments(w http.ResponseWriter, r *http.Request, chatID string, ids []string) ([]attach.File, []domain.AttachmentRef, bool) {
	if len(ids) == 0 {
		return nil, nil, true
	}
	if s.opts.Attachments == nil || s.opts.Objects == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "attachments are not configured")
		return nil, nil, false
	}
	files := make([]attach.File, 0, len(ids))
	refs := make([]domain.AttachmentRef, 0, len(ids))
	for _, id := range ids {
		att, ok := s.loadAttachment(w, r, chatID, id)
		if !ok {
			return nil, nil, false
		}
		body, err := s.opts.Objects.Get(r.Context(), s.opts.UploadsBucket, att.Key)
		if err != nil {
			s.opts.Logger.ErrorContext(r.Context(), "read attachment", slog.String("err", err.Error()))
			writeError(w, http.StatusBadRequest, "invalid", "attachment "+att.Name+" is not uploaded yet")
			return nil, nil, false
		}
		file := attach.File{Name: att.Name, ContentType: att.ContentType, Body: body}
		if err := attach.Check(file); err != nil {
			writeError(w, http.StatusBadRequest, "invalid", "attachment "+att.Name+": "+err.Error())
			return nil, nil, false
		}
		files = append(files, file)
		refs = append(refs, domain.AttachmentRef{ID: att.ID, Name: att.Name, ContentType: att.ContentType})
	}
	return files, refs, true
}

// removeAttachmentObjects deletes a chat's files before the chat goes.
// A failure is logged: the rows still go, and the bucket lifecycle can sweep the rest.
func (s *server) removeAttachmentObjects(r *http.Request, chatID string) {
	if s.opts.Attachments == nil || s.opts.Objects == nil {
		return
	}
	atts, err := s.opts.Attachments.ListAttachments(r.Context(), chatID)
	if err != nil {
		s.opts.Logger.ErrorContext(r.Context(), "list attachments", slog.String("err", err.Error()))
		return
	}
	for _, att := range atts {
		if err := s.opts.Objects.Delete(r.Context(), s.opts.UploadsBucket, att.Key); err != nil {
			s.opts.Logger.ErrorContext(r.Context(), "delete attachment object", slog.String("err", err.Error()))
		}
	}
}

func validAttachmentIDs(ids []string) bool {
	if len(ids) > attach.MaxPerMessage {
		return false
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" || len(id) > maxAttachmentIDRunes || strings.ContainsAny(id, "/# \t\r\n") || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}
