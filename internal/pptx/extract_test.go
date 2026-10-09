package pptx

import (
	"archive/zip"
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/sumonmselim/scholia-aws/internal/locator"
)

func TestDocumentRoundTrip(t *testing.T) {
	doc := Document{Slides: []Slide{{
		Number: 1,
		Spans: []Span{{
			Text:    "Hello",
			Locator: locator.Locator{Kind: locator.KindSlide, Slide: 1},
		}},
		Images: []Image{{Name: "image1.png", ContentType: "image/png", Bytes: []byte{1}}},
	}}}
	raw, err := Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Slides) != 1 || got.Slides[0].Spans[0].Text != "Hello" || len(got.Slides[0].Images[0].Bytes) != 0 {
		t.Fatalf("document %+v", got)
	}
	if ObjectKey("c", "s") != "courses/c/sources/s/derived/pptx.json" {
		t.Fatalf("json key %s", ObjectKey("c", "s"))
	}
	if ImageKey("c", "s", 1, "image1.png") != "courses/c/sources/s/derived/slide-1-image1.png" {
		t.Fatalf("image key %s", ImageKey("c", "s", 1, "image1.png"))
	}
}

func TestDocumentRejects(t *testing.T) {
	span := Span{Text: "A", Locator: locator.Locator{Kind: locator.KindSlide, Slide: 1}}
	cases := []Document{
		{},
		{Slides: []Slide{{Number: 2}}},
		{Slides: []Slide{{Number: 1, Spans: []Span{{Locator: span.Locator}}}}},
		{Slides: []Slide{{Number: 1, Spans: []Span{{Text: "A", Locator: locator.Locator{Kind: locator.KindSlide, Slide: 2}}}}}},
		{Slides: []Slide{{Number: 1, Images: []Image{{Name: "a.png"}}}}},
		{Slides: []Slide{{Number: 1, Images: []Image{{Name: "a.png", ContentType: "image/png"}, {Name: "a.png", ContentType: "image/png"}}}}},
	}
	for i, doc := range cases {
		if err := doc.Validate(); err == nil {
			t.Fatalf("case %d was accepted", i)
		}
	}
	if _, err := Decode([]byte(`{"slides":[],"extra":true}`)); err == nil {
		t.Fatal("unknown field was accepted")
	}
}

func TestGoldenDeck(t *testing.T) {
	doc, err := Extract(fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Slides) != 2 {
		t.Fatalf("slides %d", len(doc.Slides))
	}
	first := doc.Slides[0]
	if texts(first, false) != "Introduction|Packets travel in hops." {
		t.Fatalf("slide 1 text %q", texts(first, false))
	}
	if texts(first, true) != "Mention the diagram." {
		t.Fatalf("slide 1 notes %q", texts(first, true))
	}
	if len(first.Images) != 1 || first.Images[0].Name != "image1.png" || first.Images[0].ContentType != "image/png" || !bytes.HasPrefix(first.Images[0].Bytes, []byte("\x89PNG")) {
		t.Fatalf("slide 1 images %+v", first.Images)
	}
	if first.Spans[0].Locator.Slide != 1 {
		t.Fatalf("locator %+v", first.Spans[0].Locator)
	}
	second := doc.Slides[1]
	if texts(second, false) != "Routing|Tables pick the next hop." {
		t.Fatalf("slide 2 text %q", texts(second, false))
	}
	if texts(second, true) != "Ask who owns the table." {
		t.Fatalf("slide 2 notes %q", texts(second, true))
	}
	if len(second.Images) != 0 {
		t.Fatalf("slide 2 images %+v", second.Images)
	}
}

