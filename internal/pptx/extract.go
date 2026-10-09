package pptx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/sumonmselim/scholia-aws/internal/locator"
)

const (
	maxFiles     = 4096
	maxPartBytes = 32 << 20

	relNS = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
)

// Extract reads slide text and notes in presentation order, plus the images
// embedded on each slide. It does not describe those images.
func Extract(raw []byte) (Document, error) {
	if len(raw) == 0 {
		return Document{}, errors.New("pptx is empty")
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return Document{}, fmt.Errorf("pptx is not a zip: %w", err)
	}
	if len(zr.File) > maxFiles {
		return Document{}, errors.New("pptx has too many parts")
	}
	files := make(map[string]*zip.File, len(zr.File))
	for _, f := range zr.File {
		name := path.Clean(f.Name)
		if name == "." || strings.HasPrefix(name, "../") || strings.Contains(name, "/../") {
			return Document{}, errors.New("pptx part escapes the package")
		}
		files[name] = f
	}

	order, err := slideOrder(files)
	if err != nil {
		return Document{}, err
	}
	doc := Document{Slides: make([]Slide, len(order))}
	for i, part := range order {
		slide, err := readSlide(files, i+1, part)
		if err != nil {
			return Document{}, fmt.Errorf("slide %d: %w", i+1, err)
		}
		doc.Slides[i] = slide
	}
	if err := doc.Validate(); err != nil {
		return Document{}, err
	}
	return doc, nil
}

func slideOrder(files map[string]*zip.File) ([]string, error) {
	rels, err := relationships(files, relsPath("ppt/presentation.xml"))
	if err != nil {
		return nil, err
	}
	byID := map[string]string{}
	for _, rel := range rels {
		if rel.kind != "slide" || rel.external {
			continue
		}
		byID[rel.id] = rel.target
	}
	ids, err := slideIDs(files)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, errors.New("pptx has no slides")
	}
	order := make([]string, 0, len(ids))
	for _, id := range ids {
		target, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("pptx slide relationship %s is missing", id)
		}
		order = append(order, target)
	}
	return order, nil
}

func slideIDs(files map[string]*zip.File) ([]string, error) {
	raw, err := readPart(files, "ppt/presentation.xml")
	if err != nil {
		return nil, err
	}
	dec := xml.NewDecoder(bytes.NewReader(raw))
	var ids []string
	inList := false
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return ids, nil
		}
		if err != nil {
			return nil, fmt.Errorf("pptx presentation: %w", err)
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "sldIdLst":
			inList = true
		case "sldId":
			if !inList {
				continue
			}
			for _, attr := range start.Attr {
				if attr.Name.Local == "id" && attr.Name.Space == relNS {
					ids = append(ids, attr.Value)
				}
			}
		}
	}
}

type relationship struct {
	id, target, kind string
	external         bool
}

func relationships(files map[string]*zip.File, name string) ([]relationship, error) {
	raw, err := readPart(files, name)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Rels []struct {
			ID     string `xml:"Id,attr"`
			Type   string `xml:"Type,attr"`
			Target string `xml:"Target,attr"`
			Mode   string `xml:"TargetMode,attr"`
		} `xml:"Relationship"`
	}
	if err := xml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("pptx relationships: %w", err)
	}
	base := strings.TrimSuffix(strings.TrimSuffix(name, "/"+path.Base(name)), "/_rels")
	out := make([]relationship, 0, len(doc.Rels))
	for _, rel := range doc.Rels {
		target, err := resolve(base, rel.Target)
		if err != nil {
			return nil, err
		}
		out = append(out, relationship{
			id: rel.ID, target: target, kind: relKind(rel.Type), external: rel.Mode == "External",
		})
	}
	return out, nil
}

func relKind(typ string) string {
	switch {
	case strings.HasSuffix(typ, "/slide"):
		return "slide"
	case strings.HasSuffix(typ, "/notesSlide"):
		return "notes"
	case strings.HasSuffix(typ, "/image"):
		return "image"
	default:
		return ""
	}
}

func relsPath(part string) string {
	return path.Join(path.Dir(part), "_rels", path.Base(part)+".rels")
}

