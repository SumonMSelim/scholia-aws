// Package auth signs callers in with a passwordless email code.
// Cognito is behind Passwordless so tests use Fake and never call AWS.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// SessionTTL is how long a confirmed sign-in stays valid.
const SessionTTL = 12 * time.Hour

// Identity is the signed-in person. Subject is the Cognito user id.
type Identity struct {
	Subject string
	Email   string
}

// Passwordless starts an email one-time code and confirms it.
type Passwordless interface {
	Start(ctx context.Context, email string) (session string, err error)
	Confirm(ctx context.Context, email, session, code string) (Identity, error)
}

// Fake accepts one code and returns a fixed subject. Tests set the fields they care about.
type Fake struct {
	Subject    string
	Session    string
	Code       string
	StartErr   error
	ConfirmErr error
}

// Start returns a session handle. It does not send mail.
func (f *Fake) Start(context.Context, string) (string, error) {
	if f.StartErr != nil {
		return "", f.StartErr
	}
	if f.Session == "" {
		return "sess", nil
	}
	return f.Session, nil
}

// Confirm accepts the configured code, or 123456 when Code is empty.
func (f *Fake) Confirm(_ context.Context, email, _, code string) (Identity, error) {
	if f.ConfirmErr != nil {
		return Identity{}, f.ConfirmErr
	}
	want := f.Code
	if want == "" {
		want = "123456"
	}
	if strings.TrimSpace(code) != want {
		return Identity{}, errors.New("code was not accepted")
	}
	subject := f.Subject
	if subject == "" {
		subject = "user-1"
	}
	return Identity{Subject: subject, Email: strings.TrimSpace(email)}, nil
}

type sessionClaims struct {
	Sub string `json:"sub"`
	Exp int64  `json:"exp"`
}

// Sign returns an HMAC token. The secret must be at least 16 bytes.
// The token is not a Cognito JWT. The API mints it after Cognito accepts the code.
func Sign(secret, subject string, exp time.Time) (string, error) {
	if len(secret) < 16 || subject == "" || exp.IsZero() {
		return "", errors.New("session secret, subject, and expiry are required")
	}
	payload, err := json.Marshal(sessionClaims{Sub: subject, Exp: exp.Unix()})
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(payload)
	return body + "." + signBody(secret, body), nil
}

// Verify returns the subject when the token is intact and not expired.
func Verify(secret, token string, now time.Time) (string, error) {
	body, sig, ok := strings.Cut(token, ".")
	if !ok || body == "" || sig == "" {
		return "", errors.New("session token is not valid")
	}
	if !hmac.Equal([]byte(sig), []byte(signBody(secret, body))) {
		return "", errors.New("session token is not valid")
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return "", errors.New("session token is not valid")
	}
	var claims sessionClaims
	if err := json.Unmarshal(raw, &claims); err != nil || claims.Sub == "" {
		return "", errors.New("session token is not valid")
	}
	if !now.Before(time.Unix(claims.Exp, 0)) {
		return "", errors.New("session token is expired")
	}
	return claims.Sub, nil
}

func signBody(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(body))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
