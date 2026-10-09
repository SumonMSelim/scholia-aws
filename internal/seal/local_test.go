package seal

import (
	"bytes"
	"context"
	"testing"
)

func TestLocalRoundTrip(t *testing.T) {
	if _, err := OpenLocal("short"); err == nil {
		t.Fatal("short secret accepted")
	}
	box, err := OpenLocal("local-dev-session-secret-min-16")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	sealed, err := box.Seal(ctx, "u1", []byte("sk-test"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("sk-test")) {
		t.Fatal("ciphertext contains the plaintext")
	}
	got, err := box.Open(ctx, "u1", sealed)
	if err != nil || string(got) != "sk-test" {
		t.Fatalf("open = %q %v", got, err)
	}
	tests := []struct {
		name       string
		user       string
		ciphertext []byte
	}{
		{"other user", "u2", sealed},
		{"no user", "", sealed},
		{"short", "u1", []byte("x")},
		{"tampered", "u1", append(append([]byte{}, sealed[:len(sealed)-1]...), sealed[len(sealed)-1]^1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := box.Open(ctx, tt.user, tt.ciphertext); err == nil {
				t.Fatal("opened")
			}
		})
	}
	if _, err := box.Seal(ctx, "", []byte("k")); err == nil {
		t.Fatal("sealed without a user")
	}
	if _, err := box.Seal(ctx, "u1", nil); err == nil {
		t.Fatal("sealed empty plaintext")
	}
}
