package pdfdoc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"sync"
	"time"
	"unicode"

	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/enums"
	"github.com/klippa-app/go-pdfium/references"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
)

// ErrTooManyPages means the document is longer than Limits.MaxPages.
var ErrTooManyPages = errors.New("pdf has too many pages")

var (
	poolOnce sync.Once
	pool     pdfium.Pool
	poolErr  error
)

func openInstance() (pdfium.Pdfium, error) {
	poolOnce.Do(func() {
		// One worker matches the Lambda, which handles a single upload at a time.
		pool, poolErr = webassembly.Init(webassembly.Config{MinIdle: 1, MaxIdle: 1, MaxTotal: 1})
	})
	if poolErr != nil {
		return nil, poolErr
	}
	return pool.GetInstance(2 * time.Minute)
}

// Limits bound the work one PDF can cause. A small file can hold thousands of
// pages or full-page images, and every rendered page is held in memory.
type Limits struct {
	// MaxPages rejects a longer document.
	MaxPages int
	// MaxRendered is how many scanned pages are rendered. Later scanned pages
	// keep no text: their text layer is dropped all the same.
	MaxRendered int
	// MaxPNGBytes stops rendering once the rendered pages reach this size.
	MaxPNGBytes int
}

// DefaultLimits suit a course upload in the worker.
var DefaultLimits = Limits{MaxPages: 1000, MaxRendered: 100, MaxPNGBytes: 128 << 20}

// Extract is ExtractWithLimits with DefaultLimits.
func Extract(ctx context.Context, raw []byte) (Document, error) {
	return ExtractWithLimits(ctx, raw, DefaultLimits)
}

// ExtractWithLimits routes each page. Text pages keep visible lines. Scanned pages
// are rendered and their text layer is dropped, including any invisible text a
// scanner or a prompt-injection payload hid in the file.
func ExtractWithLimits(ctx context.Context, raw []byte, lim Limits) (Document, error) {
	if len(raw) == 0 {
		return Document{}, fmt.Errorf("pdf is empty")
	}
	inst, err := openInstance()
	if err != nil {
		return Document{}, err
	}
	defer inst.Close()

	opened, err := inst.OpenDocument(&requests.OpenDocument{File: &raw})
	if err != nil {
		return Document{}, fmt.Errorf("open pdf: %w", err)
	}
	defer func() {
		_, _ = inst.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: opened.Document})
	}()

	count, err := inst.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: opened.Document})
	if err != nil {
		return Document{}, err
	}
	if count.PageCount < 1 {
		return Document{}, fmt.Errorf("pdf has no pages")
	}
	if count.PageCount > lim.MaxPages {
		return Document{}, fmt.Errorf("pdf has %d pages, more than %d: %w", count.PageCount, lim.MaxPages, ErrTooManyPages)
	}

	doc := Document{Pages: make([]Page, count.PageCount)}
	heights := make([]float64, count.PageCount)
	widths := make([]float64, count.PageCount)
	images := make([][]imageBox, count.PageCount)
	for i := 0; i < count.PageCount; i++ {
		if err := ctx.Err(); err != nil {
			return Document{}, err
		}
		page, err := readPage(inst, opened.Document, i)
		if err != nil {
			return Document{}, fmt.Errorf("page %d: %w", i+1, err)
		}
		doc.Pages[i] = Page{Number: i + 1, Kind: KindText, Spans: page.spans}
		widths[i], heights[i], images[i] = page.width, page.height, page.images
	}

	images = keptImages(images, widths, heights)
	dropRunningMargins(doc.Pages, heights)
	rendered, pngTotal := 0, 0
	for i := range doc.Pages {
		if !isScanned(widths[i], heights[i], images[i]) {
			continue
		}
		doc.Pages[i].Spans = nil
		if rendered >= lim.MaxRendered || pngTotal >= lim.MaxPNGBytes {
			continue
		}
		if err := ctx.Err(); err != nil {
			return Document{}, err
		}
		pngBytes, err := renderPage(inst, opened.Document, i)
		if err != nil {
			return Document{}, fmt.Errorf("render page %d: %w", i+1, err)
		}
		rendered++
		pngTotal += len(pngBytes)
		doc.Pages[i].Kind = KindScanned
		doc.Pages[i].NeedsOCR = true
		doc.Pages[i].PNG = pngBytes
	}
	if err := doc.Validate(); err != nil {
		return Document{}, err
	}
	return doc, nil
}

type rawPage struct {
	width, height float64
	spans         []Span
	images        []imageBox
}

