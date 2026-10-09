package model

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
)

func TestBedrockCapsOutputTokens(t *testing.T) {
	tests := []struct {
		name string
		max  int
		want int32
	}{
		{"default", 0, DefaultMaxTokens},
		{"request cap", 512, 512},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &recordingConverse{}
			client := &Bedrock{api: stub, modelID: DefaultModel}
			req := Request{MaxTokens: tt.max, Messages: []Message{{Role: RoleUser, Text: "hello"}}}
			if _, err := client.Complete(context.Background(), req); err != nil {
				t.Fatal(err)
			}
			if stub.maxTokens != tt.want {
				t.Fatalf("max tokens %d, want %d", stub.maxTokens, tt.want)
			}
		})
	}
}

func TestCompatibleCapsOutputTokens(t *testing.T) {
	var got struct {
		MaxCompletionTokens int `json:"max_completion_tokens"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	t.Cleanup(srv.Close)
	client, err := OpenCompatible(srv.URL, "gpt-test", "sk-test")
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ max, want int }{{0, DefaultMaxTokens}, {64, 64}} {
		if _, err := client.Complete(context.Background(), Request{MaxTokens: tt.max, Messages: []Message{{Role: RoleUser, Text: "hi"}}}); err != nil {
			t.Fatal(err)
		}
		if got.MaxCompletionTokens != tt.want {
			t.Fatalf("max_completion_tokens %d, want %d", got.MaxCompletionTokens, tt.want)
		}
	}
}

func TestOpenBedrockKey(t *testing.T) {
	if _, err := OpenBedrockKey(aws.Config{Region: "us-east-1"}, "", "  "); err == nil {
		t.Fatal("empty key was accepted")
	}
	client, err := OpenBedrockKey(aws.Config{Region: "us-east-1", BaseEndpoint: aws.String("http://floci:4566")}, "", "bedrock-api-key")
	if err != nil {
		t.Fatal(err)
	}
	if client.modelID != DefaultModel {
		t.Fatalf("model %s", client.modelID)
	}
}

func TestCatalog(t *testing.T) {
	tests := []struct {
		id       string
		provider string
		ok       bool
	}{
		{DefaultModel, ProviderBedrock, true},
		{"gpt-4.1-mini", ProviderOpenAI, true},
		{"gpt-9", "", false},
	}
	for _, tt := range tests {
		got, ok := ProviderOf(tt.id)
		if got != tt.provider || ok != tt.ok {
			t.Fatalf("ProviderOf(%q) = %q %v", tt.id, got, ok)
		}
	}
	seen := map[string]bool{}
	for _, entry := range Catalog {
		if entry.ID == "" || entry.Label == "" || seen[entry.ID] || (entry.Provider != ProviderBedrock && entry.Provider != ProviderOpenAI) {
			t.Fatalf("bad catalog row %+v", entry)
		}
		seen[entry.ID] = true
	}
}
