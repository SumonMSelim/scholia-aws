package model

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sumonmselim/scholia-aws/internal/vision"
)

const maxReplyBytes = 1 << 20

// Compatible calls an OpenAI-compatible chat completions endpoint.
// baseURL is the API root, for example https://api.openai.com/v1. The key is sent
// as a bearer token and is never written into an error or a log.
type Compatible struct {
	endpoint string
	modelID  string
	key      string
	http     *http.Client
}

// openAITimeout bounds one call, streamed body included. It sits under the API
// Lambda's 60 second timeout so a stalled provider ends the answer cleanly.
const openAITimeout = 55 * time.Second

// OpenCompatible builds a client. http is allowed only for a loopback base URL,
// so a test server can stand in and a key is not sent in cleartext to a remote host.
func OpenCompatible(baseURL, modelID, key string) (*Compatible, error) {
	if strings.TrimSpace(modelID) == "" {
		return nil, errors.New("model id is required")
	}
	if strings.TrimSpace(key) == "" {
		return nil, errors.New("api key is required")
	}
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, errors.New("base url is not valid")
	}
	if u.User != nil {
		return nil, errors.New("base url must not contain a key")
	}
	if u.Scheme == "http" && !loopback(u.Hostname()) {
		return nil, errors.New("base url must be https")
	}
	root := strings.TrimRight(u.String(), "/")
	if !strings.HasSuffix(root, "/chat/completions") {
		root += "/chat/completions"
	}
	return &Compatible{
		endpoint: root,
		modelID:  modelID,
		key:      key,
		http: &http.Client{
			Timeout: openAITimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func loopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// Complete returns the assistant message. The response body is not included in errors,
// because a server may echo the prompt or the key.
func (c *Compatible) Complete(ctx context.Context, req Request) (string, error) {
	body, err := c.post(ctx, req, false)
	if err != nil {
		return "", err
	}
	defer body.Close()
	raw, err := io.ReadAll(io.LimitReader(body, maxReplyBytes+1))
	if err != nil {
		return "", errors.New("openai chat: read failed")
	}
	if len(raw) > maxReplyBytes {
		return "", errors.New("openai chat: response is too large")
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil || len(parsed.Choices) == 0 || parsed.Choices[0].Message.Content == "" {
		return "", errors.New("openai chat: invalid response")
	}
	return parsed.Choices[0].Message.Content, nil
}

// Stream emits content deltas in the order the server sends them.
func (c *Compatible) Stream(ctx context.Context, req Request, emit func(delta string) error) error {
	body, err := c.post(ctx, req, true)
	if err != nil {
		return err
	}
	defer body.Close()
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 0, 64*1024), maxReplyBytes)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		data, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue
		}
		data = strings.TrimSpace(data)
		if data == "[DONE]" {
			return nil
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return errors.New("openai chat: invalid stream")
		}
		if len(chunk.Choices) == 0 || chunk.Choices[0].Delta.Content == "" {
			continue
		}
		if err := emit(chunk.Choices[0].Delta.Content); err != nil {
			return err
		}
	}
	if err := sc.Err(); err != nil {
		return errors.New("openai chat: read failed")
	}
	return nil
}

// Observe asks the model for a JSON description of the image.
func (c *Compatible) Observe(ctx context.Context, contentType string, image []byte) (vision.Observation, error) {
	raw, err := c.Complete(ctx, visionRequest(observePrompt, contentType, image))
	if err != nil {
		return vision.Observation{}, err
	}
	return parseObservation(raw)
}

// Read asks the model to transcribe the image as Markdown.
func (c *Compatible) Read(ctx context.Context, contentType string, image []byte) (string, error) {
	return c.Complete(ctx, visionRequest(readPrompt, contentType, image))
}

func (c *Compatible) post(ctx context.Context, req Request, stream bool) (io.ReadCloser, error) {
	payload, err := c.payload(req, stream)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, errors.New("openai chat: request failed")
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.key)
	res, err := c.http.Do(httpReq)
	if err != nil {
		return nil, errors.New("openai chat: request failed")
	}
	if res.StatusCode != http.StatusOK {
		status := res.StatusCode
		if err := res.Body.Close(); err != nil {
			return nil, errors.New("openai chat: request failed")
		}
		return nil, fmt.Errorf("openai chat: status %d", status)
	}
	return res.Body, nil
}

func (c *Compatible) payload(req Request, stream bool) ([]byte, error) {
	if err := validate(req); err != nil {
		return nil, err
	}
	modelID := c.modelID
	if strings.TrimSpace(req.Model) != "" {
		modelID = req.Model
	}
	messages := make([]oaiMessage, 0, len(req.Messages))
	for _, msg := range req.Messages {
		item := oaiMessage{Role: string(msg.Role)}
		if len(msg.Image) == 0 {
			item.Content = msg.Content()
		} else {
			parts := make([]any, 0, 2)
			if msg.Content() != "" {
				parts = append(parts, map[string]string{"type": "text", "text": msg.Content()})
			}
			parts = append(parts, map[string]any{
				"type": "image_url",
				"image_url": map[string]string{
					"url": "data:" + msg.ContentType + ";base64," + base64.StdEncoding.EncodeToString(msg.Image),
				},
			})
			item.Content = parts
		}
		messages = append(messages, item)
	}
	return json.Marshal(oaiRequest{
		Model:               modelID,
		Messages:            messages,
		Temperature:         0,
		Stream:              stream,
		MaxCompletionTokens: maxTokens(req),
	})
}

type oaiRequest struct {
	Model       string       `json:"model"`
	Messages    []oaiMessage `json:"messages"`
	Temperature float64      `json:"temperature"`
	// MaxCompletionTokens caps the reply to bound cost.
	MaxCompletionTokens int  `json:"max_completion_tokens"`
	Stream              bool `json:"stream"`
}

type oaiMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}
