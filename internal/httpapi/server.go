// Package httpapi implements the Scholia HTTP API as a standard http.Handler,
// served by net/http locally and by a Lambda function URL in AWS.
package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/sumonmselim/scholia-aws/internal/answer"
	"github.com/sumonmselim/scholia-aws/internal/auth"
	"github.com/sumonmselim/scholia-aws/internal/domain"
	"github.com/sumonmselim/scholia-aws/internal/killswitch"
	"github.com/sumonmselim/scholia-aws/internal/limit"
	"github.com/sumonmselim/scholia-aws/internal/seal"
)

// Options configures the API handler.
type Options struct {
	Logger  *slog.Logger
	Version string
	Commit  string
	Env     string
	// Now is injectable for tests; defaults to time.Now.
	Now func() time.Time
	// Sources and Presign enable POST /api/courses/{courseID}/sources.
	// Health still serves when they are unset.
	Sources       SourceStore
	Presign       Presigner
	UploadsBucket string
	NewID         func() (string, error)
	// Answers answers chat messages.
	// Health and upload still serve when it is unset.
	Answers Answerer
	// Tutor answers chat messages sent in explain mode.
	Tutor Answerer
	// Sessions turns sign-in on. Nil leaves every course open, which is
	// how local compose and the tests run until Cognito is configured.
	Sessions      auth.Passwordless
	SessionSecret string
	// Gate limits answer traffic. Nil means no limit.
	Gate *limit.Gate
	// Keys and Box store a user's provider keys, one per provider. Both are
	// required to store a key. Box encrypts with the user id as the KMS encryption context.
	Keys KeyStore
	Box  seal.Box
	// Settings enables /api/account/settings.
	Settings SettingsStore
	// Chats enables /api/chats. Messages are answered by Answers and Tutor.
	Chats ChatStore
	// Attachments and Objects enable chat attachments. Presign and UploadsBucket sign the upload.
	Attachments AttachmentStore
	Objects     ObjectStore
	// Usage meters messages, uploads and web searches per subject and day.
	// Nil leaves them unmetered. Limits zero uses DefaultLimits.
	Usage  UsageStore
	Limits Limits
	// Switch is the operator's kill switch. Nil leaves everything on.
	Switch *killswitch.Switch
	// Guests enables POST /api/session/guest. GuestTTL zero uses DefaultGuestTTL.
	// GuestGate limits guest sessions per address; nil leaves them unlimited.
	Guests    bool
	GuestTTL  time.Duration
	GuestGate *limit.Window
	// DefaultChatModel and EmbedModel are SCHOLIA_DEFAULT_MODEL and SCHOLIA_EMBED_MODEL.
	// Empty uses the package defaults. See answer.Bound for how defaults combine.
	DefaultChatModel string
	EmbedModel       string
	// WebSearch reports that Tavily is configured. ServerModels reports that server
	// Bedrock is on; the kill switch can still pause it at runtime.
	WebSearch    bool
	ServerModels bool
}

// KeyStore keeps ciphertext. The plaintext key is never stored or returned.
type KeyStore interface {
	PutProviderKey(ctx context.Context, userID, provider string, ciphertext []byte) error
	DeleteProviderKey(ctx context.Context, userID, provider string) error
	ListProviderKeys(ctx context.Context, userID string) ([]string, error)
}

// SettingsStore keeps one user's preferences. A user with none reads the zero value.
type SettingsStore interface {
	GetSettings(ctx context.Context, userID string) (domain.Settings, error)
	PutSettings(ctx context.Context, userID string, settings domain.Settings) error
}

// ChatStore keeps chats under their owner and the messages in each chat.
// GetChat returns store.ErrNotFound for another user's chat.
type ChatStore interface {
	PutChat(ctx context.Context, chat domain.Chat) error
	GetChat(ctx context.Context, userID, chatID string) (domain.Chat, error)
	ListChats(ctx context.Context, userID string) ([]domain.Chat, error)
	DeleteChat(ctx context.Context, userID, chatID string) error
	PutMessage(ctx context.Context, msg domain.Message) error
	ListMessages(ctx context.Context, chatID string) ([]domain.Message, error)
}

