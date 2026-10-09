// Package pdfdoc extracts PDF pages into text spans with page locators.
// Scanned pages are rendered for a later vision step and keep no text layer.
// WebAssembly PDFium is used so the worker does not need cgo.
package pdfdoc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/sumonmselim/scholia-aws/internal/locator"
)

const (
	// KindText is a page whose text layer is kept.
	KindText = "text"
	// KindScanned is a page rendered for OCR. Its text layer is discarded.
	KindScanned = "scanned"
)

// Span is one line of visible text and the page rectangle that holds it.
type Span struct {
	Text    string          `json:"text"`
	Locator locator.Locator `json:"locator"`
}

// Page is one PDF page after routing. PNG is set only for scanned pages and
// is stored beside the JSON, not inside it.
type Page struct {
	Number   int    `json:"number"`
	Kind     string `json:"kind"`
	NeedsOCR bool   `json:"needs_ocr,omitempty"`
	Spans    []Span `json:"spans,omitempty"`
	PNG      []byte `json:"-"`
}

// Document is the extraction result for one uploaded PDF.
type Document struct {
	Pages []Page `json:"pages"`
}

// Validate checks page numbers, kinds, and locators.
func (d Document) Validate() error {
	if len(d.Pages) == 0 {
		return errors.New("pdf has no pages")
	}
	for i, page := range d.Pages {
		if page.Number != i+1 {
			return fmt.Errorf("pdf page %d: number is %d", i+1, page.Number)
		}
		switch page.Kind {
		case KindText:
			if page.NeedsOCR || len(page.PNG) != 0 {
				return fmt.Errorf("pdf page %d: text pages are not rendered for OCR", page.Number)
			}
		case KindScanned:
			if !page.NeedsOCR || len(page.Spans) != 0 || len(page.PNG) == 0 {
				return fmt.Errorf("pdf page %d: scanned pages are rendered and have no text layer", page.Number)
			}
		default:
			return fmt.Errorf("pdf page %d: kind %q is not valid", page.Number, page.Kind)
		}
		for j, span := range page.Spans {
			if span.Text == "" {
				return fmt.Errorf("pdf page %d: span %d has no text", page.Number, j)
			}
			if span.Locator.Kind != locator.KindPage || span.Locator.Page != page.Number {
				return fmt.Errorf("pdf page %d: span %d locator must be that page", page.Number, j)
			}
			if err := span.Locator.Validate(); err != nil {
				return fmt.Errorf("pdf page %d: span %d: %w", page.Number, j, err)
			}
		}
	}
	return nil
}

// Encode writes the page routing. PNG bytes are omitted; the caller stores those separately.
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
		return Document{}, fmt.Errorf("pdf document: %w", err)
	}
	if err := doc.Validate(); err != nil {
		return Document{}, err
	}
	return doc, nil
}

// ObjectKey is the derived JSON for one PDF. The extra path segment keeps it
// off the upload key shape.
func ObjectKey(courseID, sourceID string) string {
	return "courses/" + courseID + "/sources/" + sourceID + "/derived/pdf.json"
}

// RenderKey is the PNG for a scanned page. page is 1-based.
func RenderKey(courseID, sourceID string, page int) string {
	return fmt.Sprintf("courses/%s/sources/%s/derived/page-%d.png", courseID, sourceID, page)
}
