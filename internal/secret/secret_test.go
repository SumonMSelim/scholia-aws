package secret

import (
	"context"
	"errors"
	"testing"
)

type fakeReader struct {
	value string
	err   error
}

func (f fakeReader) SecretString(context.Context, string) (string, error) { return f.value, f.err }

func TestAPIKey(t *testing.T) {
	tests := []struct {
		name    string
		reader  fakeReader
		want    string
		wantErr bool
	}{
		{"plain", fakeReader{value: " tvly-abc \n"}, "tvly-abc", false},
		{"json", fakeReader{value: `{"api_key":"tvly-json"}`}, "tvly-json", false},
		{"json without key", fakeReader{value: `{"other":"x"}`}, "", true},
		{"broken json", fakeReader{value: `{"api_key":`}, "", true},
		{"empty", fakeReader{value: "  "}, "", true},
		{"read error", fakeReader{err: errors.New("denied")}, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := APIKey(context.Background(), tt.reader, "arn")
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Fatalf("APIKey = %q, %v", got, err)
			}
		})
	}
}

func TestPlain(t *testing.T) {
	tests := []struct {
		name    string
		reader  fakeReader
		want    string
		wantErr bool
	}{
		{"value", fakeReader{value: " 0123456789abcdef \n"}, "0123456789abcdef", false},
		{"too short", fakeReader{value: "short"}, "", true},
		{"empty", fakeReader{value: ""}, "", true},
		{"read error", fakeReader{err: errors.New("denied")}, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Plain(context.Background(), tt.reader, "arn", 16)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Fatalf("Plain = %q, %v", got, err)
			}
		})
	}
}
