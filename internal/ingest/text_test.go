package ingest

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sumonmselim/scholia-aws/internal/chunk"
	"github.com/sumonmselim/scholia-aws/internal/domain"
	"github.com/sumonmselim/scholia-aws/internal/locator"
)

func TestBlocksFromText(t *testing.T) {
	body := "\\section{Routing}\nPackets move\nhop by hop.\n\n\\subsection*{Tables}\nEach router keeps a table.\n\n# Notes\n\nlast line"
	blocks := blocksFromText(body)
	want := []struct {
		kind  string
		level int
		text  string
	}{
		{chunk.KindHeading, 1, "Routing"},
		{chunk.KindParagraph, 0, "Packets move\nhop by hop."},
		{chunk.KindHeading, 2, "Tables"},
		{chunk.KindParagraph, 0, "Each router keeps a table."},
		{chunk.KindHeading, 1, "Notes"},
		{chunk.KindParagraph, 0, "last line"},
	}
	if len(blocks) != len(want) {
		t.Fatalf("blocks = %+v", blocks)
	}
	for i, w := range want {
		b := blocks[i]
		if b.Kind != w.kind || b.Level != w.level || b.Text != w.text {
			t.Fatalf("block %d = %+v, want %+v", i, b, w)
		}
		loc := b.Locators[0]
		if loc.Kind != locator.KindText || !strings.Contains(body[loc.Start:loc.End], strings.Split(w.text, "\n")[0]) {
			t.Fatalf("block %d locator %+v covers %q", i, loc, body[loc.Start:loc.End])
		}
	}
	if got := blocksFromText(" \n\n"); len(got) != 0 {
		t.Fatalf("blank text = %+v", got)
	}
}

func TestExtractText(t *testing.T) {
	tex := "\\section{Intro}\nThe Bellman-Ford algorithm relaxes edges.\n"
	tests := []struct {
		name        string
		file        string
		contentType string
		objects     *memObjects
		wantStatus  domain.SourceStatus
		wantChunks  bool
		wantErr     bool
	}{
		{"latex", "main.tex", "application/x-tex", &memObjects{size: int64(len(tex)), prefix: []byte(tex), body: []byte(tex)}, domain.SourceReady, true, false},
		{"text/x-tex alias", "main.tex", "text/x-tex", &memObjects{size: int64(len(tex)), prefix: []byte(tex), body: []byte(tex)}, domain.SourceReady, true, false},
		{"bibtex", "refs.bib", "application/x-bibtex", &memObjects{size: 20, prefix: []byte("@book{k,"), body: []byte("@book{k, title={Nets}}")}, domain.SourceReady, true, false},
		{"markdown", "notes.md", "text/markdown", &memObjects{size: 9, prefix: []byte("# Hi"), body: []byte("# Hi\n\nyo")}, domain.SourceReady, true, false},
		{"binary body", "main.tex", "application/x-tex", &memObjects{size: 4, prefix: []byte("ok"), body: []byte{0, 1, 2}}, domain.SourceFailed, false, false},
		{"too large", "main.tex", "application/x-tex", &memObjects{size: maxImportBytes + 1, prefix: []byte("ok")}, domain.SourceFailed, false, false},
		{"too large on read", "main.tex", "application/x-tex", &memObjects{size: 4, prefix: []byte("ok"), getErr: errTooLarge}, domain.SourceFailed, false, false},
		{"read fails", "main.tex", "application/x-tex", &memObjects{size: 4, prefix: []byte("ok"), getErr: errors.New("s3 down")}, domain.SourceProcessing, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateUpload(tt.file, tt.contentType, 10); err != nil {
				t.Fatalf("upload rejected: %v", err)
			}
			mem := &memStore{src: domain.Source{ID: "s1", CourseID: "c1", Name: tt.file, ContentType: tt.contentType, Status: domain.SourceQueued}}
			p := &Processor{Store: mem, Objects: tt.objects}
			err := p.HandleUpload(context.Background(), "b", ObjectKey("c1", "s1", tt.file))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v", err)
			}
			if mem.src.Status != tt.wantStatus || (len(mem.chunks) > 0) != tt.wantChunks {
				t.Fatalf("source %+v chunks %d", mem.src, len(mem.chunks))
			}
			for _, c := range mem.chunks {
				if err := c.Validate(); err != nil || c.Locators[0].Kind != locator.KindText {
					t.Fatalf("chunk %+v err %v", c, err)
				}
			}
		})
	}
}

func TestValidateLatexUploads(t *testing.T) {
	tests := []struct {
		file, contentType string
		ok                bool
	}{
		{"main.tex", "application/x-tex", true},
		{"main.tex", "text/x-tex", true},
		{"refs.bib", "application/x-bibtex", true},
		{"refs.bib", "application/x-tex", false},
		{"main.tex", "application/x-bibtex", false},
	}
	for _, tt := range tests {
		if err := ValidateUpload(tt.file, tt.contentType, 10); (err == nil) != tt.ok {
			t.Fatalf("%s %s: %v", tt.file, tt.contentType, err)
		}
		if err := CheckMagic(tt.contentType, []byte("\\documentclass{article}")); err != nil {
			t.Fatalf("magic %s: %v", tt.contentType, err)
		}
	}
	if err := CheckMagic("application/x-tex", []byte("%PDF-1.7")); err == nil {
		t.Fatal("pdf accepted as tex")
	}
}
