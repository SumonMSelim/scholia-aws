package model

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/aws/smithy-go/auth/bearer"

	"github.com/sumonmselim/scholia-aws/internal/vision"
)

// Prompts and keys are not written to logs. Errors name the operation, not the prompt.
type converseAPI interface {
	Converse(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error)
	ConverseStream(ctx context.Context, params *bedrockruntime.ConverseStreamInput, optFns ...func(*bedrockruntime.Options)) (eventStream, error)
}

type eventStream interface {
	Events() <-chan types.ConverseStreamOutput
	Close() error
	Err() error
}

type sdkConverse struct {
	client *bedrockruntime.Client
}

func (s sdkConverse) Converse(ctx context.Context, params *bedrockruntime.ConverseInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
	return s.client.Converse(ctx, params, optFns...)
}

func (s sdkConverse) ConverseStream(ctx context.Context, params *bedrockruntime.ConverseStreamInput, optFns ...func(*bedrockruntime.Options)) (eventStream, error) {
	out, err := s.client.ConverseStream(ctx, params, optFns...)
	if err != nil {
		return nil, err
	}
	stream := out.GetStream()
	if stream == nil {
		return nil, errors.New("bedrock converse: empty stream")
	}
	return stream, nil
}

// ErrGuardrail means a Bedrock guardrail blocked the prompt or the reply.
// Callers turn it into a refusal rather than an error.
var ErrGuardrail = errors.New("the guardrail blocked this request")

// Bedrock calls Amazon Bedrock Converse. endpoint is AWS_ENDPOINT_URL: empty in AWS, floci locally.
type Bedrock struct {
	api     converseAPI
	modelID string
	// guardrailID and guardrailVersion apply a Bedrock guardrail to every streamed reply when both are set.
	guardrailID      string
	guardrailVersion string
}

// WithGuardrail applies a guardrail to every reply this client streams. It is for
// the server-paid client: a user's own key is their account, so it gets none.
func (b *Bedrock) WithGuardrail(id, version string) *Bedrock {
	b.guardrailID = strings.TrimSpace(id)
	b.guardrailVersion = strings.TrimSpace(version)
	return b
}

func (b *Bedrock) guarded() bool {
	return b.guardrailID != "" && b.guardrailVersion != ""
}

// OpenBedrock builds a client. An empty modelID selects Nova 2 Lite.
func OpenBedrock(cfg aws.Config, endpoint, modelID string) *Bedrock {
	if strings.TrimSpace(modelID) == "" {
		modelID = DefaultModel
	}
	client := bedrockruntime.NewFromConfig(cfg, func(o *bedrockruntime.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
	})
	return &Bedrock{api: sdkConverse{client: client}, modelID: modelID}
}

// OpenBedrockKey builds a client that authenticates with a Bedrock API key sent
// as a bearer token instead of the process's AWS credentials. It always calls
// the real regional endpoint: a user's key is for AWS, and floci has no Bedrock.
func OpenBedrockKey(cfg aws.Config, modelID, apiKey string) (*Bedrock, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, errors.New("bedrock api key is required")
	}
	if strings.TrimSpace(modelID) == "" {
		modelID = DefaultModel
	}
	client := bedrockruntime.NewFromConfig(cfg, func(o *bedrockruntime.Options) {
		o.BaseEndpoint = nil
		o.BearerAuthTokenProvider = bearer.TokenProviderFunc(func(context.Context) (bearer.Token, error) {
			return bearer.Token{Value: apiKey}, nil
		})
		o.AuthSchemePreference = []string{"httpBearerAuth"}
	})
	return &Bedrock{api: sdkConverse{client: client}, modelID: modelID}, nil
}

// Complete returns the model's text. Reasoning blocks are ignored.
//
// No guardrail runs here. Complete serves internal calls, the classifier's
// verdict and image reads, whose output never reaches the student. With the
// guardrail on, its output filters read the verdict {"intent":"exam"} as exam
// cheating and every turn of a mock exam was refused. The student's words are
// guarded once, in Stream.
func (b *Bedrock) Complete(ctx context.Context, req Request) (string, error) {
	in, err := b.input(req, false)
	if err != nil {
		return "", err
	}
	params := &bedrockruntime.ConverseInput{
		ModelId:         &in.modelID,
		System:          in.system,
		Messages:        in.messages,
		InferenceConfig: inferenceConfig(req),
	}
	out, err := b.api.Converse(ctx, params)
	if err != nil {
		return "", fmt.Errorf("bedrock converse: %w", err)
	}
	msg, ok := out.Output.(*types.ConverseOutputMemberMessage)
	if !ok {
		return "", errors.New("bedrock converse: empty output")
	}
	var text strings.Builder
	for _, block := range msg.Value.Content {
		piece, ok := block.(*types.ContentBlockMemberText)
		if ok {
			text.WriteString(piece.Value)
		}
	}
	if text.Len() == 0 {
		return "", errors.New("bedrock converse: empty output")
	}
	return text.String(), nil
}

