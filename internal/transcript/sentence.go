// Package transcript turns a lecture recording into sentences with time locators.
// Transcript importers emit this same sentence type.
package transcript

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/sumonmselim/scholia-aws/internal/locator"
)

// Sentence is one spoken line. Speaker is empty when the recording has no labels.
// Locator is a time locator covering the words in the line.
type Sentence struct {
	Text    string          `json:"text"`
	Speaker string          `json:"speaker,omitempty"`
	Locator locator.Locator `json:"locator"`
}

// Validate checks the line and its time locator.
func (s Sentence) Validate() error {
	if s.Text == "" {
		return errors.New("sentence: text is required")
	}
	if s.Locator.Kind != locator.KindTime {
		return errors.New("sentence: locator must be a time locator")
	}
	if err := s.Locator.Validate(); err != nil {
		return fmt.Errorf("sentence: %w", err)
	}
	return nil
}

type document struct {
	Sentences []Sentence `json:"sentences"`
}

// Encode writes the sentence list. An empty list is rejected so a finished
// lecture cannot be stored as a blank transcript.
func Encode(sentences []Sentence) ([]byte, error) {
	if len(sentences) == 0 {
		return nil, errors.New("transcript has no sentences")
	}
	for i, s := range sentences {
		if err := s.Validate(); err != nil {
			return nil, fmt.Errorf("sentence %d: %w", i, err)
		}
	}
	return json.Marshal(document{Sentences: sentences})
}

// Decode reads a transcript written by Encode.
func Decode(raw []byte) ([]Sentence, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var doc document
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("transcript: %w", err)
	}
	if len(doc.Sentences) == 0 {
		return nil, errors.New("transcript has no sentences")
	}
	for i, s := range doc.Sentences {
		if err := s.Validate(); err != nil {
			return nil, fmt.Errorf("sentence %d: %w", i, err)
		}
	}
	return doc.Sentences, nil
}

// ObjectKey is where the worker stores the sentence transcript.
// The extra path segment keeps it off the upload key shape, so the upload
// handler does not treat the transcript as a new source file.
func ObjectKey(courseID, sourceID string) string {
	return "courses/" + courseID + "/sources/" + sourceID + "/derived/transcript.json"
}
