package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sumonmselim/scholia-aws/internal/answer"
	"github.com/sumonmselim/scholia-aws/internal/auth"
	"github.com/sumonmselim/scholia-aws/internal/domain"
	"github.com/sumonmselim/scholia-aws/internal/killswitch"
	"github.com/sumonmselim/scholia-aws/internal/limit"
	"github.com/sumonmselim/scholia-aws/internal/store"
)

// memUsage mirrors the store's conditional count.
type memUsage struct {
	counts map[string]int
	err    error
}

func (m *memUsage) AddUsage(_ context.Context, subject, kind string, _ time.Time, max int) (int, error) {
	if m.err != nil {
		return 0, m.err
	}
	if m.counts == nil {
		m.counts = map[string]int{}
	}
	key := subject + "/" + kind
	if m.counts[key] >= max {
		return max, store.ErrQuota
	}
	m.counts[key]++
	return m.counts[key], nil
}

func (m *memUsage) GetUsage(_ context.Context, subject string, _ time.Time) (domain.Usage, error) {
	if m.err != nil {
		return domain.Usage{}, m.err
	}
	return domain.Usage{
		Messages: m.counts[subject+"/"+domain.UsageMessages],
		Uploads:  m.counts[subject+"/"+domain.UsageUploads],
		Web:      m.counts[subject+"/"+domain.UsageWeb],
	}, nil
}

type staticParams string

func (p staticParams) GetParameter(context.Context, string) (string, error) { return string(p), nil }

func paused(flags string) *killswitch.Switch {
	return &killswitch.Switch{Name: "/scholia/switch", Params: staticParams(flags)}
}

// pausedAnswerer fails the way the key resolver does when server models are paused.
type pausedAnswerer struct{}

func (pausedAnswerer) Answer(context.Context, answer.Request, func(string) error) (answer.Outcome, error) {
	return answer.Outcome{}, killswitch.ErrPaused
}

func guestToken(t *testing.T, now time.Time) (string, string) {
	t.Helper()
	subject, err := auth.NewGuestSubject()
	if err != nil {
		t.Fatal(err)
	}
	token, err := auth.Sign(sessionSecret, subject, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return subject, token
}

func errorCode(t *testing.T, body string) string {
	t.Helper()
	var out errorBody
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("error body %q: %v", body, err)
	}
	return out.Error.Code
}

func TestStartGuest(t *testing.T) {
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	base := func(mutate func(*Options)) *chatHarness {
		return newChatHarness(t, func(o *Options) {
			o.Now = func() time.Time { return now }
			o.Guests = true
			o.GuestGate = limit.NewWindow(2, time.Hour)
			if mutate != nil {
				mutate(o)
			}
		})
	}

	c := base(nil)
	rec := c.do(t, http.MethodPost, "/api/session/guest", "", "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	var out guestResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	subject, err := auth.Verify(sessionSecret, out.Token, now)
	if err != nil || !auth.IsGuest(subject) || !out.Guest || !out.ExpiresAt.Equal(now.Add(DefaultGuestTTL)) {
		t.Fatalf("guest = %+v subject %q err %v", out, subject, err)
	}
	if _, err := auth.Verify(sessionSecret, out.Token, now.Add(DefaultGuestTTL)); err == nil {
		t.Fatal("guest token outlived its TTL")
	}
	if rec := c.do(t, http.MethodPost, "/api/session/guest", "", ""); rec.Code != http.StatusCreated {
		t.Fatalf("second guest status %d", rec.Code)
	}
	if rec := c.do(t, http.MethodPost, "/api/session/guest", "", ""); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("third guest from one address: status %d", rec.Code)
	}

	tests := []struct {
		name   string
		mutate func(*Options)
		status int
		code   string
	}{
		{"guests off", func(o *Options) { o.Guests = false }, http.StatusServiceUnavailable, "unavailable"},
		{"sign-in off", func(o *Options) { o.Sessions = nil }, http.StatusServiceUnavailable, "unavailable"},
		{"paused", func(o *Options) { o.Switch = paused(`{"guests":false}`) }, http.StatusServiceUnavailable, "paused"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := base(tt.mutate).do(t, http.MethodPost, "/api/session/guest", "", "")
			if rec.Code != tt.status || errorCode(t, rec.Body.String()) != tt.code {
				t.Fatalf("status %d body %s", rec.Code, rec.Body)
			}
		})
	}
}

