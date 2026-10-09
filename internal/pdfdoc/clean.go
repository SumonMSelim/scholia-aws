package pdfdoc

import (
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/sumonmselim/scholia-aws/internal/locator"
)

// marginFraction is the top and bottom band where a repeated line is a
// running header or footer rather than body text.
const marginFraction = 0.12

// smallImageFraction drops logos and other marks that are not page content.
const smallImageFraction = 0.05

// scannedFraction is the remaining image area, relative to the page, above
// which the page is a scan and its text layer is not trusted.
const scannedFraction = 0.45

// glyph is one visible character in top-left page space.
type glyph struct {
	r   rune
	box locator.BBox
}

// imageBox is one image in PDF user space (origin bottom-left, y up).
type imageBox struct {
	left, bottom, right, top float64
}

func spansFromGlyphs(page int, glyphs []glyph) []Span {
	var spans []Span
	var cur *Span
	var prev glyph
	var b strings.Builder
	flush := func() {
		if cur == nil {
			return
		}
		cur.Text = strings.TrimSpace(b.String())
		if cur.Text != "" {
			spans = append(spans, *cur)
		}
		cur = nil
		b.Reset()
	}
	for _, g := range glyphs {
		if g.r == '\n' || g.r == '\r' {
			flush()
			continue
		}
		if g.r == utf8.RuneError || unicode.IsControl(g.r) || unicode.IsSpace(g.r) {
			continue
		}
		// Cluster on the glyph bottom. A period sits lower than a capital, so the
		// tops of one line are not a stable baseline.
		if cur != nil && math.Abs(g.box.Y1-prev.box.Y1) > 4 {
			flush()
		}
		if cur == nil {
			box := g.box
			cur = &Span{Locator: locator.Locator{Kind: locator.KindPage, Page: page, BBox: &box}}
		} else if g.box.X0-prev.box.X1 > 3 {
			b.WriteByte(' ')
		}
		b.WriteRune(g.r)
		union(cur.Locator.BBox, g.box)
		prev = g
	}
	flush()
	return spans
}

func union(dst *locator.BBox, box locator.BBox) {
	if box.X0 < dst.X0 {
		dst.X0 = box.X0
	}
	if box.Y0 < dst.Y0 {
		dst.Y0 = box.Y0
	}
	if box.X1 > dst.X1 {
		dst.X1 = box.X1
	}
	if box.Y1 > dst.Y1 {
		dst.Y1 = box.Y1
	}
}

// pageBBox converts a PDF character box (origin bottom-left, y up) into the
// locator box (origin top-left, y down). A box that misses the page is dropped.
func pageBBox(pageW, pageH, left, bottom, right, top float64) (locator.BBox, bool) {
	if right < left {
		left, right = right, left
	}
	if top < bottom {
		bottom, top = top, bottom
	}
	x0, x1 := left, right
	y0, y1 := pageH-top, pageH-bottom
	if x1 < 0 || y1 < 0 || x0 > pageW || y0 > pageH {
		return locator.BBox{}, false
	}
	x0 = math.Max(0, x0)
	y0 = math.Max(0, y0)
	x1 = math.Min(pageW, x1)
	y1 = math.Min(pageH, y1)
	if x1 <= x0 || y1 <= y0 {
		return locator.BBox{}, false
	}
	return locator.BBox{X0: x0, Y0: y0, X1: x1, Y1: y1}, true
}

func inMargin(span Span, pageH float64) bool {
	if span.Locator.BBox == nil || pageH <= 0 {
		return false
	}
	box := span.Locator.BBox
	return box.Y0 < pageH*marginFraction || box.Y1 > pageH*(1-marginFraction)
}

// dropRunningMargins removes a line that sits in the top or bottom band and
// repeats on another page. A one-page file has nothing to compare, so its
// margins stay.
func dropRunningMargins(pages []Page, heights []float64) {
	if len(heights) != len(pages) {
		return
	}
	counts := map[string]int{}
	for i := 0; i < len(pages); i++ {
		height := heights[i]
		seen := map[string]bool{}
		for _, span := range pages[i].Spans {
			if !inMargin(span, height) || seen[span.Text] {
				continue
			}
			seen[span.Text] = true
			counts[span.Text]++
		}
	}
	for i := 0; i < len(pages); i++ {
		height := heights[i]
		kept := make([]Span, 0, len(pages[i].Spans))
		for _, span := range pages[i].Spans {
			if inMargin(span, height) && counts[span.Text] >= 2 {
				continue
			}
			kept = append(kept, span)
		}
		pages[i].Spans = kept
	}
}

func imageArea(box imageBox) float64 {
	w := box.right - box.left
	h := box.top - box.bottom
	if w <= 0 || h <= 0 {
		return 0
	}
	return w * h
}

// keptImages drops marks that are small or that sit in the same place on
// more than one page (logos and watermarks).
func keptImages(pages [][]imageBox, widths, heights []float64) [][]imageBox {
	type key struct{ left, bottom, right, top int }
	counts := map[key]int{}
	for _, page := range pages {
		seen := map[key]bool{}
		for _, box := range page {
			k := key{int(math.Round(box.left)), int(math.Round(box.bottom)), int(math.Round(box.right)), int(math.Round(box.top))}
			if seen[k] {
				continue
			}
			seen[k] = true
			counts[k]++
		}
	}
	out := make([][]imageBox, len(pages))
	for i, page := range pages {
		area := widths[i] * heights[i]
		for _, box := range page {
			k := key{int(math.Round(box.left)), int(math.Round(box.bottom)), int(math.Round(box.right)), int(math.Round(box.top))}
			if counts[k] >= 2 {
				continue
			}
			if area > 0 && imageArea(box)/area < smallImageFraction {
				continue
			}
			out[i] = append(out[i], box)
		}
	}
	return out
}

func isScanned(pageW, pageH float64, images []imageBox) bool {
	pageArea := pageW * pageH
	if pageArea <= 0 {
		return false
	}
	var covered float64
	for _, box := range images {
		covered += imageArea(box)
	}
	return covered/pageArea >= scannedFraction
}
