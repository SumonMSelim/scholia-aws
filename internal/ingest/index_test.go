package ingest

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/sumonmselim/scholia-aws/internal/chunk"
	"github.com/sumonmselim/scholia-aws/internal/domain"
	"github.com/sumonmselim/scholia-aws/internal/embed"
	"github.com/sumonmselim/scholia-aws/internal/vector"
)

type memVectors struct {
	model string
	put   []vector.Vector
	err   error
}

func (m *memVectors) Put(_ context.Context, modelID string, vectors []vector.Vector) error {
	m.model = modelID
	m.put = append(m.put, vectors...)
	return m.err
}

func (m *memVectors) Query(context.Context, string, []float32, string, int) ([]vector.Hit, error) {
	return nil, nil
}

func TestPutChunksIndexesVectors(t *testing.T) {
	src := domain.Source{ID: "s1", CourseID: "c1", Name: "notes.md", Status: domain.SourceProcessing}
	blocks := blocksFromText("# TCP\n\nTCP retransmits lost segments.\n\nACKs are cumulative.")
	tests := []struct {
		name      string
		embedder  *embed.Fake
		vectors   *memVectors
		model     string
		wantModel string
		wantPut   bool
		wantWarn  bool
	}{
		{"default model", &embed.Fake{Dim: 4}, &memVectors{}, "", embed.DefaultModel, true, false},
		{"configured model", &embed.Fake{Dim: 4}, &memVectors{}, "amazon.titan-embed-text-v1", "amazon.titan-embed-text-v1", true, false},
		{"embed fails", &embed.Fake{Dim: 4, Err: errors.New("throttled")}, &memVectors{}, "", "", false, true},
		{"put fails", &embed.Fake{Dim: 4}, &memVectors{err: errors.New("denied")}, "", embed.DefaultModel, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs strings.Builder
			mem := &memStore{src: src}
			p := &Processor{Store: mem, Embed: tt.embedder, Vectors: tt.vectors, EmbedModel: tt.model,
				Log: slog.New(slog.NewTextHandler(&logs, nil))}
			if err := p.putChunks(context.Background(), src, blocks); err != nil {
				t.Fatalf("putChunks: %v", err)
			}
			if len(mem.chunks) == 0 {
				t.Fatal("no chunks stored")
			}
			if tt.wantPut != (len(tt.vectors.put) == len(mem.chunks)) || tt.vectors.model != tt.wantModel {
				t.Fatalf("vectors %d for %d chunks, model %q", len(tt.vectors.put), len(mem.chunks), tt.vectors.model)
			}
			if tt.wantPut && (tt.vectors.put[0].CourseID != "c1" || tt.vectors.put[0].ChunkID != mem.chunks[0].ID) {
				t.Fatalf("vector %+v", tt.vectors.put[0])
			}
			if tt.wantWarn != strings.Contains(logs.String(), "level=WARN") {
				t.Fatalf("logs %q", logs.String())
			}
		})
	}
	// Without an embedder the chunks are stored for keyword search only.
	mem := &memStore{src: src}
	if err := (&Processor{Store: mem}).putChunks(context.Background(), src, []chunk.Block{{Kind: chunk.KindParagraph, Text: "x"}}); err != nil || len(mem.chunks) == 0 {
		t.Fatalf("no embedder: %v %d", err, len(mem.chunks))
	}
}
