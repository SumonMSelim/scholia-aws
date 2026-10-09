// Package vision decides whether an image is worth reading and turns that
// reading into a child chunk. The model provider supplies the client later;
// this package only defines the call and the policy around it.
package vision

import (
	"context"
	"errors"
	"strings"

	"github.com/sumonmselim/scholia-aws/internal/domain"
	"github.com/sumonmselim/scholia-aws/internal/locator"
)

// TextAmount is how much readable text the observation found.
type TextAmount string

const (
	// TextNone means the image has no readable text.
	TextNone TextAmount = "none"
	// TextSome means a label or a word, not a passage.
	TextSome TextAmount = "some"
	// TextBlock means a passage that should be read.
	TextBlock TextAmount = "block"
)

// Observation is one look at an image, before any OCR call.
type Observation struct {
	TextAmount TextAmount
	DataVisual bool
	Caption    string
}

// Model observes an image and, when asked, reads it as Markdown.
// Formulas in the reading are LaTeX. internal/model implements this;
// callers must not add a second client.
type Model interface {
	Observe(ctx context.Context, contentType string, image []byte) (Observation, error)
	Read(ctx context.Context, contentType string, image []byte) (string, error)
}

// ShouldRead reports whether the image is a text block or a data visual.
// Anything else is left as a picture.
func ShouldRead(obs Observation) bool {
	return obs.TextAmount == TextBlock || obs.DataVisual
}

// Sanitize drops an empty reading. A non-empty reading is kept as written,
// including LaTeX, so a formula is not stripped on the way to the chunk.
func Sanitize(markdown string) (string, bool) {
	markdown = strings.TrimSpace(strings.ReplaceAll(markdown, "\x00", ""))
	if markdown == "" {
		return "", false
	}
	return markdown, true
}

// Picture is one image already located on a page or a slide.
type Picture struct {
	CourseID    string
	SourceID    string
	ParentID    string
	ChildID     string
	ContentType string
	Image       []byte
	Locator     locator.Locator
}

// Interpret observes the picture and, when it is worth reading, returns a
// parent chunk and the OCR child. The child copies the page or slide locator.
// ok is false when the picture is skipped or the reading is empty.
func Interpret(ctx context.Context, model Model, pic Picture) (parent, child domain.Chunk, ok bool, err error) {
	if model == nil {
		return domain.Chunk{}, domain.Chunk{}, false, errors.New("vision is not configured")
	}
	if len(pic.Image) == 0 {
		return domain.Chunk{}, domain.Chunk{}, false, errors.New("image is empty")
	}
	if err := pic.Locator.Validate(); err != nil {
		return domain.Chunk{}, domain.Chunk{}, false, err
	}
	obs, err := model.Observe(ctx, pic.ContentType, pic.Image)
	if err != nil {
		return domain.Chunk{}, domain.Chunk{}, false, err
	}
	if !ShouldRead(obs) {
		return domain.Chunk{}, domain.Chunk{}, false, nil
	}
	raw, err := model.Read(ctx, pic.ContentType, pic.Image)
	if err != nil {
		return domain.Chunk{}, domain.Chunk{}, false, err
	}
	text, keep := Sanitize(raw)
	if !keep {
		return domain.Chunk{}, domain.Chunk{}, false, nil
	}
	parent = domain.Chunk{
		ID: pic.ParentID, CourseID: pic.CourseID, SourceID: pic.SourceID,
		Text: quote(obs.Caption, text), Locators: []locator.Locator{pic.Locator},
	}
	child = domain.Chunk{
		ID: pic.ChildID, CourseID: pic.CourseID, SourceID: pic.SourceID,
		ParentID: pic.ParentID, Text: text, Locators: []locator.Locator{pic.Locator},
	}
	if err := parent.Validate(); err != nil {
		return domain.Chunk{}, domain.Chunk{}, false, err
	}
	if err := child.Validate(); err != nil {
		return domain.Chunk{}, domain.Chunk{}, false, err
	}
	return parent, child, true, nil
}

// quote is the text an answer may cite. The caption sits above the reading
// when the observation produced one.
func quote(caption, reading string) string {
	caption = strings.TrimSpace(caption)
	if caption == "" || caption == reading {
		return reading
	}
	return caption + "\n\n" + reading
}
