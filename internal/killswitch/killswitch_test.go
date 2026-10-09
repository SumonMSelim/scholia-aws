package killswitch

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeParams struct {
	value string
	err   error
	reads int
}

func (f *fakeParams) GetParameter(context.Context, string) (string, error) {
	f.reads++
	return f.value, f.err
}

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    State
		wantErr bool
	}{
		{"empty object", `{}`, Open, false},
		{"models off", `{"server_models":false}`, State{Guests: true, Uploads: true}, false},
		{"all off", ` {"server_models":false,"guests":false,"uploads":false,"extra":1} `, State{}, false},
		{"not json", `off`, State{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.raw)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Fatalf("Parse(%q) = %+v, %v", tt.raw, got, err)
			}
		})
	}
}

func TestSwitchCachesAndKeepsLastGood(t *testing.T) {
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	params := &fakeParams{value: `{"guests":false}`}
	s := &Switch{Name: "/scholia/switch", Params: params, Now: func() time.Time { return now }}
	if got := s.State(t.Context()); got.Guests || !got.ServerModels {
		t.Fatalf("first read = %+v", got)
	}
	params.value = `{}`
	now = now.Add(30 * time.Second)
	if got := s.State(t.Context()); got.Guests || params.reads != 1 {
		t.Fatalf("cached read = %+v after %d reads", got, params.reads)
	}
	now = now.Add(time.Minute)
	if got := s.State(t.Context()); !got.Guests || params.reads != 2 {
		t.Fatalf("refreshed read = %+v after %d reads", got, params.reads)
	}
	params.err = errors.New("ssm down")
	params.value = `{"server_models":false}`
	now = now.Add(2 * time.Minute)
	if got := s.State(t.Context()); got != Open {
		t.Fatalf("failed read = %+v, want the last good value", got)
	}
	params.err = nil
	params.value = `broken`
	now = now.Add(2 * time.Minute)
	if got := s.State(t.Context()); got != Open {
		t.Fatalf("bad JSON = %+v, want the last good value", got)
	}
}

func TestSwitchFailClosedBeforeFirstRead(t *testing.T) {
	params := &fakeParams{err: errors.New("denied")}
	s := &Switch{Name: "p", Params: params, FailClosed: true, TTL: time.Nanosecond}
	if got := s.State(t.Context()); got.ServerModels || !got.Guests || !got.Uploads {
		t.Fatalf("fail closed = %+v", got)
	}
	open := &Switch{Name: "p", Params: &fakeParams{err: errors.New("denied")}}
	if got := open.State(t.Context()); got != Open {
		t.Fatalf("fail open = %+v", got)
	}
}

func TestSwitchDisabled(t *testing.T) {
	var none *Switch
	for _, s := range []*Switch{none, {}, {Name: "p"}} {
		if got := s.State(context.Background()); got != Open {
			t.Fatalf("disabled switch = %+v", got)
		}
	}
}