func TestImageNames(t *testing.T) {
	seen := map[string]int{}
	if imageName("a b.png", seen) != "a_b.png" || imageName("a b.png", seen) != "a_b-2.png" || imageName("", seen) != "image" {
		t.Fatalf("names %#v", seen)
	}
	cases := []struct{ name, typ string }{
		{"a.png", "image/png"},
		{"a.jpg", "image/jpeg"},
		{"a.jpeg", "image/jpeg"},
		{"a.gif", "image/gif"},
		{"a.emf", "image/emf"},
		{"a.wmf", "image/wmf"},
		{"a.tif", "image/tiff"},
		{"a.tiff", "image/tiff"},
		{"a.bin", "application/octet-stream"},
	}
	for _, tc := range cases {
		if imageType(tc.name) != tc.typ {
			t.Fatalf("%s -> %s", tc.name, imageType(tc.name))
		}
	}
}

func TestExtractRejects(t *testing.T) {
	if _, err := Extract(nil); err == nil {
		t.Fatal("empty deck was accepted")
	}
	if _, err := Extract([]byte("not a zip")); err == nil {
		t.Fatal("plain text was accepted")
	}
	escaped := zipParts(t, map[string][]byte{
		"ppt/presentation.xml":            []byte(`<?xml version="1.0"?><p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><p:sldIdLst><p:sldId id="1" r:id="rId1"/></p:sldIdLst></p:presentation>`),
		"ppt/_rels/presentation.xml.rels": []byte(`<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="../../etc/passwd"/></Relationships>`),
	})
	if _, err := Extract(escaped); err == nil {
		t.Fatal("escaped target was accepted")
	}

	const office = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	const relNS = "http://schemas.openxmlformats.org/package/2006/relationships"
	presentation := `<?xml version="1.0"?><p:presentation xmlns:r="` + office + `"><p:sldIdLst><p:sldId id="1" r:id="rId1"/></p:sldIdLst></p:presentation>`
	slideRels := func(target, typ string) string {
		return `<?xml version="1.0"?><Relationships xmlns="` + relNS + `"><Relationship Id="rId1" Type="` + office + typ + `" Target="` + target + `"/></Relationships>`
	}
	rejects := []struct {
		name  string
		parts map[string][]byte
	}{
		{name: "backslash", parts: map[string][]byte{
			"ppt/presentation.xml":            []byte(presentation),
			"ppt/_rels/presentation.xml.rels": []byte(slideRels(`slides\slide1.xml`, "/slide")),
		}},
		{name: "missing slide rel", parts: map[string][]byte{
			"ppt/presentation.xml":            []byte(presentation),
			"ppt/_rels/presentation.xml.rels": []byte(slideRels("slides/slide1.xml", "/slide")),
		}},
		{name: "broken slide", parts: map[string][]byte{
			"ppt/presentation.xml":            []byte(presentation),
			"ppt/_rels/presentation.xml.rels": []byte(slideRels("slides/slide1.xml", "/slide")),
			"ppt/slides/slide1.xml":           []byte(`<p:sld><a:t>no end`),
		}},
		{name: "no slides", parts: map[string][]byte{
			"ppt/presentation.xml":            []byte(`<?xml version="1.0"?><p:presentation xmlns:r="` + office + `"><p:sldIdLst></p:sldIdLst></p:presentation>`),
			"ppt/_rels/presentation.xml.rels": []byte(`<?xml version="1.0"?><Relationships xmlns="` + relNS + `"></Relationships>`),
		}},
	}
	for _, tc := range rejects {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Extract(zipParts(t, tc.parts)); err == nil {
				t.Fatal("invalid deck was accepted")
			}
		})
	}

	bare := zipParts(t, map[string][]byte{
		"ppt/presentation.xml":            []byte(presentation),
		"ppt/_rels/presentation.xml.rels": []byte(slideRels("slides/slide1.xml", "/slide")),
		"ppt/slides/slide1.xml":           []byte(`<?xml version="1.0"?><p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><p:txBody><a:p><a:r><a:t>Only</a:t></a:r></a:p></p:txBody></p:sld>`),
	})
	doc, err := Extract(bare)
	if err != nil || len(doc.Slides) != 1 || doc.Slides[0].Spans[0].Text != "Only" {
		t.Fatalf("bare slide %+v err %v", doc, err)
	}
}

