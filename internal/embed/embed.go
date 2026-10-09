// Package embed turns text into vectors through Amazon Bedrock.
// The model id is also the vector-store index key, so callers pass the same id to both.
package embed

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
)

// DefaultModel is the Bedrock Titan text embedding model used when a course has not picked one.
// Titan Text Embeddings V2 returns 1024 dimensions unless the request asks for fewer.
const DefaultModel = "amazon.titan-embed-text-v2:0"

// Embedder maps one text to a vector for a model id.
// A fake implements this in tests. *Client is the Bedrock implementation.
type Embedder interface {
	Embed(ctx context.Context, modelID, text string) ([]float32, error)
}

// Fake is a deterministic embedder that does not call Bedrock.
// The vector depends on the model id and the text, and it is never all zeroes.
type Fake struct {
	Dim       int
	Err       error
	Calls     int
	LastModel string
	LastText  string
}

// Embed records the call and returns a stable non-zero vector.
func (f *Fake) Embed(_ context.Context, modelID, text string) ([]float32, error) {
	f.Calls++
	f.LastModel = modelID
	f.LastText = text
	if f.Err != nil {
		return nil, f.Err
	}
	if strings.TrimSpace(modelID) == "" || strings.TrimSpace(text) == "" {
		return nil, errors.New("embedding model id and text are required")
	}
	dim := f.Dim
	if dim == 0 {
		dim = 4
	}
	if dim < 1 {
		return nil, errors.New("fake embedding dimension must be >= 1")
	}
	sum := sha256.Sum256([]byte(modelID + "\n" + text))
	out := make([]float32, dim)
	for i := range out {
		out[i] = float32(sum[i%len(sum)])/255 + 0.01
	}
	return out, nil
}

type modelAPI interface {
	InvokeModel(ctx context.Context, params *bedrockruntime.InvokeModelInput, optFns ...func(*bedrockruntime.Options)) (*bedrockruntime.InvokeModelOutput, error)
}

// Client calls Bedrock InvokeModel. endpoint is AWS_ENDPOINT_URL: empty in AWS, floci in local runs.
// Only Titan text embedding models are accepted. Their request body is inputText, which other
// embedding families do not use, so a mismatched model would store a failed or empty vector.
type Client struct {
	api modelAPI
}

// Supported reports whether the client can call modelID. Only Titan text
// embedding models take the inputText body this client sends.
func Supported(modelID string) bool {
	return strings.Contains(modelID, "amazon.titan-embed-text")
}

// Open builds a client. A non-empty endpoint overrides the service URL the same way the other AWS clients do.
func Open(cfg aws.Config, endpoint string) *Client {
	api := bedrockruntime.NewFromConfig(cfg, func(o *bedrockruntime.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
	})
	return &Client{api: api}
}

// Embed requests one Titan text embedding. The returned slice is the model output as float32.
func (c *Client) Embed(ctx context.Context, modelID, text string) ([]float32, error) {
	if strings.TrimSpace(modelID) == "" || strings.TrimSpace(text) == "" {
		return nil, errors.New("embedding model id and text are required")
	}
	if !Supported(modelID) {
		return nil, errors.New("only amazon titan text embedding models are supported")
	}
	body, err := json.Marshal(struct {
		InputText string `json:"inputText"`
	}{InputText: text})
	if err != nil {
		return nil, err
	}
	out, err := c.api.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
		ModelId:     aws.String(modelID),
		ContentType: aws.String("application/json"),
		Accept:      aws.String("application/json"),
		Body:        body,
	})
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Embedding []float32 `json:"embedding"`
	}
	if err := json.Unmarshal(out.Body, &parsed); err != nil {
		return nil, err
	}
	if len(parsed.Embedding) == 0 {
		return nil, errors.New("embedding is empty")
	}
	return parsed.Embedding, nil
}
