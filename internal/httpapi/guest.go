package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/sumonmselim/scholia-aws/internal/auth"
	"github.com/sumonmselim/scholia-aws/internal/domain"
	"github.com/sumonmselim/scholia-aws/internal/limit"
	"github.com/sumonmselim/scholia-aws/internal/store"
)

// DefaultGuestTTL is how long a guest session and everything it creates lasts.
const DefaultGuestTTL = 48 * time.Hour

// guestTag marks a guest's upload so a bucket lifecycle rule on the tag can expire it.
const guestTag = "guest=true"

// pausedMessage is shown when the operator has paused a feature with the kill switch.
const pausedMessage = "The demo is paused. Please try again later."

// UsageStore counts metered calls per subject and UTC day.
// AddUsage returns store.ErrQuota when the day's allowance is used up.
type UsageStore interface {
	AddUsage(ctx context.Context, subject, kind string, now time.Time, limit int) (int, error)
	GetUsage(ctx context.Context, subject string, now time.Time) (domain.Usage, error)
}

// Quota is one tier's daily allowance per kind.
type Quota struct {
	Messages int
	Uploads  int
	Web      int
}

func (q Quota) of(kind string) int {
	switch kind {
	case domain.UsageMessages:
		return q.Messages
	case domain.UsageUploads:
		return q.Uploads
	default:
		return q.Web
	}
}

// Limits are the daily allowances. Guests and anonymous callers share the Guest tier.
type Limits struct {
	User  Quota
	Guest Quota
}

// DefaultLimits are the allowances used when config sets none.
var DefaultLimits = Limits{
	User:  Quota{Messages: 100, Uploads: 30, Web: 40},
	Guest: Quota{Messages: 30, Uploads: 5, Web: 10},
}

// meterPrefix marks a meter that is a client address, not a subject.
const meterPrefix = "ip-"

// For returns the tier for a meter: a signed-in user, or a guest or address.
func (l Limits) For(meter string) Quota {
	if auth.IsGuest(meter) || strings.HasPrefix(meter, meterPrefix) {
		return l.Guest
	}
	return l.User
}

// NewWebMeter counts web searches for the answer service. A store failure or a
// spent allowance both mean no web search: the answer goes on without it.
func NewWebMeter(usage UsageStore, limits Limits, now func() time.Time) func(ctx context.Context, meter string) bool {
	return func(ctx context.Context, meter string) bool {
		if usage == nil || meter == "" {
			return true
		}
		_, err := usage.AddUsage(ctx, meter, domain.UsageWeb, now(), limits.For(meter).Web)
		return err == nil
	}
}

// meter is who a call is counted against: the subject, or the address of an anonymous caller.
func meter(r *http.Request, who caller) string {
	if who.subject != "" {
		return who.subject
	}
	return meterPrefix + clientIP(r)
}

// charge counts one call of kind and writes 429 when the day's allowance is spent.
// A store failure fails closed: an unmetered call could run up cost.
func (s *server) charge(w http.ResponseWriter, r *http.Request, who caller, kind string) bool {
	if s.opts.Usage == nil {
		return true
	}
	m := meter(r, who)
	_, err := s.opts.Usage.AddUsage(r.Context(), m, kind, s.opts.Now(), s.limits().For(m).of(kind))
	if errors.Is(err, store.ErrQuota) {
		writeError(w, http.StatusTooManyRequests, "quota", "You've used today's "+kindName(kind)+" allowance. It resets at 00:00 UTC.")
		return false
	}
	if err != nil {
		s.opts.Logger.ErrorContext(r.Context(), "add usage", slog.String("err", err.Error()))
		writeError(w, http.StatusServiceUnavailable, "unavailable", "could not check today's allowance")
		return false
	}
	return true
}

func kindName(kind string) string {
	switch kind {
	case domain.UsageUploads:
		return "upload"
	case domain.UsageWeb:
		return "web search"
	default:
		return "message"
	}
}

func (s *server) limits() Limits {
	if s.opts.Limits == (Limits{}) {
		return DefaultLimits
	}
	return s.opts.Limits
}

func (s *server) guestTTL() time.Duration {
	if s.opts.GuestTTL <= 0 {
		return DefaultGuestTTL
	}
	return s.opts.GuestTTL
}

// expiry is the TTL for a record the caller creates: set for a guest, zero otherwise.
func (s *server) expiry(who caller) int64 {
	if !who.guest() {
		return 0
	}
	return s.opts.Now().Add(s.guestTTL()).Unix()
}

// uploadHeaders are the headers the client must send with the presigned PUT.
// A guest's upload carries the tag that the bucket lifecycle expires.
func uploadHeaders(who caller) map[string]string {
	if !who.guest() {
		return nil
	}
	return map[string]string{"x-amz-tagging": guestTag}
}

func tagging(who caller) string {
	if !who.guest() {
		return ""
	}
	return guestTag
}

