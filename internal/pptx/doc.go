// Package pptx reads a PowerPoint deck into slide text, speaker notes, and
// embedded images. Images are kept for a later vision step and are not captioned.
package pptx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/sumonmselim/scholia-aws/internal/locator"
)

// Span is one paragraph of slide text or speaker notes.
type Span struct {
	Text    string          `json:"text"`
	Notes   bool            `json:"notes,omitempty"`
	Locator locator.Locator `json:"locator"`
}

// Image is one embedded picture on a slide. Bytes are stored beside the JSON.
type Image struct {
	Name        string `json:"name"`
	ContentType string `json:"content_type"`
	Bytes       []byte `json:"-"`
}

// Slide is one slide in presentation order. Number is 1-based.
type Slide struct {
	Number int     `json:"number"`
	Spans  []Span  `json:"spans,omitempty"`
	Images []Image `json:"images,omitempty"`
}

// Document is the extraction result for one uploaded deck.
type Document struct {
	Slides []Slide `json:"slides"`
}

// Validate checks slide order, locators, and image names.
func (d Document) Validate() error {
	if len(d.Slides) == 0 {
		return errors.New("pptx has no slides")
	}
	for i, slide := range d.Slides {
		if slide.Number != i+1 {
			return fmt.Errorf("pptx slide %d: number is %d", i+1, slide.Number)
		}
		for j, span := range slide.Spans {
			if span.Text == "" {
				return fmt.Errorf("pptx slide %d: span %d has no text", slide.Number, j)
			}
			if span.Locator.Kind != locator.KindSlide || span.Locator.Slide != slide.Number {
				return fmt.Errorf("pptx slide %d: span %d locator must be that slide", slide.Number, j)
			}
			if err := span.Locator.Validate(); err != nil {
				return fmt.Errorf("pptx slide %d: span %d: %w", slide.Number, j, err)
			}
		}
		seen := map[string]bool{}
		for j, image := range slide.Images {
			if image.Name == "" || image.ContentType == "" || seen[image.Name] {
				return fmt.Errorf("pptx slide %d: image %d is missing a name or content type", slide.Number, j)
			}
			seen[image.Name] = true
		}
	}
	return nil
}

// Encode writes the deck. Image bytes are omitted; the caller stores those separately.
func Encode(doc Document) ([]byte, error) {
	if err := doc.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(doc)
}

// Decode reads a document written by Encode.
func Decode(raw []byte) (Document, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var doc Document
	if err := dec.Decode(&doc); err != nil {
		return Document{}, fmt.Errorf("pptx document: %w", err)
	}
	if err := doc.Validate(); err != nil {
		return Document{}, err
	}
	return doc, nil
}

// ObjectKey is the derived JSON for one deck. The extra path segment keeps it
// off the upload key shape.
func ObjectKey(courseID, sourceID string) string {
	return "courses/" + courseID + "/sources/" + sourceID + "/derived/pptx.json"
}

// ImageKey is one embedded image. slide is 1-based. name is a single path segment.
func ImageKey(courseID, sourceID string, slide int, name string) string {
	return fmt.Sprintf("courses/%s/sources/%s/derived/slide-%d-%s", courseID, sourceID, slide, name)
}
