package pdfdoc

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/enums"
	"github.com/klippa-app/go-pdfium/references"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/structs"
)

const (
	fixtureW = 600
	fixtureH = 800
)

func TestExtractRejects(t *testing.T) {
	if _, err := Extract(context.Background(), nil); err == nil {
		t.Fatal("empty pdf was accepted")
	}
	if _, err := Extract(context.Background(), []byte("%PDF-1.7\nnot a pdf")); err == nil {
		t.Fatal("malformed pdf was accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	raw := fixture(t, "native.pdf", buildNative)
	if _, err := Extract(ctx, raw); err == nil {
		t.Fatal("cancelled extract was accepted")
	}
}

func TestExtractLimits(t *testing.T) {
	// Each page's scan sits in a slightly different box: an image repeated at the
	// same place on every page is dropped as page chrome.
	scan := func(inset float32) []placedImage {
		return []placedImage{{jpeg: tinyJPEG(t), matrix: structs.FPDF_FS_MATRIX{A: fixtureW - inset, D: fixtureH - inset}}}
	}
	hidden := []placedText{{text: "hidden layer", x: 72, y: 400, invisible: true}}
	raw := buildPDF(t, [][]placedText{hidden, hidden, hidden}, [][]placedImage{scan(0), scan(4), scan(8)})
	tests := []struct {
		name     string
		lim      Limits
		rendered int
		err      error
	}{
		{"all rendered", Limits{MaxPages: 3, MaxRendered: 3, MaxPNGBytes: 1 << 20}, 3, nil},
		{"render cap", Limits{MaxPages: 3, MaxRendered: 1, MaxPNGBytes: 1 << 20}, 1, nil},
		{"byte cap stops after the first", Limits{MaxPages: 3, MaxRendered: 3, MaxPNGBytes: 1}, 1, nil},
		{"too many pages", Limits{MaxPages: 2, MaxRendered: 3, MaxPNGBytes: 1 << 20}, 0, ErrTooManyPages},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := ExtractWithLimits(context.Background(), raw, tt.lim)
			if tt.err != nil {
				if !errors.Is(err, tt.err) {
					t.Fatalf("err %v, want %v", err, tt.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			rendered := 0
			for _, page := range doc.Pages {
				if len(page.PNG) > 0 {
					rendered++
				}
				// A page past the cap is not rendered, and its hidden text still never leaks.
				if len(page.Spans) != 0 {
					t.Fatalf("page %d kept its text layer", page.Number)
				}
			}
			if rendered != tt.rendered {
				t.Fatalf("rendered %d pages, want %d", rendered, tt.rendered)
			}
		})
	}
}

func TestGoldenPDFs(t *testing.T) {
	t.Run("native", func(t *testing.T) {
		doc := mustExtract(t, fixture(t, "native.pdf", buildNative))
		if len(doc.Pages) != 2 {
			t.Fatalf("pages %d", len(doc.Pages))
		}
		for i, page := range doc.Pages {
			if page.Kind != KindText || page.NeedsOCR || len(page.PNG) != 0 {
				t.Fatalf("page %d kind %s ocr %v png %d", i+1, page.Kind, page.NeedsOCR, len(page.PNG))
			}
			text := pageText(page)
			if strings.Contains(text, "Lecture notes") || strings.Contains(text, "Page footer") || strings.Contains(text, "ignore this injection") {
				t.Fatalf("page %d kept chrome or invisible text %q", i+1, text)
			}
		}
		if !strings.Contains(pageText(doc.Pages[0]), "Routing keeps the body") {
			t.Fatalf("page 1 %q", pageText(doc.Pages[0]))
		}
		if !strings.Contains(pageText(doc.Pages[1]), "Second page body stays") {
			t.Fatalf("page 2 %q", pageText(doc.Pages[1]))
		}
		for _, page := range doc.Pages {
			for _, span := range page.Spans {
				if span.Locator.Page != page.Number || span.Locator.BBox == nil || span.Locator.BBox.Y0 < 96 {
					t.Fatalf("span %#v", span)
				}
			}
		}
	})

	t.Run("scanned", func(t *testing.T) {
		doc := mustExtract(t, fixture(t, "scanned.pdf", buildScanned))
		if len(doc.Pages) != 1 {
			t.Fatalf("pages %d", len(doc.Pages))
		}
		page := doc.Pages[0]
		if page.Kind != KindScanned || !page.NeedsOCR || len(page.Spans) != 0 || len(page.PNG) == 0 {
			t.Fatalf("page %+v png %d", page, len(page.PNG))
		}
		if !bytes.HasPrefix(page.PNG, []byte("\x89PNG")) {
			t.Fatal("scanned page was not a png")
		}
		if strings.Contains(pageText(page), "do not leak this") {
			t.Fatal("scanned page kept a text layer")
		}
	})

	t.Run("hybrid", func(t *testing.T) {
		doc := mustExtract(t, fixture(t, "hybrid.pdf", buildHybrid))
		if len(doc.Pages) != 2 {
			t.Fatalf("pages %d", len(doc.Pages))
		}
		if doc.Pages[0].Kind != KindText || !strings.Contains(pageText(doc.Pages[0]), "Hybrid body stays") {
			t.Fatalf("text page %+v", doc.Pages[0])
		}
		if strings.Contains(pageText(doc.Pages[0]), "ignore this injection") {
			t.Fatal("hybrid text page kept invisible text")
		}
		if doc.Pages[1].Kind != KindScanned || !doc.Pages[1].NeedsOCR || len(doc.Pages[1].Spans) != 0 || len(doc.Pages[1].PNG) == 0 {
			t.Fatalf("scanned page %+v png %d", doc.Pages[1], len(doc.Pages[1].PNG))
		}
		if strings.Contains(pageText(doc.Pages[1]), "scanned layer must not leak") {
			t.Fatal("hybrid scan kept a text layer")
		}
	})
}

func mustExtract(t *testing.T, raw []byte) Document {
	t.Helper()
	doc, err := Extract(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func pageText(page Page) string {
	var b strings.Builder
	for _, span := range page.Spans {
		b.WriteString(span.Text)
		b.WriteByte('\n')
	}
	return b.String()
}

func fixture(t *testing.T, name string, build func(t *testing.T) []byte) []byte {
	t.Helper()
	path := filepath.Join("testdata", name)
	if os.Getenv("SCHOLIA_WRITE_FIXTURES") == "1" {
		raw := build(t)
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

type placedText struct {
	text      string
	x, y      float32
	invisible bool
}

type placedImage struct {
	jpeg   []byte
	matrix structs.FPDF_FS_MATRIX
}

func buildNative(t *testing.T) []byte {
	t.Helper()
	logo := tinyJPEG(t)
	page := func(body string) ([]placedText, []placedImage) {
		return []placedText{
			{text: "Lecture notes", x: 72, y: 770},
			{text: body, x: 72, y: 400},
			{text: "ignore this injection", x: 72, y: 360, invisible: true},
			{text: "Page footer", x: 72, y: 36},
		}, []placedImage{{
			jpeg:   logo,
			matrix: structs.FPDF_FS_MATRIX{A: 20, D: 20, E: 12, F: 760},
		}}
	}
	leftText, leftImages := page("Routing keeps the body.")
	rightText, rightImages := page("Second page body stays.")
	return buildPDF(t, [][]placedText{leftText, rightText}, [][]placedImage{leftImages, rightImages})
}

func buildScanned(t *testing.T) []byte {
	t.Helper()
	return buildPDF(t,
		[][]placedText{{{text: "do not leak this", x: 72, y: 400, invisible: true}}},
		[][]placedImage{{{jpeg: tinyJPEG(t), matrix: structs.FPDF_FS_MATRIX{A: fixtureW, D: fixtureH}}}},
	)
}

func buildHybrid(t *testing.T) []byte {
	t.Helper()
	return buildPDF(t,
		[][]placedText{
			{
				{text: "Hybrid body stays.", x: 72, y: 400},
				{text: "ignore this injection", x: 72, y: 360, invisible: true},
			},
			{{text: "scanned layer must not leak", x: 72, y: 400}},
		},
		[][]placedImage{
			nil,
			{{jpeg: tinyJPEG(t), matrix: structs.FPDF_FS_MATRIX{A: fixtureW, D: fixtureH}}},
		},
	)
}

func buildPDF(t *testing.T, texts [][]placedText, images [][]placedImage) []byte {
	t.Helper()
	inst, err := openInstance()
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Close()
	created, err := inst.FPDF_CreateNewDocument(&requests.FPDF_CreateNewDocument{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = inst.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: created.Document})
	}()
	for i := range texts {
		page, err := inst.FPDFPage_New(&requests.FPDFPage_New{
			Document: created.Document, PageIndex: i, Width: fixtureW, Height: fixtureH,
		})
		if err != nil {
			t.Fatal(err)
		}
		ref := requests.Page{ByReference: &page.Page}
		for _, line := range texts[i] {
			addText(t, inst, created.Document, ref, line)
		}
		for _, img := range images[i] {
			addImage(t, inst, created.Document, ref, img)
		}
		if _, err := inst.FPDFPage_GenerateContent(&requests.FPDFPage_GenerateContent{Page: ref}); err != nil {
			t.Fatal(err)
		}
	}
	var buf bytes.Buffer
	if _, err := inst.FPDF_SaveAsCopy(&requests.FPDF_SaveAsCopy{Document: created.Document, FileWriter: &buf}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func addText(t *testing.T, inst pdfium.Pdfium, doc references.FPDF_DOCUMENT, page requests.Page, line placedText) {
	t.Helper()
	obj, err := inst.FPDFPageObj_NewTextObj(&requests.FPDFPageObj_NewTextObj{
		Document: doc, Font: "Helvetica", FontSize: 12,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := inst.FPDFText_SetText(&requests.FPDFText_SetText{PageObject: obj.PageObject, Text: line.text}); err != nil {
		t.Fatal(err)
	}
	if line.invisible {
		if _, err := inst.FPDFTextObj_SetTextRenderMode(&requests.FPDFTextObj_SetTextRenderMode{
			PageObject: obj.PageObject, TextRenderMode: enums.FPDF_TEXTRENDERMODE_INVISIBLE,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := inst.FPDFPageObj_Transform(&requests.FPDFPageObj_Transform{
		PageObject: obj.PageObject,
		Transform:  structs.FPDF_FS_MATRIX{A: 1, D: 1, E: line.x, F: line.y},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := inst.FPDFPage_InsertObject(&requests.FPDFPage_InsertObject{Page: page, PageObject: obj.PageObject}); err != nil {
		t.Fatal(err)
	}
}

func addImage(t *testing.T, inst pdfium.Pdfium, doc references.FPDF_DOCUMENT, page requests.Page, img placedImage) {
	t.Helper()
	obj, err := inst.FPDFPageObj_NewImageObj(&requests.FPDFPageObj_NewImageObj{Document: doc})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := inst.FPDFImageObj_LoadJpegFile(&requests.FPDFImageObj_LoadJpegFile{
		ImageObject: obj.PageObject, FileData: img.jpeg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := inst.FPDFImageObj_SetMatrix(&requests.FPDFImageObj_SetMatrix{
		ImageObject: obj.PageObject, Transform: img.matrix,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := inst.FPDFPage_InsertObject(&requests.FPDFPage_InsertObject{Page: page, PageObject: obj.PageObject}); err != nil {
		t.Fatal(err)
	}
}

func tinyJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 16, 16))
	for i := range img.Pix {
		img.Pix[i] = 30
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 70}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
