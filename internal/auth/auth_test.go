package auth

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestFakeAndSessionToken(t *testing.T) {
	fake := &Fake{Subject: "owner", Code: "999111"}
	session, err := fake.Start(context.Background(), "a@b.co")
	if err != nil || session == "" {
		t.Fatalf("start: %q %v", session, err)
	}
	if _, err := fake.Confirm(context.Background(), "a@b.co", session, "000000"); err == nil {
		t.Fatal("wrong code was accepted")
	}
	id, err := fake.Confirm(context.Background(), "a@b.co", session, "999111")
	if err != nil || id.Subject != "owner" {
		t.Fatalf("confirm: %+v %v", id, err)
	}

	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	secret := "session-secret-16"
	token, err := Sign(secret, id.Subject, now.Add(SessionTTL))
	if err != nil {
		t.Fatal(err)
	}
	got, err := Verify(secret, token, now)
	if err != nil || got != "owner" {
		t.Fatalf("verify: %q %v", got, err)
	}
	if _, err := Verify(secret, token, now.Add(SessionTTL)); err == nil {
		t.Fatal("expired token was accepted")
	}
	if _, err := Verify("other-secret-16b", token, now); err == nil {
		t.Fatal("wrong secret was accepted")
	}
	parts := strings.Split(token, ".")
	parts[1] = parts[1][:len(parts[1])-1] + "A"
	if _, err := Verify(secret, strings.Join(parts, "."), now); err == nil {
		t.Fatal("tampered token was accepted")
	}
	if _, err := Sign("short", "owner", now); err == nil {
		t.Fatal("short secret was accepted")
	}
	if _, err := Verify(secret, "not-a-token", now); err == nil {
		t.Fatal("malformed token was accepted")
	}
}

func TestFakeDefaults(t *testing.T) {
	fake := &Fake{}
	id, err := fake.Confirm(context.Background(), "a@b.co", "sess", "123456")
	if err != nil || id.Subject != "user-1" {
		t.Fatalf("confirm: %+v %v", id, err)
	}
	fake.StartErr = errStart
	if _, err := fake.Start(context.Background(), "a@b.co"); err == nil {
		t.Fatal("start error was ignored")
	}
}

var errStart = errSentinel("start failed")

type errSentinel string

func (e errSentinel) Error() string { return string(e) }
