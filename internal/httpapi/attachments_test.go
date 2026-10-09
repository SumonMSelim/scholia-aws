package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/sumonmselim/scholia-aws/internal/answer"
	"github.com/sumonmselim/scholia-aws/internal/domain"
	"github.com/sumonmselim/scholia-aws/internal/store"
)

type memoryAttachments struct {
	rows map[string]domain.Attachment
	err  error
}

func (m *memoryAttachments) PutAttachment(_ context.Context, att domain.Attachment) error {
	if m.err != nil {
		return m.err
	}
	if err := att.Validate(); err != nil {
		return err
	}
	if m.rows == nil {
		m.rows = map[string]domain.Attachment{}
	}
	m.rows[att.ChatID+"/"+att.ID] = att
	return nil
}

func (m *memoryAttachments) GetAttachment(_ context.Context, chatID, id string) (domain.Attachment, error) {
	if m.err != nil {
		return domain.Attachment{}, m.err
	}
	att, ok := m.rows[chatID+"/"+id]
	if !ok {
		return domain.Attachment{}, store.ErrNotFound
	}
	return att, nil
}

func (m *memoryAttachments) ListAttachments(_ context.Context, chatID string) ([]domain.Attachment, error) {
	if m.err != nil {
		return nil, m.err
	}
	var out []domain.Attachment
	for _, att := range m.rows {
		if att.ChatID == chatID {
			out = append(out, att)
		}
	}
	return out, nil
}

func (m *memoryAttachments) DeleteAttachment(_ context.Context, chatID, id string) error {
	if m.err != nil {
		return m.err
	}
	delete(m.rows, chatID+"/"+id)
	return nil
}

type memoryObjects struct {
	bodies  map[string][]byte
	deleted []string
	delErr  error
}

func (m *memoryObjects) Get(_ context.Context, _, key string) ([]byte, error) {
	body, ok := m.bodies[key]
	if !ok {
		return nil, errors.New("NoSuchKey")
	}
	return body, nil
}

func (m *memoryObjects) Delete(_ context.Context, _, key string) error {
	if m.delErr != nil {
		return m.delErr
	}
	m.deleted = append(m.deleted, key)
	delete(m.bodies, key)
	return nil
}

type attachHarness struct {
	*chatHarness
	atts    *memoryAttachments
	objects *memoryObjects
}

func newAttachHarness(t *testing.T, mutate func(*Options)) *attachHarness {
	t.Helper()
	a := &attachHarness{atts: &memoryAttachments{}, objects: &memoryObjects{bodies: map[string][]byte{}}}
	a.chatHarness = newChatHarness(t, func(o *Options) {
		o.Attachments, o.Objects = a.atts, a.objects
		o.Presign, o.UploadsBucket = &fakePresign{url: "https://uploads.example/put"}, "bucket"
		o.Chats.(*memoryChats).chats = map[string]domain.Chat{
			"owner/h1":    {ID: "h1", OwnerID: "owner", CourseID: "c1", Title: "New chat"},
			"stranger/h9": {ID: "h9", OwnerID: "stranger", CourseID: "c1", Title: "theirs"},
		}
		if mutate != nil {
			mutate(o)
		}
	})
	return a
}

// upload signs one attachment for h1 and stores body at its key.
func (a *attachHarness) upload(t *testing.T, name, contentType string, body []byte) string {
	t.Helper()
	req := `{"name":"` + name + `","content_type":"` + contentType + `","byte_size":` + itoa(len(body)) + `}`
	rec := a.do(t, http.MethodPost, "/api/chats/h1/attachments", req, a.token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status %d body %s", rec.Code, rec.Body)
	}
	var got attachmentResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	att := a.atts.rows["h1/"+got.Attachment.ID]
	if !strings.HasPrefix(att.Key, "chats/h1/attachments/") || got.UploadURL != "https://uploads.example/put" {
		t.Fatalf("attachment %+v response %+v", att, got)
	}
	a.objects.bodies[att.Key] = body
	return got.Attachment.ID
}

