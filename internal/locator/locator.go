// Package locator is the source position that travels with an extracted span onto its chunk.
package locator

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
)

// Kind selects which position a Locator carries.
type Kind string

// Position kinds. Page and slide numbers are 1-based.
const (
	KindPage  Kind = "page"
	KindSlide Kind = "slide"
	KindTime  Kind = "time"
	KindText  Kind = "text"
)

// BBox is a rectangle in page user space: origin at the top-left of the page,
// x increasing right, y increasing down, units in PDF points.
type BBox struct {
	X0 float64 `json:"x0"`
	Y0 float64 `json:"y0"`
	X1 float64 `json:"x1"`
	Y1 float64 `json:"y1"`
}

// Locator is one position in a source. Only the fields for Kind are stored.
// Page and slide numbers are 1-based. Time is milliseconds. Text offsets are
// byte offsets into the source, start inclusive and end exclusive.
type Locator struct {
	Kind    Kind
	Page    int
	BBox    *BBox
	Slide   int
	StartMS int64
	EndMS   int64
	Start   int
	End     int
}

// Validate checks the fields that belong to Kind.
func (l Locator) Validate() error {
	switch l.Kind {
	case KindPage:
		if l.Page < 1 {
			return errors.New("page locator: page must be >= 1")
		}
		if l.BBox == nil {
			return errors.New("page locator: bbox is required")
		}
		return validateBBox(*l.BBox)
	case KindSlide:
		if l.Slide < 1 {
			return errors.New("slide locator: slide must be >= 1")
		}
		return nil
	case KindTime:
		if l.StartMS < 0 || l.EndMS < l.StartMS {
			return errors.New("time locator: end_ms must be >= start_ms and both >= 0")
		}
		return nil
	case KindText:
		if l.Start < 0 || l.End < l.Start {
			return errors.New("text locator: end must be >= start and both >= 0")
		}
		return nil
	default:
		return fmt.Errorf("locator: kind %q is not valid", l.Kind)
	}
}

func validateBBox(b BBox) error {
	if !finite(b.X0) || !finite(b.Y0) || !finite(b.X1) || !finite(b.Y1) {
		return errors.New("page locator: bbox values must be finite")
	}
	if b.X0 < 0 || b.Y0 < 0 || b.X1 < b.X0 || b.Y1 < b.Y0 {
		return errors.New("page locator: bbox must sit in the page and x1 >= x0, y1 >= y0")
	}
	return nil
}

func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

type pageBody struct {
	Kind Kind  `json:"kind"`
	Page int   `json:"page"`
	BBox *BBox `json:"bbox"`
}

type slideBody struct {
	Kind  Kind `json:"kind"`
	Slide int  `json:"slide"`
}

type timeBody struct {
	Kind    Kind  `json:"kind"`
	StartMS int64 `json:"start_ms"`
	EndMS   int64 `json:"end_ms"`
}

type textBody struct {
	Kind  Kind `json:"kind"`
	Start int  `json:"start"`
	End   int  `json:"end"`
}

// MarshalJSON writes only the fields for Kind, including zeroes such as start_ms 0.
func (l Locator) MarshalJSON() ([]byte, error) {
	if err := l.Validate(); err != nil {
		return nil, err
	}
	switch l.Kind {
	case KindPage:
		return json.Marshal(pageBody{Kind: l.Kind, Page: l.Page, BBox: l.BBox})
	case KindSlide:
		return json.Marshal(slideBody{Kind: l.Kind, Slide: l.Slide})
	case KindTime:
		return json.Marshal(timeBody{Kind: l.Kind, StartMS: l.StartMS, EndMS: l.EndMS})
	case KindText:
		return json.Marshal(textBody{Kind: l.Kind, Start: l.Start, End: l.End})
	default:
		return nil, fmt.Errorf("locator: kind %q is not valid", l.Kind)
	}
}

// UnmarshalJSON accepts one locator object and rejects fields that do not belong to its kind.
func (l *Locator) UnmarshalJSON(data []byte) error {
	var head struct {
		Kind Kind `json:"kind"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return fmt.Errorf("locator: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var parsed Locator
	switch head.Kind {
	case KindPage:
		var body pageBody
		if err := dec.Decode(&body); err != nil {
			return fmt.Errorf("page locator: %w", err)
		}
		parsed = Locator{Kind: body.Kind, Page: body.Page, BBox: body.BBox}
	case KindSlide:
		var body slideBody
		if err := dec.Decode(&body); err != nil {
			return fmt.Errorf("slide locator: %w", err)
		}
		parsed = Locator{Kind: body.Kind, Slide: body.Slide}
	case KindTime:
		var body timeBody
		if err := dec.Decode(&body); err != nil {
			return fmt.Errorf("time locator: %w", err)
		}
		parsed = Locator{Kind: body.Kind, StartMS: body.StartMS, EndMS: body.EndMS}
	case KindText:
		var body textBody
		if err := dec.Decode(&body); err != nil {
			return fmt.Errorf("text locator: %w", err)
		}
		parsed = Locator{Kind: body.Kind, Start: body.Start, End: body.End}
	default:
		return fmt.Errorf("locator: kind %q is not valid", head.Kind)
	}
	if err := parsed.Validate(); err != nil {
		return err
	}
	*l = parsed
	return nil
}
