package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/sumonmselim/scholia-aws/internal/answer"
	"github.com/sumonmselim/scholia-aws/internal/auth"
	"github.com/sumonmselim/scholia-aws/internal/domain"
	"github.com/sumonmselim/scholia-aws/internal/locator"
	"github.com/sumonmselim/scholia-aws/internal/store"
)

type memorySettings struct {
	byUser map[string]domain.Settings
	err    error
}

func (m *memorySettings) GetSettings(_ context.Context, userID string) (domain.Settings, error) {
	if m.err != nil {
		return domain.Settings{}, m.err
	}
	return m.byUser[userID], nil
}

func (m *memorySettings) PutSettings(_ context.Context, userID string, settings domain.Settings) error {
	if m.err != nil {
		return m.err
	}
	if m.byUser == nil {
		m.byUser = map[string]domain.Settings{}
	}
	m.byUser[userID] = settings
	return nil
}

type memoryChats struct {
	chats    map[string]domain.Chat
	messages map[string][]domain.Message
	err      error
	// failMessages fails that PutMessage call, counting from 1. Zero never fails.
	failMessages int
	putMessages  int
}

func (m *memoryChats) PutChat(_ context.Context, chat domain.Chat) error {
	if m.err != nil {
		return m.err
	}
	if m.chats == nil {
		m.chats = map[string]domain.Chat{}
	}
	m.chats[chat.OwnerID+"/"+chat.ID] = chat
	return nil
}

func (m *memoryChats) GetChat(_ context.Context, userID, chatID string) (domain.Chat, error) {
	if m.err != nil {
		return domain.Chat{}, m.err
	}
	chat, ok := m.chats[userID+"/"+chatID]
	if !ok {
		return domain.Chat{}, store.ErrNotFound
	}
	return chat, nil
}