func itoa(n int) string {
	raw, _ := json.Marshal(n)
	return string(raw)
}

func TestChatAttachmentFlow(t *testing.T) {
	a := newAttachHarness(t, nil)
	notes := a.upload(t, "draft.md", "text/markdown", []byte("# My answer\nRouters forward."))
	pic := a.upload(t, "board.png", "image/png", append([]byte{0x89, 'P', 'N', 'G'}, make([]byte, 12)...))

	body := `{"question":"Review my answer","attachment_ids":["` + notes + `","` + pic + `"]}`
	rec := a.do(t, http.MethodPost, "/api/chats/h1/messages", body, a.token)
	if rec.Code != http.StatusOK {
		t.Fatalf("message status %d body %s", rec.Code, rec.Body)
	}
	files := a.answers.req.Files
	if len(files) != 2 || files[0].Name != "draft.md" || string(files[0].Body) != "# My answer\nRouters forward." || files[1].ContentType != "image/png" {
		t.Fatalf("files %+v", files)
	}
	if a.answers.req.UserID != "owner" || a.answers.req.CourseID != "c1" {
		t.Fatalf("request %+v", a.answers.req)
	}
	stored := a.chats.messages["h1"][0]
	if len(stored.Attachments) != 2 || stored.Attachments[0].Name != "draft.md" {
		t.Fatalf("stored refs %+v", stored.Attachments)
	}

	// The next turn sees the earlier message, with only the attachment names.
	rec = a.do(t, http.MethodPost, "/api/chats/h1/messages", `{"question":"And now?"}`, a.token)
	if rec.Code != http.StatusOK {
		t.Fatalf("second status %d", rec.Code)
	}
	history := a.answers.req.History
	if len(history) != 2 || history[0].Role != domain.RoleUser || !strings.HasSuffix(history[0].Text, "[attached: draft.md, board.png]") ||
		strings.Contains(history[0].Text, "Routers forward") || history[1].Text != "Routing picks the hop." || len(a.answers.req.Files) != 0 {
		t.Fatalf("history %+v files %d", history, len(a.answers.req.Files))
	}

	rec = a.do(t, http.MethodGet, "/api/chats/h1", "", a.token)
	if !strings.Contains(rec.Body.String(), `"attachments":[{"id":"`+notes+`","name":"draft.md","content_type":"text/markdown"}`) ||
		!strings.Contains(rec.Body.String(), `"attachments":[]`) || !strings.Contains(rec.Body.String(), `"web":[]`) {
		t.Fatalf("chat body %s", rec.Body)
	}

	if rec := a.do(t, http.MethodDelete, "/api/chats/h1/attachments/"+pic, "", a.token); rec.Code != http.StatusNoContent {
		t.Fatalf("delete status %d", rec.Code)
	}
	if _, ok := a.atts.rows["h1/"+pic]; ok || len(a.objects.deleted) != 1 {
		t.Fatalf("attachment kept: %+v %v", a.atts.rows, a.objects.deleted)
	}
	if rec := a.do(t, http.MethodDelete, "/api/chats/h1", "", a.token); rec.Code != http.StatusNoContent {
		t.Fatalf("delete chat status %d", rec.Code)
	}
	if len(a.objects.deleted) != 2 {
		t.Fatalf("chat delete left objects: %v", a.objects.deleted)
	}
}

