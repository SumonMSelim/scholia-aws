package killswitch

import (
	"errors"
	"testing"

	"github.com/sumonmselim/scholia-aws/internal/model"
)

func TestGuard(t *testing.T) {
	req := model.Request{Messages: []model.Message{{Role: model.RoleUser, Text: "hello"}}}
	img := []byte{0x89, 'P', 'N', 'G'}
	tests := []struct {
		name  string
		value string
		want  error
	}{
		{"on", `{}`, nil},
		{"paused", `{"server_models":false}`, ErrPaused},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := Guard{Next: model.Mock{}, Switch: &Switch{Name: "p", Params: &fakeParams{value: tt.value}}}
			if _, err := g.Complete(t.Context(), req); !errors.Is(err, tt.want) {
				t.Fatalf("Complete: %v", err)
			}
			if err := g.Stream(t.Context(), req, func(string) error { return nil }); !errors.Is(err, tt.want) {
				t.Fatalf("Stream: %v", err)
			}
			if _, err := g.Observe(t.Context(), "image/png", img); tt.want != nil && !errors.Is(err, tt.want) {
				t.Fatalf("Observe: %v", err)
			}
			if _, err := g.Read(t.Context(), "image/png", img); tt.want != nil && !errors.Is(err, tt.want) {
				t.Fatalf("Read: %v", err)
			}
		})
	}
}
