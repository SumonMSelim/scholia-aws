package answer

import (
	"context"
	"testing"

	"github.com/sumonmselim/scholia-aws/internal/domain"
	"github.com/sumonmselim/scholia-aws/internal/keyring"
	"github.com/sumonmselim/scholia-aws/internal/model"
)

type prefs map[string]domain.Settings

func (p prefs) GetSettings(_ context.Context, userID string) (domain.Settings, error) {
	return p[userID], nil
}

// TestDefaultsPrecedence runs the table in the Bound doc comment against the
// real key resolver. Keys are covered in keyring; here nobody has one.
func TestDefaultsPrecedence(t *testing.T) {
	const deployDefault = "us.amazon.nova-2-lite-v1:0"
	// Each case wants a model the earlier tiers did not name, so the winner is unambiguous.
	const pro = "us.amazon.nova-pro-v1:0"
	tests := []struct {
		name        string
		request     string
		userDefault string
		serverOn    bool
		wantModel   string
		wantServer  bool
	}{
		{"chat model wins", pro, "gpt-4.1", true, pro, true},
		{"user default next", "", pro, true, pro, true},
		{"deployment default last", "", "", true, deployDefault, true},
		{"openai without a key runs the default on bedrock", "gpt-4.1", "", true, deployDefault, true},
		{"user default openai without a key runs the default on bedrock", "", "gpt-4.1-mini", true, deployDefault, true},
		{"server off leaves the mock", "gpt-4.1", "", false, "gpt-4.1", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mock := &captureModel{}, &captureModel{}
			resolver := &keyring.Resolver{Fallback: deployDefault}
			if tt.serverOn {
				resolver.Server = server
			}
			bound := Bound{
				Service:     &Service{Search: &recordSearch{}, Model: mock, ChatModel: deployDefault},
				Courses:     modelCourse{course: domain.Course{ID: "c1", Title: "Nets"}},
				Providers:   resolver,
				Preferences: prefs{"u1": {DefaultModel: tt.userDefault}},
			}
			if _, err := bound.Answer(context.Background(), Request{CourseID: "c1", Question: "next hop", ChatModel: tt.request, UserID: "u1"}, drop); err != nil {
				t.Fatal(err)
			}
			used := mock
			if tt.wantServer {
				used = server
			}
			if used.calls != 1 || used.req.Model != tt.wantModel {
				t.Fatalf("server calls %d mock calls %d model %q, want %q on server=%v",
					server.calls, mock.calls, used.req.Model, tt.wantModel, tt.wantServer)
			}
		})
	}
}

var _ Preferences = prefs(nil)

func TestEmbedModelIsNotAChatModel(t *testing.T) {
	if _, ok := model.ProviderOf(model.EmbedModel); ok {
		t.Fatal("the embedding model is listed as a chat model")
	}
}