// uploadsPaused writes 503 when the kill switch has turned uploads off.
func (s *server) uploadsPaused(w http.ResponseWriter, r *http.Request) bool {
	if s.opts.Switch.State(r.Context()).Uploads {
		return false
	}
	writeError(w, http.StatusServiceUnavailable, "paused", pausedMessage)
	return true
}

// Daily caps on the unauthenticated calls that cost money: starting a guest
// session and sending a sign-in email. They live in the usage table, so unlike
// GuestGate they hold across Lambda instances.
const (
	guestsPerAddress = 20
	guestsPerDay     = 300
	codesPerAddress  = 20
	codesPerEmail    = 5
)

// deploymentMeter counts guest sessions across every address.
const deploymentMeter = "deployment"

type dailyCap struct {
	meter string
	limit int
}

// emailMeter keys a cap on an address without storing the address.
func emailMeter(email string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email))))
	return "email-" + hex.EncodeToString(sum[:16])
}

// allowCalls counts one call of kind against each cap and writes 429 when one is
// spent. A store failure fails closed, as charge does.
func (s *server) allowCalls(w http.ResponseWriter, r *http.Request, kind, message string, caps ...dailyCap) bool {
	if s.opts.Usage == nil {
		return true
	}
	for _, c := range caps {
		_, err := s.opts.Usage.AddUsage(r.Context(), c.meter, kind, s.opts.Now(), c.limit)
		if errors.Is(err, store.ErrQuota) {
			writeError(w, http.StatusTooManyRequests, "rate_limited", message)
			return false
		}
		if err != nil {
			s.opts.Logger.ErrorContext(r.Context(), "add usage", slog.String("kind", kind), slog.String("err", err.Error()))
			writeError(w, http.StatusServiceUnavailable, "unavailable", "could not check today's allowance")
			return false
		}
	}
	return true
}

type guestResponse struct {
	Token     string    `json:"token"`
	Guest     bool      `json:"guest"`
	ExpiresAt time.Time `json:"expires_at"`
}

// startGuest mints a short guest session. It takes no body. Each address may
// start a few per hour, so a script cannot mint fresh allowances in a loop.
func (s *server) startGuest(w http.ResponseWriter, r *http.Request) {
	if s.opts.Sessions == nil || s.opts.SessionSecret == "" || !s.opts.Guests {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "guest sessions are not available")
		return
	}
	if !s.opts.Switch.State(r.Context()).Guests {
		writeError(w, http.StatusServiceUnavailable, "paused", pausedMessage)
		return
	}
	if err := s.opts.GuestGate.Allow(clientIP(r), s.opts.Now()); err != nil {
		writeError(w, http.StatusTooManyRequests, "rate_limited", limit.ErrWindow.Error())
		return
	}
	if !s.allowCalls(w, r, domain.UsageGuests, "Too many demo sessions today. Please try again tomorrow.",
		dailyCap{meterPrefix + clientIP(r), guestsPerAddress}, dailyCap{deploymentMeter, guestsPerDay}) {
		return
	}
	subject, err := auth.NewGuestSubject()
	if err != nil {
		s.opts.Logger.ErrorContext(r.Context(), "guest subject", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal", "could not start a guest session")
		return
	}
	exp := s.opts.Now().Add(s.guestTTL()).UTC().Truncate(time.Second)
	token, err := auth.Sign(s.opts.SessionSecret, subject, exp)
	if err != nil {
		s.opts.Logger.ErrorContext(r.Context(), "sign guest session")
		writeError(w, http.StatusInternalServerError, "internal", "could not start a guest session")
		return
	}
	writeJSON(w, http.StatusCreated, guestResponse{Token: token, Guest: true, ExpiresAt: exp})
}

type usageCounts struct {
	Messages int `json:"messages"`
	Uploads  int `json:"uploads"`
	Web      int `json:"web"`
}

type usageResponse struct {
	Guest    bool        `json:"guest"`
	Limits   usageCounts `json:"limits"`
	Used     usageCounts `json:"used"`
	ResetsAt time.Time   `json:"resets_at"`
}

func (s *server) getUsage(w http.ResponseWriter, r *http.Request) {
	if s.opts.Usage == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "usage is not configured")
		return
	}
	userID, ok := s.signedIn(w, r, "read usage")
	if !ok {
		return
	}
	now := s.opts.Now().UTC()
	used, err := s.opts.Usage.GetUsage(r.Context(), userID, now)
	if err != nil {
		s.opts.Logger.ErrorContext(r.Context(), "get usage", slog.String("err", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal", "could not read usage")
		return
	}
	q := s.limits().For(userID)
	writeJSON(w, http.StatusOK, usageResponse{
		Guest:    auth.IsGuest(userID),
		Limits:   usageCounts(q),
		Used:     usageCounts{Messages: used.Messages, Uploads: used.Uploads, Web: used.Web},
		ResetsAt: time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC),
	})
}
