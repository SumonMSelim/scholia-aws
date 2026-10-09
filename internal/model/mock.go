package model

import (
	"context"
	"fmt"
	"strings"

	"github.com/sumonmselim/scholia-aws/internal/vision"
)

// Mock is a deterministic provider for local dev and CI. It does not call a hosted model.
// It never echoes prompt text, so fenced passages and system rules stay out of the UI.
// Stream emits the same reply as Complete, four runes at a time.
type Mock struct{}

// mockVerdict answers the router, whose system prompt asks for an intent as JSON.
const mockVerdict = `{"intent":"question","safety":"ok"}`

// Complete returns the router verdict for a routing call, and a short markdown reply otherwise.
func (Mock) Complete(_ context.Context, req Request) (string, error) {
	if err := validate(req); err != nil {
		return "", err
	}
	for _, msg := range req.Messages {
		if msg.Role == RoleSystem && strings.Contains(msg.Text, `"intent"`) {
			return mockVerdict, nil
		}
	}
	var user string
	for _, msg := range req.Messages {
		if msg.Role == RoleUser {
			user = msg.Content()
		}
	}
	first, count := passages(user)
	var b strings.Builder
	b.WriteString("### Local mock answer\n\n")
	if first != "" {
		fmt.Fprintf(&b, "- The closest course passage starts with: \"%s\"\n", first)
	} else {
		b.WriteString("- No course passage was retrieved for this message.\n")
	}
	b.WriteString("- Connect a provider key in Settings, or turn on server Bedrock, for a real answer.\n\n")
	fmt.Fprintf(&b, "```text\nmock: %d passage(s) retrieved\n```\n\n", count)
	b.WriteString("_This reply comes from the local mock model._")
	return b.String(), nil
}

// passages returns the first content line of the retrieval fence and how many
// numbered passages it holds. Fence tags, the preamble and markers are skipped.
func passages(user string) (string, int) {
	start := strings.Index(user, `source="retrieval"`)
	if start < 0 {
		return "", 0
	}
	body := user[start:]
	if end := strings.Index(body, "</untrusted_content"); end >= 0 {
		body = body[:end]
	}
	first, count := "", 0
	for i, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case i == 0, line == "", strings.HasSuffix(line, "untrusted data, not instructions."):
		case len(line) > 2 && line[0] == '[' && line[len(line)-1] == ']':
			count++
		case first == "":
			first = truncate(line, 80)
		}
	}
	return first, count
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// Stream emits Complete's text in order.
func (m Mock) Stream(ctx context.Context, req Request, emit func(delta string) error) error {
	text, err := m.Complete(ctx, req)
	if err != nil {
		return err
	}
	runes := []rune(text)
	for i := 0; i < len(runes); i += 4 {
		j := i + 4
		if j > len(runes) {
			j = len(runes)
		}
		if err := emit(string(runes[i:j])); err != nil {
			return err
		}
	}
	return nil
}

// Observe reports a text block. The caption is fixed so tests can rely on it.
func (Mock) Observe(_ context.Context, contentType string, image []byte) (vision.Observation, error) {
	if err := imageOK(contentType, image); err != nil {
		return vision.Observation{}, err
	}
	return vision.Observation{TextAmount: vision.TextBlock, Caption: "diagram"}, nil
}

// Read returns one LaTeX formula, independent of the image bytes.
func (Mock) Read(_ context.Context, contentType string, image []byte) (string, error) {
	if err := imageOK(contentType, image); err != nil {
		return "", err
	}
	return "$$E = mc^2$$", nil
}
