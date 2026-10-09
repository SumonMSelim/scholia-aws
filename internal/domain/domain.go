// Package domain holds the course, source, and chunk records stored for one account.
package domain

import (
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/sumonmselim/scholia-aws/internal/locator"
)

// SourceStatus is the ingestion state of a source.
type SourceStatus string

// Source ingestion states. A failed source carries a reason; the others do not.
const (
	SourceQueued     SourceStatus = "queued"
	SourceProcessing SourceStatus = "processing"
	SourceReady      SourceStatus = "ready"
	SourceFailed     SourceStatus = "failed"
)

// Course is one body of material a person studies.
// OwnerID is the Cognito subject. Public courses can be read and queried without sign-in.
type Course struct {
	ID      string
	Title   string
	OwnerID string
	Public  bool
	// ExpiresAt is the DynamoDB TTL in epoch seconds. Zero keeps the record.
	// Guest records carry it so the table forgets them.
	ExpiresAt int64
}

// Validate reports a missing id or title.
func (c Course) Validate() error {
	if c.ID == "" {
		return errors.New("course: id is required")
	}
	if c.Title == "" {
		return errors.New("course: title is required")
	}
	return nil
}

// Source is one uploaded file belonging to a course.
// FailureReason is set only when Status is failed.
type Source struct {
	ID            string
	CourseID      string
	Name          string
	ContentType   string
	Status        SourceStatus
	FailureReason string
	// YouTubeID is the published recording of a lecture transcript, so a time
	// citation can play the lecture at that moment. Empty for other sources.
	YouTubeID string
	// ExpiresAt is the DynamoDB TTL in epoch seconds. Zero keeps the record.
	// Guest records carry it so the table forgets them.
	ExpiresAt int64
}

// youTubeID is the 11-character video id YouTube uses. The web app builds an
// embed URL from it, so nothing else may reach that URL.
var youTubeID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// Validate checks identity, status, and that a failure reason exists only on failure.
func (s Source) Validate() error {
	if s.ID == "" {
		return errors.New("source: id is required")
	}
	if s.CourseID == "" {
		return errors.New("source: course id is required")
	}
	if s.Name == "" {
		return errors.New("source: name is required")
	}
	if s.YouTubeID != "" && !youTubeID.MatchString(s.YouTubeID) {
		return errors.New("source: youtube id is not valid")
	}
	switch s.Status {
	case SourceQueued, SourceProcessing, SourceReady:
		if s.FailureReason != "" {
			return errors.New("source: failure reason is only set when status is failed")
		}
	case SourceFailed:
		if s.FailureReason == "" {
			return errors.New("source: failure reason is required")
		}
	default:
		return fmt.Errorf("source: status %q is not valid", s.Status)
	}
	return nil
}

// Chunk is a piece of a source. ParentID is empty when this chunk is a parent.
// Children are what retrieval searches; answers quote the parent.
// Breadcrumb is the section path, empty when the document has no headings.
// Locators may be empty until extraction assigns them. A chunk keeps every locator it covers.
type Chunk struct {
	ID         string
	CourseID   string
	SourceID   string
	ParentID   string
	Text       string
	Breadcrumb string
	Locators   []locator.Locator
	// ExpiresAt is the DynamoDB TTL in epoch seconds. Zero keeps the record.
	// Guest records carry it so the table forgets them.
	ExpiresAt int64
}

// Validate checks identity and each locator.
func (c Chunk) Validate() error {
	if c.ID == "" {
		return errors.New("chunk: id is required")
	}
	if c.CourseID == "" {
		return errors.New("chunk: course id is required")
	}
	if c.SourceID == "" {
		return errors.New("chunk: source id is required")
	}
	for i, loc := range c.Locators {
		if err := loc.Validate(); err != nil {
			return fmt.Errorf("chunk: locator %d: %w", i, err)
		}
	}
	return nil
}

// Chat is one conversation a signed-in user holds with one course.
// Model is the chat model chosen when the chat started; empty uses the course default.
type Chat struct {
	ID        string
	OwnerID   string
	CourseID  string
	Title     string
	Model     string
	CreatedAt time.Time
	UpdatedAt time.Time
	// ExpiresAt is the DynamoDB TTL in epoch seconds. Zero keeps the record.
	// Guest records carry it so the table forgets them.
	ExpiresAt int64
}

