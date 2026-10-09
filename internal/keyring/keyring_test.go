package keyring

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/sumonmselim/scholia-aws/internal/killswitch"
	"github.com/sumonmselim/scholia-aws/internal/model"
	"github.com/sumonmselim/scholia-aws/internal/store"
)

type memKeys struct {
	keys map[string][]byte
	err  error
}

func (m memKeys) GetProviderKey(_ context.Context, userID, provider string) ([]byte, error) {
	if m.err != nil {
		return nil, m.err
	}
	key, ok := m.keys[userID+"/"+provider]
	if !ok {
		return nil, store.ErrNotFound
	}
	return key, nil
}

// plainBox reverses the bytes so the stored form differs from the key.
type plainBox struct{ err error }

func reverse(b []byte) []byte {
	out := make([]byte, len(b))
	for i := range b {
		out[len(b)-1-i] = b[i]
	}
	return out
}

func (p plainBox) Seal(_ context.Context, _ string, b []byte) ([]byte, error) {
	return reverse(b), p.err
}
func (p plainBox) Open(_ context.Context, _ string, b []byte) ([]byte, error) {
	return reverse(b), p.err
}

func TestResolverServerTier(t *testing.T) {
	const premium = "us.amazon.nova-pro-v1:0"
	r := Resolver{Keys: memKeys{keys: map[string][]byte{}}, Box: plainBox{}, Server: model.Mock{}, Fallback: model.DefaultModel, AWS: aws.Config{Region: "us-east-1"}}
	tests := []struct {
		name   string
		r      Resolver
		userID string
		want   string
	}{
		{"signed-in user keeps the bedrock model", r, "u2", premium},
		{"guest gets the default", r, "guest-0123", model.DefaultModel},
		{"anonymous gets the default", r, "", model.DefaultModel},
		{"no fallback keeps the model", Resolver{Server: model.Mock{}}, "guest-0123", premium},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, used, err := tt.r.For(context.Background(), tt.userID, premium)
			if err != nil || p == nil {
				t.Fatalf("For = %v, %v", p, err)
			}
			if used != tt.want {
				t.Fatalf("model %q, want %q", used, tt.want)
			}
		})
	}
}

func TestResolverPrecedence(t *testing.T) {
	server := model.Mock{}
	keys := memKeys{keys: map[string][]byte{
		"u1/openai":  reverse([]byte("sk-user")),
		"u1/bedrock": reverse([]byte("bedrock-user")),
	}}
	tests := []struct {
		name    string
		r       Resolver
		userID  string
		modelID string
		want    string // "openai", "bedrock", "server" or "none"
		wantErr bool
	}{
		{"own openai key", Resolver{Keys: keys, Box: plainBox{}}, "u1", "gpt-4.1-mini", "openai", false},
		{"own bedrock key beats the server", Resolver{Keys: keys, Box: plainBox{}, Server: server}, "u1", model.DefaultModel, "bedrock", false},
		{"no key uses server bedrock", Resolver{Keys: keys, Box: plainBox{}, Server: server}, "u2", model.DefaultModel, "server", false},
		{"anonymous uses server bedrock", Resolver{Keys: keys, Box: plainBox{}, Server: server}, "", model.DefaultModel, "server", false},
		{"no key and server off", Resolver{Keys: keys, Box: plainBox{}}, "u2", model.DefaultModel, "none", false},
		{"openai without a key falls back to server bedrock", Resolver{Keys: keys, Box: plainBox{}, Server: server, Fallback: model.DefaultModel}, "u2", "gpt-4.1", "server", false},
		{"openai without a key or server", Resolver{Keys: keys, Box: plainBox{}}, "u2", "gpt-4.1", "none", false},
		{"unknown provider falls back to server bedrock", Resolver{Keys: keys, Box: plainBox{}, Server: server, Fallback: model.DefaultModel}, "u1", "custom-model", "server", false},
		{"unknown provider without server", Resolver{Keys: keys, Box: plainBox{}}, "u1", "custom-model", "none", false},
		{"no key storage", Resolver{Server: server}, "u1", model.DefaultModel, "server", false},
		{"key read fails", Resolver{Keys: memKeys{err: errors.New("table down")}, Box: plainBox{}}, "u1", "gpt-4.1", "", true},
		{"key open fails", Resolver{Keys: keys, Box: plainBox{err: errors.New("kms down")}}, "u1", "gpt-4.1", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.r.AWS = aws.Config{Region: "us-east-1"}
			p, used, err := tt.r.For(context.Background(), tt.userID, tt.modelID)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v", err)
			}
			if tt.wantErr {
				return
			}
			got := "none"
			switch p.(type) {
			case *model.Compatible:
				got = "openai"
			case *model.Bedrock:
				got = "bedrock"
			case model.Mock:
				got = "server"
			}
			if got != tt.want {
				t.Fatalf("provider %T, want %s", p, tt.want)
			}
			wantModel := tt.modelID
			if got == "server" {
				if _, bedrock := model.ProviderOf(tt.modelID); !bedrock || tt.modelID != model.DefaultModel {
					wantModel = tt.r.Fallback
				}
			}
			if used != wantModel {
				t.Fatalf("model %q, want %q", used, wantModel)
			}
		})
	}
}

func TestResolverPausedServer(t *testing.T) {
	keys := memKeys{keys: map[string][]byte{"u1/bedrock": reverse([]byte("bedrock-user"))}}
	r := Resolver{Keys: keys, Box: plainBox{}, Server: model.Mock{}, AWS: aws.Config{Region: "us-east-1"},
		ServerAllowed: func(context.Context) bool { return false }}
	if _, _, err := r.For(context.Background(), "u2", model.DefaultModel); !errors.Is(err, killswitch.ErrPaused) {
		t.Fatalf("paused server: %v", err)
	}
	if _, _, err := r.For(context.Background(), "u2", "gpt-4.1"); !errors.Is(err, killswitch.ErrPaused) {
		t.Fatalf("paused fallback: %v", err)
	}
	// A caller's own key is their account, so the pause does not apply to it.
	p, _, err := r.For(context.Background(), "u1", model.DefaultModel)
	if err != nil || p == nil {
		t.Fatalf("own key while paused = %v, %v", p, err)
	}
	r.ServerAllowed = func(context.Context) bool { return true }
	if p, _, err := r.For(context.Background(), "u2", model.DefaultModel); err != nil || p != (model.Mock{}) {
		t.Fatalf("server on = %v, %v", p, err)
	}
}
