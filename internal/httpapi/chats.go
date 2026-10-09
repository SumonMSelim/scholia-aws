package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sumonmselim/scholia-aws/internal/answer"
	"github.com/sumonmselim/scholia-aws/internal/domain"
	"github.com/sumonmselim/scholia-aws/internal/ingest"
	"github.com/sumonmselim/scholia-aws/internal/store"
)

const (
	newChatTitle      = "New chat"
	maxChatTitleRunes = 120
	autoTitleRunes    = 60
)

type chatBody struct {
	ID        string    `json:"id"`
	CourseID  string    `json:"course_id"`
	Title     string    `json:"title"`
	Model     string    `json:"model"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// messageBody is one stored turn. Citations, web and attachments are always arrays.
type messageBody struct {
	ID            string              `json:"id"`
	Role          string              `json:"role"`
	Text          string              `json:"text"`
	Mode          string              `json:"mode"`
	Citations     []answer.Citation   `json:"citations"`
	Web           []webBody           `json:"web"`
	Attachments   []attachmentRefBody `json:"attachments"`
	Refusal       string              `json:"refusal,omitempty"`
	RefusalReason string              `json:"refusal_reason,omitempty"`
	CreatedAt     time.Time           `json:"created_at"`
}

type attachmentRefBody struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ContentType string `json:"content_type"`
}

type chatListResponse struct {
	Chats []chatBody `json:"chats"`
}

type chatResponse struct {
	Chat     chatBody      `json:"chat"`
	Messages []messageBody `json:"messages"`
}

type createChatRequest struct {
	CourseID string `json:"course_id"`
	Model    string `json:"model"`
}

type renameChatRequest struct {
	Title string `json:"title"`
}

type messageRequest struct {
	Question      string   `json:"question"`
	Mode          string   `json:"mode"`
	AttachmentIDs []string `json:"attachment_ids"`
}

func chatJSON(chat domain.Chat) chatBody {
	return chatBody{
		ID: chat.ID, CourseID: chat.CourseID, Title: chat.Title, Model: chat.Model,
		CreatedAt: chat.CreatedAt.UTC(), UpdatedAt: chat.UpdatedAt.UTC(),
	}
}

func messageJSON(msg domain.Message) messageBody {
	citations := msg.Citations
	if citations == nil {
		citations = []answer.Citation{}
	}
	refs := make([]attachmentRefBody, len(msg.Attachments))
	for i, ref := range msg.Attachments {
		refs[i] = attachmentRefBody{ID: ref.ID, Name: ref.Name, ContentType: ref.ContentType}
	}
	return messageBody{
		ID: msg.ID, Role: msg.Role, Text: msg.Text, Mode: msg.Mode,
		Citations: citations, Web: webJSON(msg.Web), Attachments: refs,
		Refusal: msg.Refusal, RefusalReason: msg.RefusalReason, CreatedAt: msg.CreatedAt.UTC(),
	}
}

// chatUser checks that chats are configured and the caller is signed in.
func (s *server) chatUser(w http.ResponseWriter, r *http.Request) (string, bool) {
	if s.opts.Chats == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "chats are not configured")
		return "", false
	}
	return s.signedIn(w, r, "use chats")
}

// loadChat reads the caller's chat. Another user's chat reads as not found.
func (s *server) loadChat(w http.ResponseWriter, r *http.Request, userID string) (domain.Chat, bool) {
	chat, err := s.opts.Chats.GetChat(r.Context(), userID, r.PathValue("chatID"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "chat not found")
			return domain.Chat{}, false
		}
		s.opts.Logger.ErrorContext(r.Context(), "get chat", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal", "could not read the chat")
		return domain.Chat{}, false
	}
	return chat, true
}

func (s *server) newID() (string, error) {
	if s.opts.NewID != nil {
		return s.opts.NewID()
	}
	return ingest.NewID()
}

func (s *server) listChats(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.chatUser(w, r)
	if !ok {
		return
	}
	chats, err := s.opts.Chats.ListChats(r.Context(), userID)
	if err != nil {
		s.opts.Logger.ErrorContext(r.Context(), "list chats", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal", "could not list chats")
		return
	}
	out := make([]chatBody, len(chats))
	for i, chat := range chats {
		out[i] = chatJSON(chat)
	}
	writeJSON(w, http.StatusOK, chatListResponse{Chats: out})
}

func (s *server) createChat(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.chatUser(w, r)
	if !ok {
		return
	}
	if s.opts.Sources == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "courses are not configured")
		return
	}
	var req createChatRequest
	if !decodeJSON(w, r, 4096, &req) {
		return
	}
	courseID := strings.TrimSpace(req.CourseID)
	chatModel := strings.TrimSpace(req.Model)
	if courseID == "" || !modelID(chatModel) {
		writeError(w, http.StatusBadRequest, "invalid", "course id is required and the model id must be valid")
		return
	}
	course, err := s.opts.Sources.GetCourse(r.Context(), courseID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "course not found")
			return
		}
		s.opts.Logger.ErrorContext(r.Context(), "get course", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal", "could not start the chat")
		return
	}
	if who := s.caller(r); !who.canRead(course) {
		deny(w, who, false)
		return
	}
	// An empty model takes the user's default, so the chat records what answers
	// it. answer.Bound documents the whole order.
	if chatModel == "" && s.opts.Settings != nil {
		settings, err := s.opts.Settings.GetSettings(r.Context(), userID)
		if err != nil {
			s.opts.Logger.ErrorContext(r.Context(), "get settings", slog.String("err", err.Error()))
			writeError(w, http.StatusInternalServerError, "internal", "could not start the chat")
			return
		}
		chatModel = settings.DefaultModel
	}
	id, err := s.newID()
	if err != nil {
		s.opts.Logger.ErrorContext(r.Context(), "new id", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal", "could not start the chat")
		return
	}
	now := s.opts.Now().UTC()
	chat := domain.Chat{
		ID: id, OwnerID: userID, CourseID: course.ID, Title: newChatTitle, Model: chatModel,
		CreatedAt: now, UpdatedAt: now, ExpiresAt: s.expiry(s.caller(r)),
	}
	if err := s.opts.Chats.PutChat(r.Context(), chat); err != nil {
		s.opts.Logger.ErrorContext(r.Context(), "put chat", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal", "could not start the chat")
		return
	}
	writeJSON(w, http.StatusCreated, chatJSON(chat))
}

func (s *server) getChat(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.chatUser(w, r)
	if !ok {
		return
	}
	chat, ok := s.loadChat(w, r, userID)
	if !ok {
		return
	}
	msgs, err := s.opts.Chats.ListMessages(r.Context(), chat.ID)
	if err != nil {
		s.opts.Logger.ErrorContext(r.Context(), "list messages", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal", "could not read the chat")
		return
	}
	out := make([]messageBody, len(msgs))
	for i, msg := range msgs {
		out[i] = messageJSON(msg)
	}
	writeJSON(w, http.StatusOK, chatResponse{Chat: chatJSON(chat), Messages: out})
}

func (s *server) renameChat(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.chatUser(w, r)
	if !ok {
		return
	}
	var req renameChatRequest
	if !decodeJSON(w, r, 4096, &req) {
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" || utf8.RuneCountInString(title) > maxChatTitleRunes {
		writeError(w, http.StatusBadRequest, "invalid", "title is required and must be at most 120 characters")
		return
	}
	chat, ok := s.loadChat(w, r, userID)
	if !ok {
		return
	}
	chat.Title = title
	chat.UpdatedAt = s.opts.Now().UTC()
	if err := s.opts.Chats.PutChat(r.Context(), chat); err != nil {
		s.opts.Logger.ErrorContext(r.Context(), "put chat", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal", "could not rename the chat")
		return
	}
	writeJSON(w, http.StatusOK, chatJSON(chat))
}

func (s *server) deleteChat(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.chatUser(w, r)
	if !ok {
		return
	}
	chat, ok := s.loadChat(w, r, userID)
	if !ok {
		return
	}
	s.removeAttachmentObjects(r, chat.ID)
	if err := s.opts.Chats.DeleteChat(r.Context(), userID, chat.ID); err != nil {
		s.opts.Logger.ErrorContext(r.Context(), "delete chat", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal", "could not delete the chat")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// postMessage stores the question, streams the answer from the chat's course
// and model, then stores the reply. A failed answer leaves only the question.
func (s *server) postMessage(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.chatUser(w, r)
	if !ok {
		return
	}
	var req messageRequest
	if !decodeJSON(w, r, maxQuestionBytes, &req) {
		return
	}
	question := strings.TrimSpace(req.Question)
	mode := strings.TrimSpace(req.Mode)
	if mode == "" {
		mode = domain.ModeAnswer
	}
	if question == "" || (mode != domain.ModeAnswer && mode != domain.ModeExplain) {
		writeError(w, http.StatusBadRequest, "invalid", "question is required and mode must be answer or explain")
		return
	}
	if !validAttachmentIDs(req.AttachmentIDs) {
		writeError(w, http.StatusBadRequest, "invalid", "at most 5 distinct attachment ids are allowed")
		return
	}
	runner, fail := s.opts.Answers, "could not answer"
	if mode == domain.ModeExplain {
		runner, fail = s.opts.Tutor, "could not explain"
	}
	if runner == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "answers are not configured")
		return
	}
	chat, ok := s.loadChat(w, r, userID)
	if !ok {
		return
	}
	if !s.allowUse(w, r, chat.CourseID, fail) {
		return
	}
	earlier, err := s.opts.Chats.ListMessages(r.Context(), chat.ID)
	if err != nil {
		s.opts.Logger.ErrorContext(r.Context(), "list messages", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal", fail)
		return
	}
	files, refs, ok := s.readAttachments(w, r, chat.ID, req.AttachmentIDs)
	if !ok {
		return
	}

	asked := s.opts.Now().UTC()
	// Messages expire with their chat, so a guest's thread goes as one.
	if !s.putMessage(w, r, domain.Message{ChatID: chat.ID, Role: domain.RoleUser, Text: question, Mode: mode, Attachments: refs, CreatedAt: asked, ExpiresAt: chat.ExpiresAt}) {
		return
	}
	if chat.Title == newChatTitle {
		chat.Title = autoTitle(question)
	}
	chat.UpdatedAt = asked
	if err := s.opts.Chats.PutChat(r.Context(), chat); err != nil {
		s.opts.Logger.ErrorContext(r.Context(), "put chat", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal", fail)
		return
	}

	out := s.stream(w, r, runner, answer.Request{
		CourseID: chat.CourseID, Question: question, ChatModel: chat.Model, UserID: userID,
		History: turns(earlier), Files: files, Meter: meter(r, s.caller(r)),
	})
	if !out.ok {
		return
	}
	// The reply sorts after the question even when the clock has not moved.
	answered := s.opts.Now().UTC()
	if !answered.After(asked) {
		answered = asked.Add(time.Nanosecond)
	}
	reply := domain.Message{ChatID: chat.ID, Role: domain.RoleAssistant, Text: out.text, Mode: mode, CreatedAt: answered, ExpiresAt: chat.ExpiresAt}
	if out.outcome.Refused {
		// The refusal replaces any text that streamed first, such as a guardrail's
		// own blocked message, on screen and in the history of later turns.
		reply.Text = ""
		reply.Refusal = firstNonEmptyText(out.outcome.Text, answer.RefusalText)
		reply.RefusalReason = firstNonEmptyText(out.outcome.Reason, domain.RefusalNoMaterial)
	} else {
		reply.Citations = out.outcome.Citations
		reply.Web = out.outcome.Web
	}
	// Headers are sent, so a store failure is only logged.
	if err := s.storeReply(r, chat, reply); err != nil {
		s.opts.Logger.ErrorContext(r.Context(), "store reply", slog.String("err", err.Error()))
	}
}

// turns maps stored messages onto prompt history. An earlier turn keeps only
// the names of its attachments: their text was read into that turn's prompt
// once, and repeating it every turn would crowd out the budget.
func turns(msgs []domain.Message) []answer.Turn {
	out := make([]answer.Turn, 0, len(msgs))
	for _, msg := range msgs {
		text := msg.Text
		if msg.Role == domain.RoleAssistant && strings.TrimSpace(text) == "" {
			text = msg.Refusal
		}
		if len(msg.Attachments) > 0 {
			names := make([]string, len(msg.Attachments))
			for i, ref := range msg.Attachments {
				names[i] = ref.Name
			}
			text += "\n[attached: " + strings.Join(names, ", ") + "]"
		}
		out = append(out, answer.Turn{Role: msg.Role, Text: text})
	}
	return out
}

func firstNonEmptyText(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func (s *server) storeReply(r *http.Request, chat domain.Chat, reply domain.Message) error {
	id, err := s.newID()
	if err != nil {
		return err
	}
	reply.ID = id
	if err := s.opts.Chats.PutMessage(r.Context(), reply); err != nil {
		return err
	}
	chat.UpdatedAt = reply.CreatedAt
	return s.opts.Chats.PutChat(r.Context(), chat)
}

func (s *server) putMessage(w http.ResponseWriter, r *http.Request, msg domain.Message) bool {
	id, err := s.newID()
	if err == nil {
		msg.ID = id
		err = s.opts.Chats.PutMessage(r.Context(), msg)
	}
	if err != nil {
		s.opts.Logger.ErrorContext(r.Context(), "put message", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal", "could not store the message")
		return false
	}
	return true
}

// autoTitle names a chat after its first question.
func autoTitle(question string) string {
	title := strings.Join(strings.Fields(question), " ")
	if utf8.RuneCountInString(title) <= autoTitleRunes {
		return title
	}
	return strings.TrimSpace(string([]rune(title)[:autoTitleRunes-1])) + "…"
}
