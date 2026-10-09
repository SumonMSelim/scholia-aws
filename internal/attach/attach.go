// Package attach checks and reads files a student sends into one chat.
// A chat attachment is read into that chat's prompt at send time and is never
// indexed into course knowledge. The text is untrusted: callers fence it.
package attach

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/sumonmselim/scholia-aws/internal/ingest"
	"github.com/sumonmselim/scholia-aws/internal/pdfdoc"
)

const (
	// MaxBytes is the largest attachment a chat accepts.
	MaxBytes = 10 << 20
	// maxImageBytes matches what the vision models accept in one request.
	maxImageBytes = 4 << 20
	// MaxTextBytes caps the text one attachment adds to a prompt.
	MaxTextBytes = 128 << 10
	// MaxPerMessage is how many attachments one message may send.
	MaxPerMessage = 5
	// maxScannedPages bounds vision calls for a scanned PDF.
	maxScannedPages = 3
	// maxPDFPages bounds the pages read from one attached PDF inside the API request.
	maxPDFPages = 300
)

// allowed maps each accepted content type to whether it is an image.
var allowed = map[string]bool{
	"application/pdf":      false,
	"text/markdown":        false,
	"text/plain":           false,
	"application/x-tex":    false,
	"text/x-tex":           false,
	"application/x-bibtex": false,
	"image/png":            true,
	"image/jpeg":           true,
	"image/webp":           true,
}

// Reader transcribes an image. model.Provider satisfies it.
type Reader interface {
	Read(ctx context.Context, contentType string, image []byte) (string, error)
}

// File is one attachment read from the uploads bucket.
type File struct {
	Name        string
	ContentType string
	Body        []byte
}

// Validate checks the declared name, type and size before an upload URL is signed.
func Validate(name, contentType string, size int64) error {
	contentType = canonical(contentType)
	image, ok := allowed[contentType]
	if !ok {
		return fmt.Errorf("content type %q is not allowed in a chat", contentType)
	}
	if err := ingest.ValidateUpload(name, contentType, size); err != nil {
		return err
	}
	if size > MaxBytes || (image && size > maxImageBytes) {
		return errors.New("attachment is too large")
	}
	return nil
}

// Check verifies the stored bytes against the declared type.
func Check(f File) error {
	if len(f.Body) == 0 {
		return errors.New("attachment is empty")
	}
	if len(f.Body) > MaxBytes {
		return errors.New("attachment is too large")
	}
	if _, ok := allowed[canonical(f.ContentType)]; !ok {
		return fmt.Errorf("content type %q is not allowed in a chat", f.ContentType)
	}
	prefix := f.Body
	if len(prefix) > 512 {
		prefix = prefix[:512]
	}
	return ingest.CheckMagic(f.ContentType, prefix)
}

// Convert returns the attachment as text, at most MaxTextBytes.
// PDFs keep their visible text layer; scanned pages and images go through reader.
func Convert(ctx context.Context, f File, reader Reader) (string, error) {
	if err := Check(f); err != nil {
		return "", err
	}
	contentType := canonical(f.ContentType)
	var text string
	switch {
	case contentType == "application/pdf":
		out, err := pdfText(ctx, f.Body, reader)
		if err != nil {
			return "", err
		}
		text = out
	case allowed[contentType]:
		if reader == nil {
			return "", errors.New("no model can read images")
		}
		out, err := reader.Read(ctx, contentType, f.Body)
		if err != nil {
			return "", errors.New("could not read the image")
		}
		text = out
	default:
		if !utf8.Valid(f.Body) {
			return "", errors.New("text is not valid UTF-8")
		}
		text = string(f.Body)
	}
	text = strings.TrimSpace(strings.ReplaceAll(text, "\x00", ""))
	if text == "" {
		return "", errors.New("attachment has no readable text")
	}
	return clip(text, MaxTextBytes), nil
}

func pdfText(ctx context.Context, raw []byte, reader Reader) (string, error) {
	// Only pages a vision call will read are rendered: rendering holds each page in memory.
	lim := pdfdoc.Limits{MaxPages: maxPDFPages, MaxPNGBytes: maxScannedPages * maxImageBytes}
	if reader != nil {
		lim.MaxRendered = maxScannedPages
	}
	doc, err := pdfdoc.ExtractWithLimits(ctx, raw, lim)
	if err != nil {
		return "", errors.New("could not read the PDF")
	}
	var b strings.Builder
	scanned := 0
	for _, page := range doc.Pages {
		var body string
		switch {
		case page.Kind == pdfdoc.KindScanned && reader != nil && scanned < maxScannedPages:
			scanned++
			if out, err := reader.Read(ctx, "image/png", page.PNG); err == nil {
				body = out
			}
		default:
			lines := make([]string, 0, len(page.Spans))
			for _, span := range page.Spans {
				lines = append(lines, span.Text)
			}
			body = strings.Join(lines, "\n")
		}
		if strings.TrimSpace(body) == "" {
			continue
		}
		fmt.Fprintf(&b, "[page %d]\n%s\n\n", page.Number, body)
		if b.Len() > MaxTextBytes {
			break
		}
	}
	return b.String(), nil
}

func canonical(contentType string) string {
	contentType, _, _ = strings.Cut(contentType, ";")
	return strings.ToLower(strings.TrimSpace(contentType))
}

// clip cuts s to at most n bytes without splitting a rune.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return string(bytes.TrimSpace([]byte(s[:cut])))
}