func TestChatAttachmentRejects(t *testing.T) {
	failing := errors.New("down")
	tests := []struct {
		name   string
		method string
		path   string
		body   string
		token  func(*attachHarness) string
		setup  func(*attachHarness)
		opts   func(*Options)
		status int
	}{
		{"anonymous", http.MethodPost, "/api/chats/h1/attachments", `{"name":"a.md","content_type":"text/markdown","byte_size":3}`, func(*attachHarness) string { return "" }, nil, nil, http.StatusUnauthorized},
		{"not configured", http.MethodPost, "/api/chats/h1/attachments", `{"name":"a.md","content_type":"text/markdown","byte_size":3}`, nil, nil, func(o *Options) { o.Attachments = nil }, http.StatusServiceUnavailable},
		{"audio", http.MethodPost, "/api/chats/h1/attachments", `{"name":"a.mp3","content_type":"audio/mpeg","byte_size":3}`, nil, nil, nil, http.StatusBadRequest},
		{"too large", http.MethodPost, "/api/chats/h1/attachments", `{"name":"a.pdf","content_type":"application/pdf","byte_size":10485761}`, nil, nil, nil, http.StatusBadRequest},
		{"bad body", http.MethodPost, "/api/chats/h1/attachments", `{"name":1}`, nil, nil, nil, http.StatusBadRequest},
		{"someone else's chat", http.MethodPost, "/api/chats/h9/attachments", `{"name":"a.md","content_type":"text/markdown","byte_size":3}`, nil, nil, nil, http.StatusNotFound},
		{"id fails", http.MethodPost, "/api/chats/h1/attachments", `{"name":"a.md","content_type":"text/markdown","byte_size":3}`, nil, nil, func(o *Options) { o.NewID = func() (string, error) { return "", failing } }, http.StatusInternalServerError},
		{"row fails", http.MethodPost, "/api/chats/h1/attachments", `{"name":"a.md","content_type":"text/markdown","byte_size":3}`, nil, func(a *attachHarness) { a.atts.err = failing }, nil, http.StatusInternalServerError},
		{"presign fails", http.MethodPost, "/api/chats/h1/attachments", `{"name":"a.md","content_type":"text/markdown","byte_size":3}`, nil, nil, func(o *Options) { o.Presign = &fakePresign{err: failing} }, http.StatusInternalServerError},
		{"delete missing", http.MethodDelete, "/api/chats/h1/attachments/nope", "", nil, nil, nil, http.StatusNotFound},
		{"delete not configured", http.MethodDelete, "/api/chats/h1/attachments/nope", "", nil, nil, func(o *Options) { o.Attachments = nil }, http.StatusServiceUnavailable},
		{"delete read fails", http.MethodDelete, "/api/chats/h1/attachments/x", "", nil, func(a *attachHarness) { a.atts.err = failing }, nil, http.StatusInternalServerError},
		{"too many ids", http.MethodPost, "/api/chats/h1/messages", `{"question":"q","attachment_ids":["a","b","c","d","e","f"]}`, nil, nil, nil, http.StatusBadRequest},
		{"repeated id", http.MethodPost, "/api/chats/h1/messages", `{"question":"q","attachment_ids":["a","a"]}`, nil, nil, nil, http.StatusBadRequest},
		{"id with a slash", http.MethodPost, "/api/chats/h1/messages", `{"question":"q","attachment_ids":["../x"]}`, nil, nil, nil, http.StatusBadRequest},
		{"unknown id", http.MethodPost, "/api/chats/h1/messages", `{"question":"q","attachment_ids":["nope"]}`, nil, nil, nil, http.StatusNotFound},
		{"ids without storage", http.MethodPost, "/api/chats/h1/messages", `{"question":"q","attachment_ids":["a"]}`, nil, nil, func(o *Options) { o.Objects = nil }, http.StatusServiceUnavailable},
		{"history fails", http.MethodPost, "/api/chats/h1/messages", `{"question":"q"}`, nil, nil, func(o *Options) { o.Chats = listFailChats{o.Chats.(*memoryChats)} }, http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newAttachHarness(t, tt.opts)
			if tt.setup != nil {
				tt.setup(a)
			}
			token := a.token
			if tt.token != nil {
				token = tt.token(a)
			}
			if rec := a.do(t, tt.method, tt.path, tt.body, token); rec.Code != tt.status {
				t.Fatalf("status %d body %s", rec.Code, rec.Body)
			}
		})
	}
}

