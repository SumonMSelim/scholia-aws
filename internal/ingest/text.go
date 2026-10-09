package ingest

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/sumonmselim/scholia-aws/internal/chunk"
	"github.com/sumonmselim/scholia-aws/internal/domain"
	"github.com/sumonmselim/scholia-aws/internal/locator"
)

// textKinds are the uploads read as UTF-8 text. LaTeX and BibTeX come from Overleaf exports.
var textKinds = map[string]bool{
	"text/plain":           true,
	"text/markdown":        true,
	"application/x-tex":    true,
	"text/x-tex":           true,
	"application/x-bibtex": true,
}

var (
	markdownHeading = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*#*\s*$`)
	latexHeading    = regexp.MustCompile(`^\\(part|chapter|section|subsection|subsubsection)\*?\{(.+)\}\s*$`)
	latexLevels     = map[string]int{"part": 1, "chapter": 1, "section": 1, "subsection": 2, "subsubsection": 3}
)

func isText(contentType string) bool {
	return textKinds[canonicalType(contentType)]
}

// extractText chunks a text upload. Each paragraph keeps its byte range in the file.
func (p *Processor) extractText(ctx context.Context, src domain.Source, bucket, key string, size int64) error {
	if size > maxImportBytes {
		return p.fail(ctx, src, "text file is too large")
	}
	body, err := p.Objects.Get(ctx, bucket, key)
	if err != nil {
		if errors.Is(err, errTooLarge) {
			return p.fail(ctx, src, "text file is too large")
		}
		return err
	}
	if len(body) > 0 && !looksLikeText(body) {
		return p.fail(ctx, src, "file content does not match content type")
	}
	if err := p.putChunks(ctx, src, blocksFromText(string(body))); err != nil {
		return err
	}
	src.Status = domain.SourceReady
	src.FailureReason = ""
	return p.Store.PutSource(ctx, src)
}

// blocksFromText splits on blank lines. A Markdown or LaTeX section line becomes a heading.
func blocksFromText(body string) []chunk.Block {
	var out []chunk.Block
	start := -1
	flush := func(end int) {
		if start < 0 {
			return
		}
		text := strings.TrimSpace(body[start:end])
		if text != "" {
			out = append(out, chunk.Block{
				Kind: chunk.KindParagraph, Text: text,
				Locators: []locator.Locator{{Kind: locator.KindText, Start: start, End: end}},
			})
		}
		start = -1
	}
	offset := 0
	for _, line := range strings.SplitAfter(body, "\n") {
		lineStart, lineEnd := offset, offset+len(line)
		offset = lineEnd
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			flush(lineStart)
			continue
		}
		if level, title, ok := headingOf(trimmed); ok {
			flush(lineStart)
			out = append(out, chunk.Block{
				Kind: chunk.KindHeading, Level: level, Text: title,
				Locators: []locator.Locator{{Kind: locator.KindText, Start: lineStart, End: lineEnd}},
			})
			continue
		}
		if start < 0 {
			start = lineStart
		}
	}
	flush(len(body))
	return out
}

func headingOf(line string) (int, string, bool) {
	if m := markdownHeading.FindStringSubmatch(line); m != nil {
		return len(m[1]), m[2], true
	}
	if m := latexHeading.FindStringSubmatch(line); m != nil {
		return latexLevels[m[1]], m[2], true
	}
	return 0, "", false
}