// Stream emits text deltas in the order Bedrock sends them.
func (b *Bedrock) Stream(ctx context.Context, req Request, emit func(delta string) error) error {
	in, err := b.input(req, b.guarded())
	if err != nil {
		return err
	}
	params := &bedrockruntime.ConverseStreamInput{
		ModelId:         &in.modelID,
		System:          in.system,
		Messages:        in.messages,
		InferenceConfig: inferenceConfig(req),
	}
	if b.guarded() {
		// Sync mode checks each reply chunk before it is sent. Async would stream
		// text the guardrail later blocks.
		params.GuardrailConfig = &types.GuardrailStreamConfiguration{
			GuardrailIdentifier:  aws.String(b.guardrailID),
			GuardrailVersion:     aws.String(b.guardrailVersion),
			StreamProcessingMode: types.GuardrailStreamProcessingModeSync,
		}
	}
	stream, err := b.api.ConverseStream(ctx, params)
	if err != nil {
		return fmt.Errorf("bedrock converse stream: %w", err)
	}
	defer stream.Close()
	blocked := false
	for ev := range stream.Events() {
		if stop, ok := ev.(*types.ConverseStreamOutputMemberMessageStop); ok {
			blocked = blocked || stop.Value.StopReason == types.StopReasonGuardrailIntervened
			continue
		}
		delta, ok := ev.(*types.ConverseStreamOutputMemberContentBlockDelta)
		if !ok {
			continue
		}
		text, ok := delta.Value.Delta.(*types.ContentBlockDeltaMemberText)
		if !ok || text.Value == "" {
			continue
		}
		if err := emit(text.Value); err != nil {
			return err
		}
	}
	if err := stream.Err(); err != nil {
		return fmt.Errorf("bedrock converse stream: %w", err)
	}
	if blocked {
		return ErrGuardrail
	}
	return nil
}

// Observe asks the model for a JSON description of the image.
func (b *Bedrock) Observe(ctx context.Context, contentType string, image []byte) (vision.Observation, error) {
	raw, err := b.Complete(ctx, visionRequest(observePrompt, contentType, image))
	if err != nil {
		return vision.Observation{}, err
	}
	return parseObservation(raw)
}

// Read asks the model to transcribe the image as Markdown.
func (b *Bedrock) Read(ctx context.Context, contentType string, image []byte) (string, error) {
	return b.Complete(ctx, visionRequest(readPrompt, contentType, image))
}

type converseIn struct {
	modelID  string
	system   []types.SystemContentBlock
	messages []types.Message
}

// input builds the Converse messages. guarded says whether this call carries
// the guardrail, so the user's words become a guardContent block.
func (b *Bedrock) input(req Request, guarded bool) (converseIn, error) {
	if err := validate(req); err != nil {
		return converseIn{}, err
	}
	modelID := b.modelID
	if strings.TrimSpace(req.Model) != "" {
		modelID = req.Model
	}
	var in converseIn
	in.modelID = modelID
	for _, msg := range req.Messages {
		if msg.Role == RoleSystem {
			in.system = append(in.system, &types.SystemContentBlockMemberText{Value: msg.Text})
			continue
		}
		blocks := make([]types.ContentBlock, 0, 3)
		if msg.Text != "" {
			blocks = append(blocks, &types.ContentBlockMemberText{Value: msg.Text})
		}
		if msg.Guard != "" {
			blocks = append(blocks, guardBlock(msg.Guard, guarded))
		}
		if len(msg.Image) > 0 {
			format, err := imageFormat(msg.ContentType)
			if err != nil {
				return converseIn{}, err
			}
			blocks = append(blocks, &types.ContentBlockMemberImage{Value: types.ImageBlock{
				Format: format,
				Source: &types.ImageSourceMemberBytes{Value: msg.Image},
			}})
		}
		role := types.ConversationRoleUser
		if msg.Role == RoleAssistant {
			role = types.ConversationRoleAssistant
		}
		in.messages = append(in.messages, types.Message{Role: role, Content: blocks})
	}
	return in, nil
}

// guardBlock carries the user's own words. With any guardContent in a request,
// the guardrail's input filters read only those blocks; on a call without the
// guardrail the words are plain text.
func guardBlock(text string, guarded bool) types.ContentBlock {
	if !guarded {
		return &types.ContentBlockMemberText{Value: text}
	}
	return &types.ContentBlockMemberGuardContent{Value: &types.GuardrailConverseContentBlockMemberText{
		Value: types.GuardrailConverseTextBlock{Text: aws.String(text)},
	}}
}

func imageFormat(contentType string) (types.ImageFormat, error) {
	switch contentType {
	case "image/png":
		return types.ImageFormatPng, nil
	case "image/jpeg", "image/jpg":
		return types.ImageFormatJpeg, nil
	case "image/gif":
		return types.ImageFormatGif, nil
	case "image/webp":
		return types.ImageFormatWebp, nil
	default:
		return "", errors.New("image content type is not supported")
	}
}

func inferenceConfig(req Request) *types.InferenceConfiguration {
	return &types.InferenceConfiguration{
		MaxTokens:   aws.Int32(int32(min(maxTokens(req), math.MaxInt32))), // #nosec G115 -- clamped above
		Temperature: aws.Float32(0),
	}
}
