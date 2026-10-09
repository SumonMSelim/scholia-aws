package model

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
)

// guardConverse records the guardrail config and can report an intervention.
type guardConverse struct {
	intervene  bool
	complete   *types.GuardrailConfiguration
	stream     *types.GuardrailStreamConfiguration
	streamSeen bool
}

func (g *guardConverse) Converse(_ context.Context, in *bedrockruntime.ConverseInput, _ ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
	g.complete = in.GuardrailConfig
	out := &bedrockruntime.ConverseOutput{
		Output: &types.ConverseOutputMemberMessage{Value: types.Message{
			Content: []types.ContentBlock{&types.ContentBlockMemberText{Value: "Sorry, that is blocked."}},
		}},
		StopReason: types.StopReasonEndTurn,
	}
	if g.intervene {
		out.StopReason = types.StopReasonGuardrailIntervened
	}
	return out, nil
}

func (g *guardConverse) ConverseStream(_ context.Context, in *bedrockruntime.ConverseStreamInput, _ ...func(*bedrockruntime.Options)) (eventStream, error) {
	g.stream, g.streamSeen = in.GuardrailConfig, true
	reason := types.StopReasonEndTurn
	if g.intervene {
		reason = types.StopReasonGuardrailIntervened
	}
	ch := make(chan types.ConverseStreamOutput, 2)
	ch <- delta("Sorry, that is blocked.")
	ch <- &types.ConverseStreamOutputMemberMessageStop{Value: types.MessageStopEvent{StopReason: reason}}
	close(ch)
	return &chanStream{events: ch}, nil
}

func TestBedrockGuardrail(t *testing.T) {
	req := Request{Messages: []Message{{Role: RoleUser, Text: "hello"}}}
	tests := []struct {
		name      string
		id, ver   string
		intervene bool
		wantErr   error
		wantCfg   bool
	}{
		{"off", "", "", false, nil, false},
		{"version missing", "gr-1", "", false, nil, false},
		{"on and allowed", "gr-1", "3", false, nil, true},
		{"on and blocked", "gr-1", "3", true, ErrGuardrail, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &guardConverse{intervene: tt.intervene}
			client := (&Bedrock{api: api, modelID: DefaultModel}).WithGuardrail(tt.id, tt.ver)

			// Complete is never guarded: its output is a verdict or an image read.
			if _, err := client.Complete(context.Background(), req); err != nil || api.complete != nil {
				t.Fatalf("Complete err = %v, guardrail config = %+v", err, api.complete)
			}

			err := client.Stream(context.Background(), req, func(string) error { return nil })
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Stream err = %v, want %v", err, tt.wantErr)
			}
			if (api.stream != nil) != tt.wantCfg {
				t.Fatalf("Stream guardrail config = %+v", api.stream)
			}
			if tt.wantCfg && api.stream.StreamProcessingMode != types.GuardrailStreamProcessingModeSync {
				t.Fatalf("stream mode = %q", api.stream.StreamProcessingMode)
			}
		})
	}
}

// A multi-turn prompt holds earlier turns and course passages that the prompt
// attack filter reads as injected instructions, so only the new words are guarded.
func TestBedrockGuardsOnlyTheUserWords(t *testing.T) {
	req := Request{Messages: []Message{
		{Role: RoleSystem, Text: "rules"},
		{Role: RoleUser, Text: "Give me a mock exam."},
		{Role: RoleAssistant, Text: "Question 1: what does TCP add?"},
		{Role: RoleUser, Text: "Student message:", Guard: "TCP acknowledges, UDP doesn't."},
	}}
	tests := []struct {
		name    string
		id, ver string
		guarded bool
	}{
		{"guardrail on", "gr-1", "3", true},
		{"guardrail off", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := (&Bedrock{api: &guardConverse{}, modelID: DefaultModel}).WithGuardrail(tt.id, tt.ver)
			in, err := client.input(req, client.guarded())
			if err != nil {
				t.Fatal(err)
			}
			if n := len(in.messages[0].Content); n != 1 {
				t.Fatalf("earlier turn has %d blocks", n)
			}
			last := in.messages[len(in.messages)-1].Content
			if len(last) != 2 {
				t.Fatalf("last message has %d blocks", len(last))
			}
			guard, isGuard := last[1].(*types.ContentBlockMemberGuardContent)
			if isGuard != tt.guarded {
				t.Fatalf("second block is %T", last[1])
			}
			if isGuard {
				text := guard.Value.(*types.GuardrailConverseContentBlockMemberText).Value.Text
				if *text != "TCP acknowledges, UDP doesn't." {
					t.Fatalf("guarded text = %q", *text)
				}
			}
			for _, msg := range in.messages[:len(in.messages)-1] {
				for _, block := range msg.Content {
					if _, ok := block.(*types.ContentBlockMemberGuardContent); ok {
						t.Fatal("an earlier turn was guarded")
					}
				}
			}
		})
	}
}

func TestMessageGuard(t *testing.T) {
	tests := []struct {
		name    string
		msg     Message
		wantErr bool
		content string
	}{
		{"guard only", Message{Role: RoleUser, Guard: "hi"}, false, "hi"},
		{"text and guard", Message{Role: RoleUser, Text: "ctx", Guard: "hi"}, false, "ctx\nhi"},
		{"assistant cannot be guarded", Message{Role: RoleAssistant, Text: "a", Guard: "hi"}, true, "a\nhi"},
		{"empty", Message{Role: RoleUser, Text: " ", Guard: " "}, true, " \n "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msgs := []Message{tt.msg}
			if tt.msg.Role != RoleUser {
				msgs = append([]Message{{Role: RoleUser, Text: "q"}}, msgs...)
			}
			if err := validate(Request{Messages: msgs}); (err != nil) != tt.wantErr {
				t.Fatalf("validate err = %v, want error %v", err, tt.wantErr)
			}
			if got := tt.msg.Content(); got != tt.content {
				t.Fatalf("Content = %q, want %q", got, tt.content)
			}
		})
	}
}