func resolve(base, target string) (string, error) {
	if target == "" {
		return "", errors.New("pptx relationship has no target")
	}
	if strings.Contains(target, "\\") {
		return "", errors.New("pptx relationship target is not a package path")
	}
	joined := target
	if !strings.HasPrefix(target, "/") {
		joined = path.Join(base, target)
	}
	joined = path.Clean(strings.TrimPrefix(joined, "/"))
	if joined == ".." || strings.HasPrefix(joined, "../") {
		return "", errors.New("pptx relationship escapes the package")
	}
	return joined, nil
}

func readSlide(files map[string]*zip.File, number int, part string) (Slide, error) {
	raw, err := readPart(files, part)
	if err != nil {
		return Slide{}, err
	}
	lines, err := paragraphTexts(raw)
	if err != nil {
		return Slide{}, err
	}
	slide := Slide{Number: number, Spans: spans(number, lines, false)}
	rels, err := relationships(files, relsPath(part))
	if err != nil {
		if errors.Is(err, errMissing) {
			return slide, nil
		}
		return Slide{}, err
	}
	names := map[string]int{}
	for _, rel := range rels {
		if rel.external {
			continue
		}
		switch rel.kind {
		case "notes":
			noteXML, err := readPart(files, rel.target)
			if err != nil {
				return Slide{}, err
			}
			paras, err := paragraphTexts(noteXML)
			if err != nil {
				return Slide{}, err
			}
			slide.Spans = append(slide.Spans, spans(number, paras, true)...)
		case "image":
			body, err := readPart(files, rel.target)
			if err != nil {
				return Slide{}, err
			}
			name := imageName(path.Base(rel.target), names)
			slide.Images = append(slide.Images, Image{
				Name: name, ContentType: imageType(name), Bytes: body,
			})
		}
	}
	return slide, nil
}

func spans(number int, paragraphs []string, notes bool) []Span {
	out := make([]Span, 0, len(paragraphs))
	for _, text := range paragraphs {
		out = append(out, Span{
			Text: text, Notes: notes,
			Locator: locator.Locator{Kind: locator.KindSlide, Slide: number},
		})
	}
	return out
}

func paragraphTexts(raw []byte) ([]string, error) {
	dec := xml.NewDecoder(bytes.NewReader(raw))
	var out []string
	var cur strings.Builder
	depth := 0
	inText := false
	var text strings.Builder
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		switch el := tok.(type) {
		case xml.StartElement:
			switch el.Name.Local {
			case "p":
				if depth == 0 {
					cur.Reset()
				}
				depth++
			case "t":
				inText = true
				text.Reset()
			}
		case xml.EndElement:
			switch el.Name.Local {
			case "t":
				cur.WriteString(text.String())
				inText = false
			case "p":
				if depth == 0 {
					break
				}
				depth--
				if depth == 0 {
					if paragraph := strings.TrimSpace(cur.String()); paragraph != "" {
						out = append(out, paragraph)
					}
					cur.Reset()
				}
			}
		case xml.CharData:
			if inText {
				text.Write(el)
			}
		}
	}
}

func imageName(base string, seen map[string]int) string {
	clean := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, base)
	if clean == "" || clean == "." || clean == ".." {
		clean = "image"
	}
	seen[clean]++
	if seen[clean] == 1 {
		return clean
	}
	ext := path.Ext(clean)
	stem := strings.TrimSuffix(clean, ext)
	return fmt.Sprintf("%s-%d%s", stem, seen[clean], ext)
}

func imageType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".emf":
		return "image/emf"
	case ".wmf":
		return "image/wmf"
	case ".tif", ".tiff":
		return "image/tiff"
	default:
		return "application/octet-stream"
	}
}

var errMissing = errors.New("pptx part is missing")

func readPart(files map[string]*zip.File, name string) ([]byte, error) {
	f, ok := files[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", errMissing, name)
	}
	if f.UncompressedSize64 > maxPartBytes {
		return nil, fmt.Errorf("pptx part %s is too large", name)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	body, err := io.ReadAll(io.LimitReader(rc, maxPartBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxPartBytes {
		return nil, fmt.Errorf("pptx part %s is too large", name)
	}
	return body, nil
}