// Validate reports a missing id, owner, course or title.
func (c Chat) Validate() error {
	switch {
	case c.ID == "":
		return errors.New("chat: id is required")
	case c.OwnerID == "":
		return errors.New("chat: owner id is required")
	case c.CourseID == "":
		return errors.New("chat: course id is required")
	case c.Title == "":
		return errors.New("chat: title is required")
	}
	return nil
}

// Message roles and modes.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
	ModeAnswer    = "answer"
	ModeExplain   = "explain"
)

// Citation is one chunk an assistant message drew on.
type Citation struct {
	ChunkID  string            `json:"chunk_id"`
	SourceID string            `json:"source_id"`
	Locators []locator.Locator `json:"locators"`
	// Section is the heading path of the cited passage and Excerpt its opening,
	// so the reader can see what supports the answer without opening the file.
	Section string `json:"section,omitempty"`
	Excerpt string `json:"excerpt,omitempty"`
}

// Refusal reasons. A refused reply names why the model was not asked or could not answer.
const (
	RefusalNoMaterial = "no_material"
	RefusalOffTopic   = "off_topic"
	RefusalUnsafe     = "unsafe"
)

// WebResult is one page a web search returned for an answer.
type WebResult struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

// AttachmentRef names a chat attachment on the message that sent it.
type AttachmentRef struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ContentType string `json:"content_type"`
}

// Message is one turn in a chat. Refusal is set, with its reason, when the model
// was not asked or the course had nothing to answer from.
type Message struct {
	ID            string
	ChatID        string
	Role          string
	Text          string
	Mode          string
	Citations     []Citation
	Web           []WebResult
	Attachments   []AttachmentRef
	Refusal       string
	RefusalReason string
	CreatedAt     time.Time
	// ExpiresAt is the DynamoDB TTL in epoch seconds. Zero keeps the record.
	// Guest records carry it so the table forgets them.
	ExpiresAt int64
}

// Validate checks identity, role and mode.
func (m Message) Validate() error {
	if m.ID == "" || m.ChatID == "" {
		return errors.New("message: id and chat id are required")
	}
	if m.Role != RoleUser && m.Role != RoleAssistant {
		return fmt.Errorf("message: role %q is not valid", m.Role)
	}
	if m.Mode != ModeAnswer && m.Mode != ModeExplain {
		return fmt.Errorf("message: mode %q is not valid", m.Mode)
	}
	return nil
}

// Settings are one user's preferences. DefaultModel seeds new chats.
type Settings struct {
	DefaultModel string
	// ExpiresAt is the DynamoDB TTL in epoch seconds, set for a guest. Zero keeps it.
	ExpiresAt int64
}

// Attachment is a file sent into one chat. It is read into that chat's prompt
// and never indexed into course knowledge. Key is the uploads-bucket object.
type Attachment struct {
	ID          string
	ChatID      string
	Name        string
	ContentType string
	ByteSize    int64
	Key         string
	CreatedAt   time.Time
	// ExpiresAt is the DynamoDB TTL in epoch seconds. Zero keeps the record.
	// Guest records carry it so the table forgets them.
	ExpiresAt int64
}

// Validate reports a missing id, chat, name, type or key.
func (a Attachment) Validate() error {
	switch {
	case a.ID == "" || a.ChatID == "":
		return errors.New("attachment: id and chat id are required")
	case a.Name == "" || a.ContentType == "":
		return errors.New("attachment: name and content type are required")
	case a.Key == "":
		return errors.New("attachment: key is required")
	case a.ByteSize < 1:
		return errors.New("attachment: byte size must be at least 1")
	}
	return nil
}

// Metered usage kinds. Each is counted per subject for one UTC day.
const (
	UsageMessages = "messages"
	UsageUploads  = "uploads"
	UsageWeb      = "web"
	// UsageGuests and UsageCodes are counted per client address, email or deployment,
	// not per subject: guest sessions started and sign-in codes sent.
	UsageGuests = "guests"
	UsageCodes  = "codes"
)

// Usage is one subject's metered calls for one UTC day.
type Usage struct {
	Messages int
	Uploads  int
	Web      int
}