func (m *memoryChats) ListChats(_ context.Context, userID string) ([]domain.Chat, error) {
	if m.err != nil {
		return nil, m.err
	}
	out := []domain.Chat{}
	for _, chat := range m.chats {
		if chat.OwnerID == userID {
			out = append(out, chat)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

func (m *memoryChats) DeleteChat(_ context.Context, userID, chatID string) error {
	if m.err != nil {
		return m.err
	}
	delete(m.chats, userID+"/"+chatID)
	delete(m.messages, chatID)
	return nil
}

func (m *memoryChats) PutMessage(_ context.Context, msg domain.Message) error {
	if m.err != nil {
		return m.err
	}
	m.putMessages++
	if m.putMessages == m.failMessages {
		return errors.New("table down")
	}
	if err := msg.Validate(); err != nil {
		return err
	}
	if m.messages == nil {
		m.messages = map[string][]domain.Message{}
	}
	m.messages[msg.ChatID] = append(m.messages[msg.ChatID], msg)
	return nil
}

func (m *memoryChats) ListMessages(_ context.Context, chatID string) ([]domain.Message, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.messages[chatID], nil
}

// recordAnswerer streams a fixed reply and records the model it was asked to use.
type recordAnswerer struct {
	model   string
	req     answer.Request
	refuse  bool
	refusal *answer.Outcome // replaces the bare refusal when set
	outcome *answer.Outcome // replaces the default reply when set
	failAt  int             // 1 fails before any delta, 2 fails after one
	answers int
}

func (a *recordAnswerer) Answer(_ context.Context, req answer.Request, emit func(string) error) (answer.Outcome, error) {
	a.model = req.ChatModel
	a.req = req
	a.answers++
	if a.failAt == 1 {
		return answer.Outcome{}, errors.New("model down")
	}
	if a.refuse {
		if a.refusal != nil {
			return *a.refusal, nil
		}
		return answer.Outcome{Refused: true}, nil
	}
	if err := emit("Routing "); err != nil {
		return answer.Outcome{}, err
	}
	if a.failAt == 2 {
		return answer.Outcome{}, errors.New("model down")
	}
	if err := emit("picks the hop."); err != nil {
		return answer.Outcome{}, err
	}
	if a.outcome != nil {
		return *a.outcome, nil
	}
	return answer.Outcome{Citations: []answer.Citation{{ChunkID: "k1", SourceID: "s1", Locators: []locator.Locator{{Kind: locator.KindSlide, Slide: 2}}}}}, nil
}

type chatHarness struct {
	h        http.Handler
	chats    *memoryChats
	settings *memorySettings
	answers  *recordAnswerer
	tutor    *recordAnswerer
	token    string
	other    string
	clock    *time.Time
}

func newChatHarness(t *testing.T, mutate func(*Options)) *chatHarness {
	t.Helper()
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	c := &chatHarness{
		chats: &memoryChats{}, settings: &memorySettings{},
		answers: &recordAnswerer{}, tutor: &recordAnswerer{}, clock: &now,
	}
	var err error
	if c.token, err = auth.Sign(sessionSecret, "owner", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if c.other, err = auth.Sign(sessionSecret, "stranger", now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	next := 0
	opts := Options{
		Sessions: &auth.Fake{}, SessionSecret: sessionSecret,
		Now:     func() time.Time { return *c.clock },
		Sources: &fakeSources{course: domain.Course{ID: "c1", Title: "Nets", OwnerID: "owner"}},
		NewID: func() (string, error) {
			next++
			return "id" + string(rune('a'+next)), nil
		},
		Chats: c.chats, Settings: c.settings, Answers: c.answers, Tutor: c.tutor,
	}
	if mutate != nil {
		mutate(&opts)
	}
	c.h = New(opts)
	return c
}

func (c *chatHarness) do(t *testing.T, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set(TokenHeader, token)
	}
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	return rec
}

func TestChatLifecycle(t *testing.T) {
	c := newChatHarness(t, nil)
	c.settings.byUser = map[string]domain.Settings{"owner": {DefaultModel: "gpt-4.1-mini"}}

	rec := c.do(t, http.MethodPost, "/api/chats", `{"course_id":"c1","model":""}`, c.token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status %d body %s", rec.Code, rec.Body)
	}
	var created chatBody
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Title != "New chat" || created.CourseID != "c1" || created.Model != "gpt-4.1-mini" || created.ID == "" {
		t.Fatalf("created %+v", created)
	}
	if !strings.Contains(rec.Body.String(), `"created_at":"2026-09-28T12:00:00Z"`) {
		t.Fatalf("times not RFC3339: %s", rec.Body)
	}

	rec = c.do(t, http.MethodPost, "/api/chats/"+created.ID+"/messages", `{"question":"  How does routing pick the next hop?  ","mode":"answer"}`, c.token)
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("message status %d body %s", rec.Code, rec.Body)
	}
	events := parseSSE(t, rec.Body.String())
	if len(events) != 3 || events[0].name != "delta" || events[2].name != "citation" {
		t.Fatalf("events %+v", events)
	}
	if c.answers.model != "gpt-4.1-mini" || c.tutor.answers != 0 {
		t.Fatalf("answer model %q tutor calls %d", c.answers.model, c.tutor.answers)
	}

	*c.clock = c.clock.Add(time.Minute)
	rec = c.do(t, http.MethodPost, "/api/chats/"+created.ID+"/messages", `{"question":"Explain it","mode":"explain"}`, c.token)
	if rec.Code != http.StatusOK || c.tutor.answers != 1 {
		t.Fatalf("explain status %d tutor %d", rec.Code, c.tutor.answers)
	}

	rec = c.do(t, http.MethodGet, "/api/chats/"+created.ID, "", c.token)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status %d", rec.Code)
	}
	var got chatResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Chat.Title != "How does routing pick the next hop?" || !got.Chat.UpdatedAt.After(got.Chat.CreatedAt) {
		t.Fatalf("chat %+v", got.Chat)
	}
	if len(got.Messages) != 4 {
		t.Fatalf("messages %+v", got.Messages)
	}
	first, reply := got.Messages[0], got.Messages[1]
	if first.Role != "user" || first.Text != "How does routing pick the next hop?" || first.Mode != "answer" || first.Citations == nil {
		t.Fatalf("user message %+v", first)
	}
	if reply.Role != "assistant" || reply.Text != "Routing picks the hop." || len(reply.Citations) != 1 || reply.Citations[0].Locators[0].Slide != 2 {
		t.Fatalf("reply %+v", reply)
	}
	if !reply.CreatedAt.After(first.CreatedAt) {
		t.Fatalf("reply does not sort after question: %v %v", reply.CreatedAt, first.CreatedAt)
	}
	if got.Messages[2].Mode != "explain" {
		t.Fatalf("explain message %+v", got.Messages[2])
	}

	rec = c.do(t, http.MethodPatch, "/api/chats/"+created.ID, `{"title":"  Routing  "}`, c.token)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"title":"Routing"`) {
		t.Fatalf("rename status %d body %s", rec.Code, rec.Body)
	}

	second := c.do(t, http.MethodPost, "/api/chats", `{"course_id":"c1","model":"gpt-4.1"}`, c.token)
	if second.Code != http.StatusCreated || !strings.Contains(second.Body.String(), `"model":"gpt-4.1"`) {
		t.Fatalf("second status %d body %s", second.Code, second.Body)
	}
	rec = c.do(t, http.MethodGet, "/api/chats", "", c.token)
	var list chatListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || len(list.Chats) != 2 {
		t.Fatalf("list status %d body %s", rec.Code, rec.Body)
	}
	if strangers := c.do(t, http.MethodGet, "/api/chats", "", c.other); !strings.Contains(strangers.Body.String(), `"chats":[]`) {
		t.Fatalf("stranger sees chats: %s", strangers.Body)
	}
	if peek := c.do(t, http.MethodGet, "/api/chats/"+created.ID, "", c.other); peek.Code != http.StatusNotFound {
		t.Fatalf("stranger read status %d", peek.Code)
	}

	rec = c.do(t, http.MethodDelete, "/api/chats/"+created.ID, "", c.token)
	if rec.Code != http.StatusNoContent || len(c.chats.messages[created.ID]) != 0 {
		t.Fatalf("delete status %d messages %v", rec.Code, c.chats.messages)
	}
	if gone := c.do(t, http.MethodGet, "/api/chats/"+created.ID, "", c.token); gone.Code != http.StatusNotFound {
		t.Fatalf("deleted chat status %d", gone.Code)
	}
}

func TestChatRefusalIsStored(t *testing.T) {
	c := newChatHarness(t, nil)
	c.answers.refuse = true
	c.chats.chats = map[string]domain.Chat{"owner/h1": {ID: "h1", OwnerID: "owner", CourseID: "c1", Title: "Kept", Model: "gpt-4.1"}}
	rec := c.do(t, http.MethodPost, "/api/chats/h1/messages", `{"question":"`+strings.Repeat("word ", 30)+`"}`, c.token)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "event: refusal") {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	msgs := c.chats.messages["h1"]
	if len(msgs) != 2 || msgs[1].Refusal != answer.RefusalText || msgs[1].Text != "" || msgs[0].Mode != "answer" {
		t.Fatalf("messages %+v", msgs)
	}
	if c.chats.chats["owner/h1"].Title != "Kept" || c.answers.model != "gpt-4.1" {
		t.Fatalf("chat %+v model %q", c.chats.chats["owner/h1"], c.answers.model)
	}
}

func TestChatRefusalAfterDeltasDropsTheText(t *testing.T) {
	// A Bedrock guardrail streams its own blocked message before the stop reason.
	c := newChatHarness(t, nil)
	c.answers.outcome = &answer.Outcome{Refused: true, Reason: domain.RefusalUnsafe, Text: "I can't help with that."}
	c.chats.chats = map[string]domain.Chat{"owner/h1": {ID: "h1", OwnerID: "owner", CourseID: "c1", Title: "Kept", Model: "gpt-4.1"}}
	rec := c.do(t, http.MethodPost, "/api/chats/h1/messages", `{"question":"Quiz me"}`, c.token)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "event: refusal") {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	msgs := c.chats.messages["h1"]
	if len(msgs) != 2 || msgs[1].Text != "" || msgs[1].Refusal != "I can't help with that." || msgs[1].RefusalReason != domain.RefusalUnsafe {
		t.Fatalf("messages %+v", msgs)
	}
}

func TestAutoTitle(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"short  question\n", "short question"},
		{strings.Repeat("a", 60), strings.Repeat("a", 60)},
		{strings.Repeat("é", 70), strings.Repeat("é", 59) + "…"},
	}
	for _, tt := range tests {
		if got := autoTitle(tt.in); got != tt.want {
			t.Fatalf("autoTitle(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestChatRejects(t *testing.T) {
	failing := errors.New("table down")
	seeded := func(o *Options) {
		o.Chats.(*memoryChats).chats = map[string]domain.Chat{"owner/h1": {ID: "h1", OwnerID: "owner", CourseID: "c1", Title: "New chat"}}
	}
	tests := []struct {
		name   string
		method string
		path   string
		body   string
		anon   bool
		opts   func(*Options)
		status int
	}{
		{"no store", http.MethodGet, "/api/chats", "", false, func(o *Options) { o.Chats = nil }, http.StatusServiceUnavailable},
		{"anonymous list", http.MethodGet, "/api/chats", "", true, nil, http.StatusUnauthorized},
		{"anonymous create", http.MethodPost, "/api/chats", `{"course_id":"c1"}`, true, nil, http.StatusUnauthorized},
		{"list fails", http.MethodGet, "/api/chats", "", false, func(o *Options) { o.Chats = &memoryChats{err: failing} }, http.StatusInternalServerError},
		{"no courses", http.MethodPost, "/api/chats", `{"course_id":"c1"}`, false, func(o *Options) { o.Sources = nil }, http.StatusServiceUnavailable},
		{"unknown field", http.MethodPost, "/api/chats", `{"course_id":"c1","mode":"x"}`, false, nil, http.StatusBadRequest},
		{"missing course id", http.MethodPost, "/api/chats", `{"model":"gpt-4.1"}`, false, nil, http.StatusBadRequest},
		{"bad model", http.MethodPost, "/api/chats", `{"course_id":"c1","model":"a b"}`, false, nil, http.StatusBadRequest},
		{"course missing", http.MethodPost, "/api/chats", `{"course_id":"c1"}`, false, func(o *Options) { o.Sources = &fakeSources{} }, http.StatusNotFound},
		{"course read fails", http.MethodPost, "/api/chats", `{"course_id":"c1"}`, false, func(o *Options) { o.Sources = &fakeSources{getErr: failing} }, http.StatusInternalServerError},
		{"private course", http.MethodPost, "/api/chats", `{"course_id":"c1"}`, false, func(o *Options) {
			o.Sources = &fakeSources{course: domain.Course{ID: "c1", Title: "Nets", OwnerID: "someone"}}
		}, http.StatusForbidden},
		{"settings fail", http.MethodPost, "/api/chats", `{"course_id":"c1"}`, false, func(o *Options) { o.Settings = &memorySettings{err: failing} }, http.StatusInternalServerError},
		{"id fails", http.MethodPost, "/api/chats", `{"course_id":"c1"}`, false, func(o *Options) { o.NewID = func() (string, error) { return "", failing } }, http.StatusInternalServerError},
		{"put fails", http.MethodPost, "/api/chats", `{"course_id":"c1"}`, false, func(o *Options) { o.Chats = &memoryChats{err: failing} }, http.StatusInternalServerError},
		{"get missing", http.MethodGet, "/api/chats/nope", "", false, nil, http.StatusNotFound},
		{"get fails", http.MethodGet, "/api/chats/h1", "", false, func(o *Options) { o.Chats = &memoryChats{err: failing} }, http.StatusInternalServerError},
		{"rename empty", http.MethodPatch, "/api/chats/h1", `{"title":"  "}`, false, seeded, http.StatusBadRequest},
		{"rename long", http.MethodPatch, "/api/chats/h1", `{"title":"` + strings.Repeat("x", 121) + `"}`, false, seeded, http.StatusBadRequest},
		{"rename bad body", http.MethodPatch, "/api/chats/h1", `{`, false, seeded, http.StatusBadRequest},
		{"rename missing", http.MethodPatch, "/api/chats/nope", `{"title":"x"}`, false, nil, http.StatusNotFound},
		{"delete missing", http.MethodDelete, "/api/chats/nope", "", false, nil, http.StatusNotFound},
		{"message bad mode", http.MethodPost, "/api/chats/h1/messages", `{"question":"q","mode":"solve"}`, false, seeded, http.StatusBadRequest},
		{"message empty", http.MethodPost, "/api/chats/h1/messages", `{"question":" "}`, false, seeded, http.StatusBadRequest},
		{"message unknown field", http.MethodPost, "/api/chats/h1/messages", `{"question":"q","model":"x"}`, false, seeded, http.StatusBadRequest},
		{"message too large", http.MethodPost, "/api/chats/h1/messages", `{"question":"` + strings.Repeat("q", maxQuestionBytes) + `"}`, false, seeded, http.StatusBadRequest},
		{"message no answers", http.MethodPost, "/api/chats/h1/messages", `{"question":"q"}`, false, func(o *Options) { seeded(o); o.Answers = nil }, http.StatusServiceUnavailable},
		{"message no tutor", http.MethodPost, "/api/chats/h1/messages", `{"question":"q","mode":"explain"}`, false, func(o *Options) { seeded(o); o.Tutor = nil }, http.StatusServiceUnavailable},
		{"message missing chat", http.MethodPost, "/api/chats/nope/messages", `{"question":"q"}`, false, nil, http.StatusNotFound},
		{"message course gone", http.MethodPost, "/api/chats/h1/messages", `{"question":"q"}`, false, func(o *Options) { seeded(o); o.Sources = &fakeSources{} }, http.StatusNotFound},
		{"message store fails", http.MethodPost, "/api/chats/h1/messages", `{"question":"q"}`, false, func(o *Options) { seeded(o); o.Chats.(*memoryChats).failMessages = 1 }, http.StatusInternalServerError},
		{"message id fails", http.MethodPost, "/api/chats/h1/messages", `{"question":"q"}`, false, func(o *Options) { seeded(o); o.NewID = func() (string, error) { return "", failing } }, http.StatusInternalServerError},
		{"answer fails", http.MethodPost, "/api/chats/h1/messages", `{"question":"q"}`, false, func(o *Options) { seeded(o); o.Answers = &recordAnswerer{failAt: 1} }, http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newChatHarness(t, tt.opts)
			token := c.token
			if tt.anon {
				token = ""
			}
			rec := c.do(t, tt.method, tt.path, tt.body, token)
			if rec.Code != tt.status {
				t.Fatalf("status %d body %s", rec.Code, rec.Body)
			}
		})
	}
}

func TestChatStreamFailuresKeepTheQuestion(t *testing.T) {
	tests := []struct {
		name     string
		failAt   int
		failPut  int
		wantMsgs int
		wantBody string
	}{
		{"error after first delta", 2, 0, 1, "event: error"},
		{"reply store fails", 0, 2, 1, "event: citation"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			c := newChatHarness(t, func(o *Options) {
				o.Answers = &recordAnswerer{failAt: tt.failAt}
				o.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
				chats := o.Chats.(*memoryChats)
				chats.failMessages = tt.failPut
				chats.chats = map[string]domain.Chat{"owner/h1": {ID: "h1", OwnerID: "owner", CourseID: "c1", Title: "New chat"}}
			})
			rec := c.do(t, http.MethodPost, "/api/chats/h1/messages", `{"question":"q"}`, c.token)
			if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Fatalf("status %d body %s", rec.Code, rec.Body)
			}
			if got := len(c.chats.messages["h1"]); got != tt.wantMsgs {
				t.Fatalf("messages %d, want %d", got, tt.wantMsgs)
			}
			// A failure after the first byte only reaches the client as an event, so the log is the one record of why.
			if tt.failAt == 2 && !strings.Contains(logs.String(), `"err":"model down"`) {
				t.Fatalf("mid-stream failure not logged: %s", logs.String())
			}
		})
	}
}

func TestChatDeleteAndRenameStoreFailures(t *testing.T) {
	for _, method := range []string{http.MethodDelete, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			c := newChatHarness(t, nil)
			c.chats.chats = map[string]domain.Chat{"owner/h1": {ID: "h1", OwnerID: "owner", CourseID: "c1", Title: "t"}}
			// Reads succeed, writes fail.
			c.h = New(Options{
				Sessions: &auth.Fake{}, SessionSecret: sessionSecret, Now: func() time.Time { return *c.clock },
				Chats: writeFailChats{c.chats},
			})
			rec := c.do(t, method, "/api/chats/h1", `{"title":"x"}`, c.token)
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("status %d body %s", rec.Code, rec.Body)
			}
		})
	}
}

type writeFailChats struct{ *memoryChats }

func (writeFailChats) PutChat(context.Context, domain.Chat) error       { return errors.New("down") }
func (writeFailChats) DeleteChat(context.Context, string, string) error { return errors.New("down") }

func TestListMessagesFailure(t *testing.T) {
	c := newChatHarness(t, nil)
	c.chats.chats = map[string]domain.Chat{"owner/h1": {ID: "h1", OwnerID: "owner", CourseID: "c1", Title: "t"}}
	c.h = New(Options{
		Sessions: &auth.Fake{}, SessionSecret: sessionSecret, Now: func() time.Time { return *c.clock },
		Chats: listFailChats{c.chats},
	})
	if rec := c.do(t, http.MethodGet, "/api/chats/h1", "", c.token); rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d", rec.Code)
	}
}

type listFailChats struct{ *memoryChats }

func (listFailChats) ListMessages(context.Context, string) ([]domain.Message, error) {
	return nil, errors.New("down")
}

func TestModelCatalog(t *testing.T) {
	rec := httptest.NewRecorder()
	New(Options{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/models", nil))
	var body modelListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || len(body.Models) == 0 {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	seen := map[string]bool{}
	for _, m := range body.Models {
		if !modelID(m.ID) || m.ID == "" || m.Label == "" || (m.Provider != "openai" && m.Provider != "bedrock") || seen[m.ID] {
			t.Fatalf("bad catalog row %+v", m)
		}
		seen[m.ID] = true
	}
	if !seen["us.amazon.nova-2-lite-v1:0"] {
		t.Fatal("default model is not in the catalog")
	}
	kinds := map[string]string{}
	for _, m := range body.Models {
		kinds[m.ID] = m.Kind
	}
	if kinds["us.amazon.nova-2-lite-v1:0"] != "chat" || kinds["amazon.titan-embed-text-v2:0"] != "embedding" {
		t.Fatalf("kinds = %v", kinds)
	}
	if body.Defaults != (defaultsBody{ChatModel: "us.amazon.nova-2-lite-v1:0", EmbedModel: "amazon.titan-embed-text-v2:0"}) {
		t.Fatalf("empty options defaults = %+v", body.Defaults)
	}
	if !strings.Contains(rec.Body.String(), `"defaults":{"chat_model":"us.amazon.nova-2-lite-v1:0","embed_model":"amazon.titan-embed-text-v2:0","web_search":false,"server_models":false}`) {
		t.Fatalf("defaults shape: %s", rec.Body)
	}
}

func TestModelDefaults(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		want defaultsBody
	}{
		{"configured", Options{DefaultChatModel: "us.amazon.nova-pro-v1:0", EmbedModel: "amazon.titan-embed-text-v1", WebSearch: true, ServerModels: true},
			defaultsBody{ChatModel: "us.amazon.nova-pro-v1:0", EmbedModel: "amazon.titan-embed-text-v1", WebSearch: true, ServerModels: true}},
		{"paused", Options{ServerModels: true, Switch: paused(`{"server_models":false}`)},
			defaultsBody{ChatModel: "us.amazon.nova-2-lite-v1:0", EmbedModel: "amazon.titan-embed-text-v2:0"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			New(tt.opts).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/models", nil))
			var body modelListResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Defaults != tt.want {
				t.Fatalf("defaults = %+v", body.Defaults)
			}
		})
	}
}

func TestNewChatTakesTheUserDefault(t *testing.T) {
	c := newChatHarness(t, func(o *Options) {
		o.Sources = &fakeSources{course: domain.Course{ID: "c1", Title: "Nets", OwnerID: "owner"}}
	})
	c.settings.byUser = map[string]domain.Settings{"owner": {DefaultModel: "gpt-4.1-mini"}}
	rec := c.do(t, http.MethodPost, "/api/chats", `{"course_id":"c1","model":""}`, c.token)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"model":"gpt-4.1-mini"`) {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
}