// AttachmentStore keeps the attachment rows of one chat.
type AttachmentStore interface {
	PutAttachment(ctx context.Context, att domain.Attachment) error
	GetAttachment(ctx context.Context, chatID, id string) (domain.Attachment, error)
	ListAttachments(ctx context.Context, chatID string) ([]domain.Attachment, error)
	DeleteAttachment(ctx context.Context, chatID, id string) error
}

// ObjectStore reads and removes chat attachment objects in the uploads bucket.
type ObjectStore interface {
	Get(ctx context.Context, bucket, key string) ([]byte, error)
	Delete(ctx context.Context, bucket, key string) error
}

// Answerer streams an answer for one course. The handler flushes each delta.
// An empty req.ChatModel lets the answerer use the course or service default.
type Answerer interface {
	Answer(ctx context.Context, req answer.Request, emit func(delta string) error) (answer.Outcome, error)
}

// SourceStore is the subset of the repository the course and upload routes use.
type SourceStore interface {
	GetCourse(ctx context.Context, id string) (domain.Course, error)
	PutCourse(ctx context.Context, course domain.Course) error
	ListCourses(ctx context.Context) ([]domain.Course, error)
	PutSource(ctx context.Context, source domain.Source) error
	DeleteSource(ctx context.Context, courseID, sourceID string) error
	ListSources(ctx context.Context, courseID string) ([]domain.Source, error)
}

// Presigner signs a PUT to the uploads bucket. The URL is returned to the client and not logged.
// A non-empty tagging is signed into the URL, so the client must send it as x-amz-tagging.
// size is signed as Content-Length, so the object cannot be larger than declared.
type Presigner interface {
	PresignPutSized(ctx context.Context, bucket, key, contentType, tagging string, size int64) (string, error)
}

type server struct {
	opts Options
}

// New returns the API handler with middleware applied.
func New(opts Options) http.Handler {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	s := &server{opts: opts}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("POST /api/session", s.startSession)
	mux.HandleFunc("POST /api/session/confirm", s.confirmSession)
	mux.HandleFunc("POST /api/session/guest", s.startGuest)
	mux.HandleFunc("GET /api/account/usage", s.getUsage)
	mux.HandleFunc("GET /api/models", s.listModels)
	mux.HandleFunc("GET /api/account/settings", s.getSettings)
	mux.HandleFunc("PUT /api/account/settings", s.putSettings)
	mux.HandleFunc("PUT /api/account/keys/{provider}", s.putKey)
	mux.HandleFunc("DELETE /api/account/keys/{provider}", s.deleteKey)
	mux.HandleFunc("GET /api/chats", s.listChats)
	mux.HandleFunc("POST /api/chats", s.createChat)
	mux.HandleFunc("GET /api/chats/{chatID}", s.getChat)
	mux.HandleFunc("PATCH /api/chats/{chatID}", s.renameChat)
	mux.HandleFunc("DELETE /api/chats/{chatID}", s.deleteChat)
	mux.HandleFunc("POST /api/chats/{chatID}/messages", s.postMessage)
	mux.HandleFunc("POST /api/chats/{chatID}/attachments", s.createAttachment)
	mux.HandleFunc("DELETE /api/chats/{chatID}/attachments/{attachmentID}", s.deleteAttachment)
	mux.HandleFunc("GET /api/courses", s.listCourses)
	mux.HandleFunc("POST /api/courses", s.createCourse)
	mux.HandleFunc("GET /api/courses/{courseID}/sources", s.listSources)
	mux.HandleFunc("POST /api/courses/{courseID}/sources", s.createUpload)

	return chain(mux,
		recoverer(opts.Logger),
		accessLog(opts.Logger, opts.Now),
		securityHeaders,
	)
}

type healthResponse struct {
	Status  string    `json:"status"`
	Version string    `json:"version"`
	Commit  string    `json:"commit"`
	Env     string    `json:"env"`
	Time    time.Time `json:"time"`
}

func (s *server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{
		Status:  "ok",
		Version: s.opts.Version,
		Commit:  s.opts.Commit,
		Env:     s.opts.Env,
		Time:    s.opts.Now().UTC(),
	})
}

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorBody{Error: errorDetail{Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	// Encoding failures here mean the client went away; nothing useful to do.
	_ = json.NewEncoder(w).Encode(v)
}