func TestGuestRecordsExpire(t *testing.T) {
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	subject, token := guestToken(t, now)
	sources := &fakeSources{}
	presign := &fakePresign{url: "https://uploads.example/put"}
	atts := &memoryAttachments{}
	c := newChatHarness(t, func(o *Options) {
		o.Sources, o.Presign, o.UploadsBucket = sources, presign, "bucket"
		o.Attachments, o.Objects = atts, &memoryObjects{}
		o.GuestTTL = 24 * time.Hour
	})
	*c.clock = now
	want := now.Add(24 * time.Hour).Unix()

	rec := c.do(t, http.MethodPost, "/api/courses", `{"title":"My notes","public":true}`, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create course %d %s", rec.Code, rec.Body)
	}
	if sources.course.Public || sources.course.OwnerID != subject || sources.course.ExpiresAt != want {
		t.Fatalf("guest course = %+v", sources.course)
	}

	rec = c.do(t, http.MethodPost, "/api/courses/"+sources.course.ID+"/sources", `{"name":"a.md","content_type":"text/markdown","byte_size":4}`, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload %d %s", rec.Code, rec.Body)
	}
	var ticket uploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &ticket); err != nil {
		t.Fatal(err)
	}
	if ticket.Headers["x-amz-tagging"] != "guest=true" || presign.tagging != "guest=true" {
		t.Fatalf("guest upload headers %+v tagging %q", ticket.Headers, presign.tagging)
	}
	for _, src := range sources.sources {
		if src.ExpiresAt != want {
			t.Fatalf("guest source = %+v", src)
		}
	}

	rec = c.do(t, http.MethodPost, "/api/chats", `{"course_id":"`+sources.course.ID+`","model":""}`, token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create chat %d %s", rec.Code, rec.Body)
	}
	var chat chatBody
	if err := json.Unmarshal(rec.Body.Bytes(), &chat); err != nil {
		t.Fatal(err)
	}
	if got := c.chats.chats[subject+"/"+chat.ID]; got.ExpiresAt != want {
		t.Fatalf("guest chat = %+v", got)
	}
	rec = c.do(t, http.MethodPost, "/api/chats/"+chat.ID+"/attachments", `{"name":"hw.md","content_type":"text/markdown","byte_size":4}`, token)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"x-amz-tagging":"guest=true"`) {
		t.Fatalf("attachment %d %s", rec.Code, rec.Body)
	}
	for _, att := range atts.rows {
		if att.ExpiresAt != want {
			t.Fatalf("guest attachment = %+v", att)
		}
	}
	if rec := c.do(t, http.MethodPost, "/api/chats/"+chat.ID+"/messages", `{"question":"What is TCP?"}`, token); rec.Code != http.StatusOK {
		t.Fatalf("message %d %s", rec.Code, rec.Body)
	}
	msgs := c.chats.messages[chat.ID]
	if len(msgs) != 2 || msgs[0].ExpiresAt != want || msgs[1].ExpiresAt != want {
		t.Fatalf("guest messages = %+v", msgs)
	}
	if c.answers.req.Meter != subject {
		t.Fatalf("meter = %q", c.answers.req.Meter)
	}

	rec = c.do(t, http.MethodPut, "/api/account/settings", `{"default_model":""}`, token)
	if rec.Code != http.StatusOK || c.settings.byUser[subject].ExpiresAt != want {
		t.Fatalf("guest settings %d %+v", rec.Code, c.settings.byUser[subject])
	}
}

func TestSignedInRecordsKeep(t *testing.T) {
	sources := &fakeSources{course: domain.Course{ID: "c1", Title: "Nets", OwnerID: "owner"}}
	presign := &fakePresign{url: "https://uploads.example/put"}
	c := newChatHarness(t, func(o *Options) {
		o.Sources, o.Presign, o.UploadsBucket = sources, presign, "bucket"
	})
	rec := c.do(t, http.MethodPost, "/api/courses/c1/sources", `{"name":"a.md","content_type":"text/markdown","byte_size":4}`, c.token)
	if rec.Code != http.StatusCreated || strings.Contains(rec.Body.String(), "headers") || presign.tagging != "" {
		t.Fatalf("upload %d %s tagging %q", rec.Code, rec.Body, presign.tagging)
	}
	for _, src := range sources.sources {
		if src.ExpiresAt != 0 {
			t.Fatalf("user source expires: %+v", src)
		}
	}
	rec = c.do(t, http.MethodPost, "/api/courses", `{"title":"Shared","public":true}`, c.token)
	if rec.Code != http.StatusCreated || !sources.course.Public || sources.course.ExpiresAt != 0 {
		t.Fatalf("user course %d %+v", rec.Code, sources.course)
	}
}

func TestGuestCannotStoreKeys(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	_, token := guestToken(t, now)
	c := newChatHarness(t, func(o *Options) {
		o.Keys, o.Box = &memoryKeys{}, &memoryBox{}
	})
	rec := c.do(t, http.MethodPut, "/api/account/keys/openai", `{"api_key":"sk-guest"}`, token)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("guest key: status %d body %s", rec.Code, rec.Body)
	}
}

func TestDailyQuota(t *testing.T) {
	usage := &memUsage{}
	limits := Limits{User: Quota{Messages: 2, Uploads: 1, Web: 1}, Guest: Quota{Messages: 1, Uploads: 1, Web: 1}}
	sources := &fakeSources{course: domain.Course{ID: "c1", Title: "Nets", OwnerID: "owner", Public: true}}
	c := newChatHarness(t, func(o *Options) {
		o.Usage, o.Limits = usage, limits
		o.Sources, o.Presign, o.UploadsBucket = sources, &fakePresign{url: "https://u"}, "bucket"
	})
	rec := c.do(t, http.MethodPost, "/api/chats", `{"course_id":"c1","model":""}`, c.token)
	var chat chatBody
	if err := json.Unmarshal(rec.Body.Bytes(), &chat); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if rec := c.do(t, http.MethodPost, "/api/chats/"+chat.ID+"/messages", `{"question":"q"}`, c.token); rec.Code != http.StatusOK {
			t.Fatalf("message %d: %d %s", i, rec.Code, rec.Body)
		}
	}
	rec = c.do(t, http.MethodPost, "/api/chats/"+chat.ID+"/messages", `{"question":"q"}`, c.token)
	if rec.Code != http.StatusTooManyRequests || errorCode(t, rec.Body.String()) != "quota" ||
		!strings.Contains(rec.Body.String(), "today's message allowance. It resets at 00:00 UTC.") {
		t.Fatalf("over quota: %d %s", rec.Code, rec.Body)
	}
	if c.answers.answers != 2 {
		t.Fatalf("answers after quota = %d", c.answers.answers)
	}

	upload := `{"name":"a.md","content_type":"text/markdown","byte_size":4}`
	sources.course.Public = false
	if rec := c.do(t, http.MethodPost, "/api/courses/c1/sources", upload, c.token); rec.Code != http.StatusCreated {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}
	rec = c.do(t, http.MethodPost, "/api/courses/c1/sources", upload, c.token)
	if rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), "upload allowance") {
		t.Fatalf("upload over quota: %d %s", rec.Code, rec.Body)
	}

	rec = c.do(t, http.MethodGet, "/api/account/usage", "", c.token)
	if rec.Code != http.StatusOK {
		t.Fatalf("usage: %d %s", rec.Code, rec.Body)
	}
	var got usageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Guest || got.Used != (usageCounts{Messages: 2, Uploads: 1}) || got.Limits != (usageCounts{Messages: 2, Uploads: 1, Web: 1}) ||
		!got.ResetsAt.Equal(time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("usage = %+v", got)
	}
	if rec := c.do(t, http.MethodGet, "/api/account/usage", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous usage: %d", rec.Code)
	}

	usage.err = errors.New("table down")
	if rec := c.do(t, http.MethodPost, "/api/chats/"+chat.ID+"/messages", `{"question":"q"}`, c.token); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("store down: %d", rec.Code)
	}
	if rec := c.do(t, http.MethodGet, "/api/account/usage", "", c.token); rec.Code != http.StatusInternalServerError {
		t.Fatalf("usage store down: %d", rec.Code)
	}
	if rec := newChatHarness(t, nil).do(t, http.MethodGet, "/api/account/usage", "", c.token); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("usage off: %d", rec.Code)
	}
}

func TestKillSwitchPauses(t *testing.T) {
	sources := &fakeSources{course: domain.Course{ID: "c1", Title: "Nets", OwnerID: "owner"}}
	c := newChatHarness(t, func(o *Options) {
		o.Answers = pausedAnswerer{}
		o.Sources, o.Presign, o.UploadsBucket = sources, &fakePresign{url: "https://u"}, "bucket"
		o.Attachments, o.Objects = &memoryAttachments{}, &memoryObjects{}
		o.Switch = paused(`{"uploads":false}`)
	})
	rec := c.do(t, http.MethodPost, "/api/chats", `{"course_id":"c1","model":""}`, c.token)
	var chat chatBody
	if err := json.Unmarshal(rec.Body.Bytes(), &chat); err != nil {
		t.Fatal(err)
	}
	for name, call := range map[string][3]string{
		"message":    {http.MethodPost, "/api/chats/" + chat.ID + "/messages", `{"question":"q"}`},
		"upload":     {http.MethodPost, "/api/courses/c1/sources", `{"name":"a.md","content_type":"text/markdown","byte_size":4}`},
		"attachment": {http.MethodPost, "/api/chats/" + chat.ID + "/attachments", `{"name":"a.md","content_type":"text/markdown","byte_size":4}`},
	} {
		t.Run(name, func(t *testing.T) {
			rec := c.do(t, call[0], call[1], call[2], c.token)
			if rec.Code != http.StatusServiceUnavailable || errorCode(t, rec.Body.String()) != "paused" ||
				!strings.Contains(rec.Body.String(), pausedMessage) {
				t.Fatalf("status %d body %s", rec.Code, rec.Body)
			}
		})
	}
}

func TestNewWebMeter(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC) }
	usage := &memUsage{}
	allowed := NewWebMeter(usage, Limits{User: Quota{Web: 2}, Guest: Quota{Web: 1}}, now)
	if !allowed(t.Context(), "guest-abc") || allowed(t.Context(), "guest-abc") {
		t.Fatal("guest web allowance is one")
	}
	if !allowed(t.Context(), "ip-1.2.3.4") || allowed(t.Context(), "ip-1.2.3.4") {
		t.Fatal("anonymous web allowance is the guest tier")
	}
	for i, want := range []bool{true, true, false} {
		if got := allowed(t.Context(), "owner"); got != want {
			t.Fatalf("user web search %d allowed = %v", i+1, got)
		}
	}
	if !allowed(t.Context(), "") {
		t.Fatal("an empty meter is not counted")
	}
	usage.err = errors.New("down")
	if allowed(t.Context(), "someone") {
		t.Fatal("a store failure must skip the web")
	}
	if !NewWebMeter(nil, DefaultLimits, now)(t.Context(), "x") {
		t.Fatal("no store allows the web")
	}
}
