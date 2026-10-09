// Package limit caps how often one address may ask a question,
// and how many anonymous questions the API will answer in a day.
package limit

import (
	"errors"
	"sync"
	"time"
)

// ErrRateLimited means this address asked again inside the one-minute window.
var ErrRateLimited = errors.New("too many questions from this address")

// ErrSpendCap means anonymous questions have used the day's allowance.
// Signed-in callers are not counted against this cap.
var ErrSpendCap = errors.New("the daily limit for anonymous questions has been reached")

// Gate is an in-process limiter. A Lambda with several concurrent
// executions each has its own counts, so the real ceiling is this
// limit times the account concurrency.
type Gate struct {
	mu        sync.Mutex
	hits      map[string][]time.Time
	daily     map[string]int
	sweep     sweeper
	PerMinute int
	Daily     int
}

// pruneAt is the key count that first triggers a sweep of stale keys.
const pruneAt = 1024

// sweeper drops keys with no hit inside the window once the map has doubled
// since the last sweep, so memory tracks live keys and each sweep is amortized.
type sweeper struct{ next int }

func (s *sweeper) run(hits map[string][]time.Time, since time.Time) {
	if s.next < pruneAt {
		s.next = pruneAt
	}
	if len(hits) < s.next {
		return
	}
	for key, times := range hits {
		if len(times) == 0 || !times[len(times)-1].After(since) {
			delete(hits, key)
		}
	}
	s.next = max(pruneAt, 2*len(hits))
}

// New returns a gate. Non-positive limits fall back to 20 per minute and 50 per day.
func New(perMinute, daily int) *Gate {
	if perMinute < 1 {
		perMinute = 20
	}
	if daily < 1 {
		daily = 50
	}
	return &Gate{
		hits:      map[string][]time.Time{},
		daily:     map[string]int{},
		PerMinute: perMinute,
		Daily:     daily,
	}
}

// Allow records one question. anonymous selects the daily cap.
// A nil gate allows every call so existing handlers stay open.
func (g *Gate) Allow(ip string, now time.Time, anonymous bool) error {
	if g == nil {
		return nil
	}
	if ip == "" {
		ip = "unknown"
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	now = now.UTC()
	window := now.Add(-time.Minute)
	g.sweep.run(g.hits, window)
	kept := make([]time.Time, 0, len(g.hits[ip])+1)
	for _, hit := range g.hits[ip] {
		if hit.After(window) {
			kept = append(kept, hit)
		}
	}
	if len(kept) >= g.PerMinute {
		g.hits[ip] = kept
		return ErrRateLimited
	}
	g.hits[ip] = append(kept, now)
	if !anonymous {
		return nil
	}
	day := now.Format("2006-01-02")
	for d := range g.daily {
		if d != day {
			delete(g.daily, d)
		}
	}
	if g.daily[day] >= g.Daily {
		return ErrSpendCap
	}
	g.daily[day]++
	return nil
}

// ErrWindow means the key has used its allowance for the current window.
var ErrWindow = errors.New("too many requests from this address")

// Window allows Max calls per key in each Per window. Like Gate it is
// in-process, so each concurrent Lambda keeps its own counts.
type Window struct {
	mu    sync.Mutex
	hits  map[string][]time.Time
	sweep sweeper
	Max   int
	Per   time.Duration
}

// NewWindow returns a window limiter. Non-positive values fall back to 5 per hour.
func NewWindow(max int, per time.Duration) *Window {
	if max < 1 {
		max = 5
	}
	if per <= 0 {
		per = time.Hour
	}
	return &Window{hits: map[string][]time.Time{}, Max: max, Per: per}
}

// Allow records one call for key. A nil window allows every call.
func (w *Window) Allow(key string, now time.Time) error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	since := now.Add(-w.Per)
	w.sweep.run(w.hits, since)
	kept := make([]time.Time, 0, len(w.hits[key])+1)
	for _, hit := range w.hits[key] {
		if hit.After(since) {
			kept = append(kept, hit)
		}
	}
	if len(kept) >= w.Max {
		w.hits[key] = kept
		return ErrWindow
	}
	w.hits[key] = append(kept, now)
	return nil
}
