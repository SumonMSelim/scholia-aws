package model

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"github.com/sumonmselim/scholia-aws/internal/awscfg"
)

func TestBedrockContract(t *testing.T) {
	client := &Bedrock{api: stubConverse{}, modelID: DefaultModel}
	exercise(t, client)
}

func TestBedrockUsesDefaultModel(t *testing.T) {
	stub := &recordingConverse{}
	client := &Bedrock{api: stub, modelID: DefaultModel}
	if _, err := client.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Text: "hello"}}}); err != nil {
		t.Fatal(err)
	}
	if stub.modelID != DefaultModel {
		t.Fatalf("model %s", stub.modelID)
	}
	opened := OpenBedrock(aws.Config{Region: "us-east-1"}, "http://127.0.0.1:9", "")
	if opened.modelID != DefaultModel {
		t.Fatalf("opened %s", opened.modelID)
	}
}

func TestBedrockLive(t *testing.T) {
	if os.Getenv("SCHOLIA_BEDROCK_LIVE") == "" {
		t.Skip("SCHOLIA_BEDROCK_LIVE is not set")
	}
	cfg, err := awscfg.Load(context.Background(), "us-east-1", "")
	if err != nil {
		t.Fatal(err)
	}
	exercise(t, OpenBedrock(cfg, "", ""))
}

type stubConverse struct{}

func (stubConverse) Converse(_ context.Context, in *bedrockruntime.ConverseInput, _ ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
	return &bedrockruntime.ConverseOutput{
		Output: &types.ConverseOutputMemberMessage{Value: types.Message{
			Role:    types.ConversationRoleAssistant,
			Content: []types.ContentBlock{&types.ContentBlockMemberText{Value: replyFor(userText(in))}},
		}},
	}, nil
}

func (stubConverse) ConverseStream(_ context.Context, in *bedrockruntime.ConverseStreamInput, _ ...func(*bedrockruntime.Options)) (eventStream, error) {
	text := replyFor(streamUserText(in))
	mid := len(text) / 2
	ch := make(chan types.ConverseStreamOutput, 2)
	ch <- delta(text[:mid])
	ch <- delta(text[mid:])
	close(ch)
	return &chanStream{events: ch}, nil
}

type recordingConverse struct {
	modelID   string
	maxTokens int32
}

func (r *recordingConverse) Converse(_ context.Context, in *bedrockruntime.ConverseInput, _ ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
	if in.ModelId != nil {
		r.modelID = *in.ModelId
	}
	if in.InferenceConfig != nil && in.InferenceConfig.MaxTokens != nil {
		r.maxTokens = *in.InferenceConfig.MaxTokens
	}
	return &bedrockruntime.ConverseOutput{
		Output: &types.ConverseOutputMemberMessage{Value: types.Message{
			Content: []types.ContentBlock{&types.ContentBlockMemberText{Value: "ok"}},
		}},
	}, nil
}

func (r *recordingConverse) ConverseStream(context.Context, *bedrockruntime.ConverseStreamInput, ...func(*bedrockruntime.Options)) (eventStream, error) {
	ch := make(chan types.ConverseStreamOutput)
	close(ch)
	return &chanStream{events: ch}, nil
}

type chanStream struct {
	events chan types.ConverseStreamOutput
	err    error
}

func (c *chanStream) Events() <-chan types.ConverseStreamOutput { return c.events }
func (c *chanStream) Close() error                              { return nil }
func (c *chanStream) Err() error                                { return c.err }

func delta(text string) types.ConverseStreamOutput {
	return &types.ConverseStreamOutputMemberContentBlockDelta{
		Value: types.ContentBlockDeltaEvent{
			Delta: &types.ContentBlockDeltaMemberText{Value: text},
		},
	}
}

func replyFor(text string) string {
	switch {
	case strings.Contains(text, "text_amount"):
		return `{"text_amount":"block","data_visual":false,"caption":"diagram"}`
	case strings.Contains(text, "LaTeX"):
		return "$$E = mc^2$$"
	default:
		return "hello there"
	}
}

func userText(in *bedrockruntime.ConverseInput) string {
	var b strings.Builder
	for _, block := range in.System {
		if text, ok := block.(*types.SystemContentBlockMemberText); ok {
			b.WriteString(text.Value)
		}
	}
	for _, msg := range in.Messages {
		for _, block := range msg.Content {
			if text, ok := block.(*types.ContentBlockMemberText); ok {
				b.WriteString(text.Value)
			}
		}
	}
	return b.String()
}

func streamUserText(in *bedrockruntime.ConverseStreamInput) string {
	var b strings.Builder
	for _, block := range in.System {
		if text, ok := block.(*types.SystemContentBlockMemberText); ok {
			b.WriteString(text.Value)
		}
	}
	for _, msg := range in.Messages {
		for _, block := range msg.Content {
			if text, ok := block.(*types.ContentBlockMemberText); ok {
				b.WriteString(text.Value)
			}
		}
	}
	return b.String()
}
