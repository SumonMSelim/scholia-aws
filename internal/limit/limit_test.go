package limit

import (
	"errors"
	"strconv"
	"testing"
	"time"
)

func TestAllow(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	g := New(3, 1)
	if err := g.Allow("203.0.113.5", now, true); err != nil {
		t.Fatal(err)
	}
	if err := g.Allow("203.0.113.5", now.Add(time.Second), true); !errors.Is(err, ErrSpendCap) {
		t.Fatalf("second anonymous: %v", err)
	}
	if err := g.Allow("203.0.113.5", now.Add(2*time.Second), false); err != nil {
		t.Fatalf("signed-in under the rate limit: %v", err)
	}
	if err := g.Allow("203.0.113.5", now.Add(3*time.Second), false); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("rate: %v", err)
	}
	if err := g.Allow("203.0.113.5", now.Add(time.Minute+time.Second), true); !errors.Is(err, ErrSpendCap) {
		t.Fatalf("next window still capped: %v", err)
	}
	next := now.Add(24 * time.Hour)
	if err := g.Allow("203.0.113.9", next, true); err != nil {
		t.Fatalf("next day: %v", err)
	}
	var nilGate *Gate
	if err := nilGate.Allow("", now, true); err != nil {
		t.Fatal(err)
	}
	if got := New(0, 0); got.PerMinute != 20 || got.Daily != 50 {
		t.Fatalf("defaults: %+v", got)
	}
}

func TestWindow(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	w := NewWindow(2, time.Hour)
	for i := 0; i < 2; i++ {
		if err := w.Allow("1.2.3.4", now); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if err := w.Allow("1.2.3.4", now.Add(time.Minute)); !errors.Is(err, ErrWindow) {
		t.Fatalf("third call: %v", err)
	}
	if err := w.Allow("5.6.7.8", now); err != nil {
		t.Fatalf("other key: %v", err)
	}
	if err := w.Allow("1.2.3.4", now.Add(time.Hour+time.Second)); err != nil {
		t.Fatalf("after the window: %v", err)
	}
	var none *Window
	if err := none.Allow("x", now); err != nil {
		t.Fatalf("nil window: %v", err)
	}
	if d := NewWindow(0, 0); d.Max != 5 || d.Per != time.Hour {
		t.Fatalf("defaults = %d %v", d.Max, d.Per)
	}
}

func TestPruneStaleKeys(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		later time.Duration
		want  int
	}{
		// Every key's hits are outside the window, so only the new key is left.
		{"stale keys go", 2 * time.Hour, 1},
		// Keys still inside the window stay, however many there are.
		{"live keys stay", time.Minute, pruneAt + 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := NewWindow(5, time.Hour)
			for i := range pruneAt {
				if err := w.Allow("ip-"+strconv.Itoa(i), now); err != nil {
					t.Fatal(err)
				}
			}
			if err := w.Allow("fresh", now.Add(tt.later)); err != nil {
				t.Fatal(err)
			}
			if got := len(w.hits); got != tt.want {
				t.Fatalf("window keeps %d keys, want %d", got, tt.want)
			}
		})
	}

	g := New(5, 5)
	for i := range pruneAt {
		if err := g.Allow("ip-"+strconv.Itoa(i), now, true); err != nil && !errors.Is(err, ErrSpendCap) {
			t.Fatal(err)
		}
	}
	if err := g.Allow("fresh", now.Add(25*time.Hour), true); err != nil {
		t.Fatal(err)
	}
	if len(g.hits) != 1 || len(g.daily) != 1 {
		t.Fatalf("gate keeps %d addresses and %d days, want 1 and 1", len(g.hits), len(g.daily))
	}
}
