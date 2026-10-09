package store

import (
	"errors"
	"testing"
	"time"

	"github.com/sumonmselim/scholia-aws/internal/domain"
)

func TestRepositoryUsageAndExpiry(t *testing.T) {
	ctx := t.Context()
	client, err := NewClient(ctx, "us-east-1", startFloci(t))
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	const table = "scholia-usage"
	createTable(t, client, table)
	repo, err := New(client, table)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	now := time.Date(2026, 10, 1, 23, 59, 0, 0, time.UTC)

	for want := 1; want <= 2; want++ {
		got, err := repo.AddUsage(ctx, "guest-1", domain.UsageMessages, now, 2)
		if err != nil || got != want {
			t.Fatalf("add %d = %d, %v", want, got, err)
		}
	}
	if _, err := repo.AddUsage(ctx, "guest-1", domain.UsageMessages, now, 2); !errors.Is(err, ErrQuota) {
		t.Fatalf("over limit: got %v", err)
	}
	if _, err := repo.AddUsage(ctx, "guest-1", domain.UsageWeb, now, 5); err != nil {
		t.Fatalf("other kind: %v", err)
	}
	if got, err := repo.AddUsage(ctx, "guest-1", domain.UsageMessages, now.Add(2*time.Minute), 2); err != nil || got != 1 {
		t.Fatalf("next day = %d, %v", got, err)
	}
	for _, kind := range []string{domain.UsageGuests, domain.UsageCodes} {
		if _, err := repo.AddUsage(ctx, "ip-203.0.113.9", kind, now, 1); err != nil {
			t.Fatalf("address kind %s: %v", kind, err)
		}
		if _, err := repo.AddUsage(ctx, "ip-203.0.113.9", kind, now, 1); !errors.Is(err, ErrQuota) {
			t.Fatalf("address kind %s over limit: %v", kind, err)
		}
	}
	usage, err := repo.GetUsage(ctx, "guest-1", now)
	if err != nil || usage != (domain.Usage{Messages: 2, Web: 1}) {
		t.Fatalf("usage = %+v, %v", usage, err)
	}
	if usage, err := repo.GetUsage(ctx, "nobody", now); err != nil || usage != (domain.Usage{}) {
		t.Fatalf("empty usage = %+v, %v", usage, err)
	}
	for _, bad := range []struct {
		subject, kind string
		limit         int
	}{{"", domain.UsageWeb, 1}, {"u", "tokens", 1}, {"u", domain.UsageWeb, 0}} {
		if _, err := repo.AddUsage(ctx, bad.subject, bad.kind, now, bad.limit); err == nil || errors.Is(err, ErrQuota) {
			t.Fatalf("AddUsage(%+v) = %v, want a validation error", bad, err)
		}
	}

	const exp = int64(1790000000)
	course := domain.Course{ID: "g1", Title: "Guest", OwnerID: "guest-1", ExpiresAt: exp}
	if err := repo.PutCourse(ctx, course); err != nil {
		t.Fatalf("put course: %v", err)
	}
	if got, err := repo.GetCourse(ctx, "g1"); err != nil || got.ExpiresAt != exp {
		t.Fatalf("course expiry = %+v, %v", got, err)
	}
	src := domain.Source{ID: "s1", CourseID: "g1", Name: "a.md", Status: domain.SourceQueued, ExpiresAt: exp}
	if err := repo.PutSource(ctx, src); err != nil {
		t.Fatalf("put source: %v", err)
	}
	if got, err := repo.GetSource(ctx, "g1", "s1"); err != nil || got.ExpiresAt != exp {
		t.Fatalf("source expiry = %+v, %v", got, err)
	}
	chunk := domain.Chunk{ID: "k1", CourseID: "g1", SourceID: "s1", Text: "x", ExpiresAt: exp}
	if err := repo.PutChunk(ctx, chunk); err != nil {
		t.Fatalf("put chunk: %v", err)
	}
	if got, err := repo.GetChunk(ctx, "s1", "k1"); err != nil || got.ExpiresAt != exp {
		t.Fatalf("chunk expiry = %+v, %v", got, err)
	}
	chat := domain.Chat{ID: "h1", OwnerID: "guest-1", CourseID: "g1", Title: "t", CreatedAt: now, UpdatedAt: now, ExpiresAt: exp}
	if err := repo.PutChat(ctx, chat); err != nil {
		t.Fatalf("put chat: %v", err)
	}
	if got, err := repo.GetChat(ctx, "guest-1", "h1"); err != nil || got.ExpiresAt != exp {
		t.Fatalf("chat expiry = %+v, %v", got, err)
	}
	msg := domain.Message{ID: "m1", ChatID: "h1", Role: domain.RoleUser, Text: "q", Mode: domain.ModeAnswer, CreatedAt: now, ExpiresAt: exp}
	if err := repo.PutMessage(ctx, msg); err != nil {
		t.Fatalf("put message: %v", err)
	}
	if got, err := repo.ListMessages(ctx, "h1"); err != nil || len(got) != 1 || got[0].ExpiresAt != exp {
		t.Fatalf("message expiry = %+v, %v", got, err)
	}
	att := domain.Attachment{ID: "a1", ChatID: "h1", Name: "a.md", ContentType: "text/markdown", ByteSize: 1, Key: "chats/h1/a1", CreatedAt: now, ExpiresAt: exp}
	if err := repo.PutAttachment(ctx, att); err != nil {
		t.Fatalf("put attachment: %v", err)
	}
	if got, err := repo.GetAttachment(ctx, "h1", "a1"); err != nil || got.ExpiresAt != exp {
		t.Fatalf("attachment expiry = %+v, %v", got, err)
	}
}
