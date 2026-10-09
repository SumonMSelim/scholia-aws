// Package model is the chat and vision client used by answers and by image understanding.
// Bedrock, one OpenAI-compatible adapter, and a deterministic mock implement the same contract.
// Callers must not add a second vision client.
package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/sumonmselim/scholia-aws/internal/vision"
)

// DefaultModel is the US inference profile for Amazon Nova 2 Lite.
// In a US region, Nova 2 is invoked through that profile rather than the bare model id.
const DefaultModel = "us.amazon.nova-2-lite-v1:0"

// Role is who produced a message.
type Role string

// Chat roles. System instructions are not a user turn.
const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is one turn. Image is optional and only valid on a user turn.
type Message struct {
	Role Role
	Text string
	// Guard is the end user's own words, sent after Text. When a guardrail is on,
	// Bedrock checks only Guard against its input filters, so course passages and
	// earlier turns, which read like injected instructions, are not mistaken for
	// a prompt attack. Only a user message carries it.
	Guard       string
	ContentType string
	Image       []byte
}

// Content is all the text the model reads from m, for providers without guarded blocks.
func (m Message) Content() string {
	if m.Text == "" || m.Guard == "" {
		return m.Text + m.Guard
	}
	return m.Text + "\n" + m.Guard
}

// DefaultMaxTokens caps a reply when the request names no cap.
const DefaultMaxTokens = 2048

// Request is one completion. An empty Model uses the client's default.
// MaxTokens caps the reply to bound cost; zero uses DefaultMaxTokens.
type Request struct {
	Model     string
	Messages  []Message
	MaxTokens int
}

func maxTokens(req Request) int {
	if req.MaxTokens > 0 {
		return req.MaxTokens
	}
	return DefaultMaxTokens
}

// Provider completes a chat, streams the same reply in order, and reads images.
// *Bedrock, *Compatible, and Mock all implement vision.Model.
type Provider interface {
	Complete(ctx context.Context, req Request) (string, error)
	Stream(ctx context.Context, req Request, emit func(delta string) error) error
	Observe(ctx context.Context, contentType string, image []byte) (vision.Observation, error)
	Read(ctx context.Context, contentType string, image []byte) (string, error)
}

const (
	observePrompt = "Return JSON only with keys text_amount, data_visual, and caption. text_amount is none, some, or block."
	readPrompt    = "Transcribe the image as Markdown. Write formulas as LaTeX. Do not add a preamble."
	maxImageBytes = 4 << 20
)

func visionRequest(prompt, contentType string, image []byte) Request {
	return Request{Messages: []Message{
		{Role: RoleSystem, Text: prompt},
		{Role: RoleUser, Text: "Look at this image.", ContentType: contentType, Image: image},
	}}
}

func validate(req Request) error {
	var users int
	for i, msg := range req.Messages {
		switch msg.Role {
		case RoleSystem, RoleUser, RoleAssistant:
		default:
			return fmt.Errorf("message %d: role %q is not valid", i, msg.Role)
		}
		if msg.Role == RoleUser {
			users++
		}
		if msg.Guard != "" && msg.Role != RoleUser {
			return fmt.Errorf("message %d: only a user message can be guarded", i)
		}
		if len(msg.Image) > 0 {
			if msg.Role != RoleUser {
				return errors.New("only a user message can include an image")
			}
			if err := imageOK(msg.ContentType, msg.Image); err != nil {
				return err
			}
		} else if strings.TrimSpace(msg.Content()) == "" {
			return fmt.Errorf("message %d: text is required", i)
		}
	}
	if users == 0 {
		return errors.New("a user message is required")
	}
	return nil
}

func imageOK(contentType string, image []byte) error {
	if len(image) == 0 {
		return errors.New("image is empty")
	}
	if len(image) > maxImageBytes {
		return errors.New("image is too large")
	}
	switch contentType {
	case "image/png", "image/jpeg", "image/jpg", "image/gif", "image/webp":
		return nil
	default:
		return fmt.Errorf("image content type %q is not supported", contentType)
	}
}

func parseObservation(raw string) (vision.Observation, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	var body struct {
		TextAmount string `json:"text_amount"`
		DataVisual bool   `json:"data_visual"`
		Caption    string `json:"caption"`
	}
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		return vision.Observation{}, errors.New("vision observation is not json")
	}
	amount := vision.TextAmount(body.TextAmount)
	switch amount {
	case vision.TextNone, vision.TextSome, vision.TextBlock:
	default:
		return vision.Observation{}, fmt.Errorf("vision text amount %q is not valid", body.TextAmount)
	}
	return vision.Observation{TextAmount: amount, DataVisual: body.DataVisual, Caption: strings.TrimSpace(body.Caption)}, nil
}
