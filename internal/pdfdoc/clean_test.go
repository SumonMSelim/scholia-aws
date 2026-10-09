package pdfdoc

import (
	"bytes"
	"testing"

	"github.com/sumonmselim/scholia-aws/internal/locator"
)

func TestDocumentRoundTrip(t *testing.T) {
	box := locator.BBox{X0: 10, Y0: 20, X1: 80, Y1: 40}
	doc := Document{Pages: []Page{{
		Number: 1,
		Kind:   KindText,
		Spans: []Span{{
			Text:    "Kept",
			Locator: locator.Locator{Kind: locator.KindPage, Page: 1, BBox: &box},
		}},
	}}}
	raw, err := Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Pages) != 1 || got.Pages[0].Spans[0].Text != "Kept" || got.Pages[0].Spans[0].Locator.Page != 1 {
		t.Fatalf("document %+v", got)
	}
	if ObjectKey("c", "s") != "courses/c/sources/s/derived/pdf.json" {
		t.Fatalf("json key %s", ObjectKey("c", "s"))
	}
	if RenderKey("c", "s", 2) != "courses/c/sources/s/derived/page-2.png" {
		t.Fatalf("png key %s", RenderKey("c", "s", 2))
	}
}

func TestDocumentRejects(t *testing.T) {
	box := locator.BBox{X0: 1, Y0: 1, X1: 2, Y1: 2}
	span := Span{Text: "A", Locator: locator.Locator{Kind: locator.KindPage, Page: 1, BBox: &box}}
	cases := []struct {
		name string
		doc  Document
		raw  string
	}{
		{name: "no pages", doc: Document{}},
		{name: "number", doc: Document{Pages: []Page{{Number: 2, Kind: KindText}}}},
		{name: "kind", doc: Document{Pages: []Page{{Number: 1, Kind: "slide"}}}},
		{name: "text rendered", doc: Document{Pages: []Page{{Number: 1, Kind: KindText, NeedsOCR: true}}}},
		{name: "scanned text", doc: Document{Pages: []Page{{Number: 1, Kind: KindScanned, NeedsOCR: true, PNG: []byte{1}, Spans: []Span{span}}}}},
		{name: "scanned missing png", doc: Document{Pages: []Page{{Number: 1, Kind: KindScanned, NeedsOCR: true}}}},
		{name: "empty span", doc: Document{Pages: []Page{{Number: 1, Kind: KindText, Spans: []Span{{Locator: span.Locator}}}}}},
		{name: "bad locator", doc: Document{Pages: []Page{{Number: 1, Kind: KindText, Spans: []Span{{Text: "A", Locator: locator.Locator{Kind: locator.KindPage, Page: 2, BBox: &box}}}}}}},
		{name: "unknown json", raw: `{"pages":[],"extra":true}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.raw != "" {
				if _, err := Decode([]byte(tc.raw)); err == nil {
					t.Fatal("invalid document was accepted")
				}
				return
			}
			if err := tc.doc.Validate(); err == nil {
				t.Fatal("invalid document was accepted")
			}
		})
	}
	if _, err := Encode(Document{}); err == nil {
		t.Fatal("empty document was encoded")
	}
}

func TestSpansAndMargins(t *testing.T) {
	box := func(y0, y1 float64) locator.BBox {
		return locator.BBox{X0: 10, Y0: y0, X1: 100, Y1: y1}
	}
	page := func(n int, spans ...Span) Page {
		return Page{Number: n, Kind: KindText, Spans: spans}
	}
	line := func(n int, text string, y0, y1 float64) Span {
		b := box(y0, y1)
		return Span{Text: text, Locator: locator.Locator{Kind: locator.KindPage, Page: n, BBox: &b}}
	}
	pages := []Page{
		page(1, line(1, "Lecture notes", 10, 24), line(1, "Body one", 200, 220), line(1, "Page footer", 760, 780)),
		page(2, line(2, "Lecture notes", 10, 24), line(2, "Body two", 200, 220), line(2, "Page footer", 760, 780)),
	}
	dropRunningMargins(pages, []float64{800, 800})
	if texts := joined(pages[0].Spans); texts != "Body one" {
		t.Fatalf("page 1 %q", texts)
	}
	if texts := joined(pages[1].Spans); texts != "Body two" {
		t.Fatalf("page 2 %q", texts)
	}

	single := []Page{page(1, line(1, "Lecture notes", 10, 24), line(1, "Only page", 200, 220))}
	dropRunningMargins(single, []float64{800})
	if texts := joined(single[0].Spans); texts != "Lecture notes Only page" {
		t.Fatalf("single page %q", texts)
	}

	glyphs := []glyph{
		{r: 'A', box: locator.BBox{X0: 10, Y0: 20, X1: 18, Y1: 32}},
		{r: 'B', box: locator.BBox{X0: 30, Y0: 20, X1: 38, Y1: 32}},
		{r: '\n'},
		{r: 'C', box: locator.BBox{X0: 10, Y0: 40, X1: 18, Y1: 52}},
	}
	spans := spansFromGlyphs(3, glyphs)
	if len(spans) != 2 || spans[0].Text != "A B" || spans[1].Text != "C" || spans[0].Locator.Page != 3 {
		t.Fatalf("spans %#v", spans)
	}
	if spans[0].Locator.BBox.X1 != 38 || spans[0].Locator.BBox.Y1 != 32 {
		t.Fatalf("union %#v", spans[0].Locator.BBox)
	}

	if _, ok := pageBBox(100, 100, -50, -50, -10, -10); ok {
		t.Fatal("off-page box was kept")
	}
	got, ok := pageBBox(100, 200, 10, 20, 40, 50)
	if !ok || got.X0 != 10 || got.Y0 != 150 || got.X1 != 40 || got.Y1 != 180 {
		t.Fatalf("box %#v ok %v", got, ok)
	}
}

func TestImageFilter(t *testing.T) {
	logo := imageBox{left: 10, bottom: 760, right: 30, top: 780}
	scan := imageBox{left: 0, bottom: 0, right: 600, top: 800}
	pages := keptImages([][]imageBox{{logo, scan}, {logo}}, []float64{600, 600}, []float64{800, 800})
	if len(pages[0]) != 1 || pages[0][0] != scan {
		t.Fatalf("page 1 images %#v", pages[0])
	}
	if len(pages[1]) != 0 {
		t.Fatalf("page 2 images %#v", pages[1])
	}
	if !isScanned(600, 800, pages[0]) {
		t.Fatal("full-page image was not scanned")
	}
	if isScanned(600, 800, nil) {
		t.Fatal("empty page was scanned")
	}
	if isScanned(0, 0, []imageBox{scan}) {
		t.Fatal("zero page was scanned")
	}
}

func joined(spans []Span) string {
	var b bytes.Buffer
	for i, span := range spans {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(span.Text)
	}
	return b.String()
}