func TestChatAttachmentMustBeUploadedAndMatch(t *testing.T) {
	a := newAttachHarness(t, nil)
	fake := a.upload(t, "hw.pdf", "application/pdf", []byte("not a pdf"))
	if rec := a.do(t, http.MethodPost, "/api/chats/h1/messages", `{"question":"q","attachment_ids":["`+fake+`"]}`, a.token); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "does not match") {
		t.Fatalf("mismatch status %d body %s", rec.Code, rec.Body)
	}
	missing := a.upload(t, "b.md", "text/markdown", []byte("x"))
	delete(a.objects.bodies, a.atts.rows["h1/"+missing].Key)
	if rec := a.do(t, http.MethodPost, "/api/chats/h1/messages", `{"question":"q","attachment_ids":["`+missing+`"]}`, a.token); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "not uploaded") {
		t.Fatalf("missing status %d body %s", rec.Code, rec.Body)
	}
	if len(a.chats.messages["h1"]) != 0 || a.answers.answers != 0 {
		t.Fatal("a rejected message was stored or answered")
	}
	a.objects.delErr = errors.New("s3 down")
	if rec := a.do(t, http.MethodDelete, "/api/chats/h1/attachments/"+fake, "", a.token); rec.Code != http.StatusInternalServerError {
		t.Fatalf("object delete failure status %d", rec.Code)
	}
	// A chat still goes when its objects cannot be removed.
	if rec := a.do(t, http.MethodDelete, "/api/chats/h1", "", a.token); rec.Code != http.StatusNoContent {
		t.Fatalf("chat delete status %d", rec.Code)
	}
}

func TestChatReplyCarriesWebAndRefusalReason(t *testing.T) {
	tests := []struct {
		name    string
		outcome answer.Outcome
		event   string
		check   func(t *testing.T, msg domain.Message, data string)
	}{
		{"web", answer.Outcome{Web: []domain.WebResult{{Title: "TCP", URL: "https://example.test/tcp"}}}, "citation", func(t *testing.T, msg domain.Message, data string) {
			if len(msg.Web) != 1 || !strings.Contains(data, `"web":[{"title":"TCP","url":"https://example.test/tcp"}]`) || !strings.Contains(data, `"citations":[]`) {
				t.Fatalf("msg %+v data %s", msg, data)
			}
		}},
		{"off topic", answer.Outcome{Refused: true, Reason: domain.RefusalOffTopic, Text: "I can only help with Nets."}, "refusal", func(t *testing.T, msg domain.Message, data string) {
			if msg.RefusalReason != domain.RefusalOffTopic || msg.Refusal != "I can only help with Nets." || data != `{"text":"I can only help with Nets.","reason":"off_topic"}` {
				t.Fatalf("msg %+v data %s", msg, data)
			}
		}},
		{"bare refusal", answer.Outcome{Refused: true}, "refusal", func(t *testing.T, msg domain.Message, data string) {
			if msg.RefusalReason != domain.RefusalNoMaterial || msg.Refusal != answer.RefusalText || !strings.Contains(data, `"reason":"no_material"`) {
				t.Fatalf("msg %+v data %s", msg, data)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newAttachHarness(t, nil)
			out := tt.outcome
			a.answers.outcome = &out
			a.answers.refuse = false
			if out.Refused {
				a.answers.refuse = true
				a.answers.refusal = &out
			}
			rec := a.do(t, http.MethodPost, "/api/chats/h1/messages", `{"question":"q"}`, a.token)
			events := parseSSE(t, rec.Body.String())
			last := events[len(events)-1]
			if last.name != tt.event {
				t.Fatalf("events %+v", events)
			}
			msgs := a.chats.messages["h1"]
			tt.check(t, msgs[len(msgs)-1], last.data)
			rec = a.do(t, http.MethodGet, "/api/chats/h1", "", a.token)
			if out.Refused && !strings.Contains(rec.Body.String(), `"refusal_reason":"`+msgs[1].RefusalReason+`"`) {
				t.Fatalf("chat body %s", rec.Body)
			}
		})
	}
}
