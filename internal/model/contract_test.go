package model

import (
	"context"
	"strings"
	"testing"

	"github.com/sumonmselim/scholia-aws/internal/vision"
)

var (
	_ Provider     = Mock{}
	_ vision.Model = Mock{}
	_ Provider     = (*Bedrock)(nil)
	_ vision.Model = (*Bedrock)(nil)
	_ Provider     = (*Compatible)(nil)
	_ vision.Model = (*Compatible)(nil)
)

func exercise(t *testing.T, p Provider) {
	t.Helper()
	ctx := context.Background()
	req := Request{Messages: []Message{{Role: RoleUser, Text: "Say hello in one word."}}}
	got, err := p.Complete(ctx, req)
	if err != nil || got == "" {
		t.Fatalf("complete %q err %v", got, err)
	}
	var chunks []string
	if err := p.Stream(ctx, req, func(delta string) error {
		if delta == "" {
			t.Fatal("empty delta")
		}
		chunks = append(chunks, delta)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(chunks) == 0 {
		t.Fatal("stream produced no chunks")
	}
	pos := 0
	for _, chunk := range chunks {
		if !strings.HasPrefix(got[pos:], chunk) {
			t.Fatalf("chunk %q out of order in %q", chunk, got)
		}
		pos += len(chunk)
	}
	if pos != len(got) {
		t.Fatalf("stream joined %q, complete %q", strings.Join(chunks, ""), got)
	}

	obs, err := p.Observe(ctx, "image/png", []byte{0x89, 'P', 'N', 'G'})
	if err != nil {
		t.Fatal(err)
	}
	switch obs.TextAmount {
	case vision.TextNone, vision.TextSome, vision.TextBlock:
	default:
		t.Fatalf("text amount %q", obs.TextAmount)
	}
	reading, err := p.Read(ctx, "image/png", []byte{0x89, 'P', 'N', 'G'})
	if err != nil || strings.TrimSpace(reading) == "" {
		t.Fatalf("read %q err %v", reading, err)
	}
}

func TestMockContract(t *testing.T) {
	exercise(t, Mock{})
}

func TestParseObservation(t *testing.T) {
	got, err := parseObservation("```json\n{\"text_amount\":\"some\",\"data_visual\":true,\"caption\":\" chart \"}\n```")
	if err != nil {
		t.Fatal(err)
	}
	if got.TextAmount != vision.TextSome || !got.DataVisual || got.Caption != "chart" {
		t.Fatalf("%+v", got)
	}
	if _, err := parseObservation("not json"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := parseObservation(`{"text_amount":"lots"}`); err == nil {
		t.Fatal("expected error")
	}
}

func TestValidate(t *testing.T) {
	mock := Mock{}
	if _, err := mock.Complete(context.Background(), Request{}); err == nil {
		t.Fatal("expected error")
	}
	if _, err := mock.Observe(context.Background(), "image/tiff", []byte{1}); err == nil {
		t.Fatal("expected error")
	}
	if _, err := mock.Read(context.Background(), "image/png", nil); err == nil {
		t.Fatal("expected error")
	}
}
