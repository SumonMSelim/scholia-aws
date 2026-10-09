package auth

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

// GuestPrefix starts every guest subject. Cognito subjects are UUIDs, so they never share it.
const GuestPrefix = "guest-"

// NewGuestSubject returns a random guest subject with 128 bits of entropy.
func NewGuestSubject() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return GuestPrefix + hex.EncodeToString(b[:]), nil
}

// IsGuest reports whether subject belongs to a guest session.
func IsGuest(subject string) bool {
	return strings.HasPrefix(subject, GuestPrefix)
}