func texts(slide Slide, notes bool) string {
	var b bytes.Buffer
	for _, span := range slide.Spans {
		if span.Notes != notes {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('|')
		}
		b.WriteString(span.Text)
	}
	return b.String()
}

func fixture(t *testing.T) []byte {
	t.Helper()
	path := filepath.Join("testdata", "lecture.pptx")
	if os.Getenv("SCHOLIA_WRITE_FIXTURES") == "1" {
		raw := lecturePPTX(t)
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0o644); err != nil {
			t.Fatal(err)
		}
		return raw
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func lecturePPTX(t *testing.T) []byte {
	t.Helper()
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	const relNS = "http://schemas.openxmlformats.org/package/2006/relationships"
	const office = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	slide := func(title, body string) string {
		return `<?xml version="1.0" encoding="UTF-8"?>` +
			`<p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main">` +
			`<p:cSld><p:spTree><p:sp><p:txBody>` +
			`<a:p><a:r><a:t>` + title + `</a:t></a:r></a:p>` +
			`<a:p><a:r><a:t>` + body + `</a:t></a:r></a:p>` +
			`</p:txBody></p:sp></p:spTree></p:cSld></p:sld>`
	}
	notes := func(text string) string {
		return `<?xml version="1.0" encoding="UTF-8"?>` +
			`<p:notes xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main">` +
			`<p:cSld><p:spTree><p:sp><p:txBody><a:p><a:r><a:t>` + text + `</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:notes>`
	}
	return zipParts(t, map[string][]byte{
		"ppt/presentation.xml": []byte(`<?xml version="1.0" encoding="UTF-8"?>` +
			`<p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:r="` + office + `">` +
			`<p:sldIdLst><p:sldId id="256" r:id="rId10"/><p:sldId id="257" r:id="rId20"/></p:sldIdLst></p:presentation>`),
		"ppt/_rels/presentation.xml.rels": []byte(`<?xml version="1.0"?>` +
			`<Relationships xmlns="` + relNS + `">` +
			`<Relationship Id="rId10" Type="` + office + `/slide" Target="slides/slide2.xml"/>` +
			`<Relationship Id="rId20" Type="` + office + `/slide" Target="slides/slide1.xml"/>` +
			`</Relationships>`),
		"ppt/slides/slide2.xml":           []byte(slide("Introduction", "Packets travel in hops.")),
		"ppt/slides/slide1.xml":           []byte(slide("Routing", "Tables pick the next hop.")),
		"ppt/notesSlides/notesSlide2.xml": []byte(notes("Mention the diagram.")),
		"ppt/notesSlides/notesSlide1.xml": []byte(notes("Ask who owns the table.")),
		"ppt/slides/_rels/slide2.xml.rels": []byte(`<?xml version="1.0"?>` +
			`<Relationships xmlns="` + relNS + `">` +
			`<Relationship Id="rId1" Type="` + office + `/notesSlide" Target="../notesSlides/notesSlide2.xml"/>` +
			`<Relationship Id="rId2" Type="` + office + `/image" Target="../media/image1.png"/>` +
			`<Relationship Id="rId3" Type="` + office + `/slideLayout" Target="../slideLayouts/slideLayout1.xml"/>` +
			`</Relationships>`),
		"ppt/slides/_rels/slide1.xml.rels": []byte(`<?xml version="1.0"?>` +
			`<Relationships xmlns="` + relNS + `">` +
			`<Relationship Id="rId1" Type="` + office + `/notesSlide" Target="../notesSlides/notesSlide1.xml"/>` +
			`</Relationships>`),
		"ppt/media/image1.png": pngBuf.Bytes(),
	})
}

func zipParts(t *testing.T, parts map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range parts {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