func readPage(inst pdfium.Pdfium, document references.FPDF_DOCUMENT, index int) (rawPage, error) {
	size, err := inst.FPDF_GetPageSizeByIndex(&requests.FPDF_GetPageSizeByIndex{Document: document, Index: index})
	if err != nil {
		return rawPage{}, err
	}
	page := requests.Page{ByIndex: &requests.PageByIndex{Document: document, Index: index}}
	textPage, err := inst.FPDFText_LoadPage(&requests.FPDFText_LoadPage{Page: page})
	if err != nil {
		return rawPage{}, err
	}
	defer func() {
		_, _ = inst.FPDFText_ClosePage(&requests.FPDFText_ClosePage{TextPage: textPage.TextPage})
	}()

	glyphs, err := readGlyphs(inst, textPage.TextPage, size.Width, size.Height)
	if err != nil {
		return rawPage{}, err
	}
	images, err := readImages(inst, page)
	if err != nil {
		return rawPage{}, err
	}
	return rawPage{
		width: size.Width, height: size.Height,
		spans: spansFromGlyphs(index+1, glyphs), images: images,
	}, nil
}

func readGlyphs(inst pdfium.Pdfium, textPage references.FPDF_TEXTPAGE, pageW, pageH float64) ([]glyph, error) {
	counted, err := inst.FPDFText_CountChars(&requests.FPDFText_CountChars{TextPage: textPage})
	if err != nil {
		return nil, err
	}
	glyphs := make([]glyph, 0, counted.Count)
	for i := 0; i < counted.Count; i++ {
		char, err := inst.FPDFText_GetUnicode(&requests.FPDFText_GetUnicode{TextPage: textPage, Index: i})
		if err != nil {
			return nil, err
		}
		if char.Unicode > unicode.MaxRune {
			continue
		}
		r := rune(char.Unicode)
		if r == 0 {
			continue
		}
		visible, err := glyphVisible(inst, textPage, i)
		if err != nil {
			return nil, err
		}
		if !visible && r != '\n' && r != '\r' {
			continue
		}
		boxResp, err := inst.FPDFText_GetCharBox(&requests.FPDFText_GetCharBox{TextPage: textPage, Index: i})
		if err != nil {
			return nil, err
		}
		box, ok := pageBBox(pageW, pageH, boxResp.Left, boxResp.Bottom, boxResp.Right, boxResp.Top)
		if !ok {
			continue
		}
		sizeResp, err := inst.FPDFText_GetFontSize(&requests.FPDFText_GetFontSize{TextPage: textPage, Index: i})
		if err != nil {
			return nil, err
		}
		if sizeResp.FontSize < 0.5 && r != '\n' && r != '\r' {
			continue
		}
		glyphs = append(glyphs, glyph{r: r, box: box})
	}
	return glyphs, nil
}

func glyphVisible(inst pdfium.Pdfium, textPage references.FPDF_TEXTPAGE, index int) (bool, error) {
	obj, err := inst.FPDFText_GetTextObject(&requests.FPDFText_GetTextObject{TextPage: textPage, Index: index})
	if err != nil {
		// Some characters have no page object. They stay; render mode is checked when one exists.
		return true, nil
	}
	mode, err := inst.FPDFTextObj_GetTextRenderMode(&requests.FPDFTextObj_GetTextRenderMode{PageObject: obj.TextObject})
	if err != nil {
		return false, err
	}
	switch mode.TextRenderMode {
	case enums.FPDF_TEXTRENDERMODE_INVISIBLE, enums.FPDF_TEXTRENDERMODE_CLIP:
		return false, nil
	default:
		return true, nil
	}
}

func readImages(inst pdfium.Pdfium, page requests.Page) ([]imageBox, error) {
	counted, err := inst.FPDFPage_CountObjects(&requests.FPDFPage_CountObjects{Page: page})
	if err != nil {
		return nil, err
	}
	var images []imageBox
	for i := 0; i < counted.Count; i++ {
		obj, err := inst.FPDFPage_GetObject(&requests.FPDFPage_GetObject{Page: page, Index: i})
		if err != nil {
			return nil, err
		}
		kind, err := inst.FPDFPageObj_GetType(&requests.FPDFPageObj_GetType{PageObject: obj.PageObject})
		if err != nil {
			return nil, err
		}
		if kind.Type != enums.FPDF_PAGEOBJ_IMAGE {
			continue
		}
		bounds, err := inst.FPDFPageObj_GetBounds(&requests.FPDFPageObj_GetBounds{PageObject: obj.PageObject})
		if err != nil {
			return nil, err
		}
		images = append(images, imageBox{
			left: float64(bounds.Left), bottom: float64(bounds.Bottom),
			right: float64(bounds.Right), top: float64(bounds.Top),
		})
	}
	return images, nil
}

func renderPage(inst pdfium.Pdfium, document references.FPDF_DOCUMENT, index int) ([]byte, error) {
	rendered, err := inst.RenderPageInDPI(&requests.RenderPageInDPI{
		Page: requests.Page{ByIndex: &requests.PageByIndex{Document: document, Index: index}},
		DPI:  72,
	})
	if err != nil {
		return nil, err
	}
	defer rendered.Cleanup()
	if rendered.Result.Image == nil {
		return nil, fmt.Errorf("render produced no image")
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, rendered.Result.Image); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
