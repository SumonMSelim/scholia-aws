package attach

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

type fakeReader struct {
	text  string
	err   error
	calls int
}

func (f *fakeReader) Read(context.Context, string, []byte) (string, error) {
	f.calls++
	return f.text, f.err
}

var png = append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, make([]byte, 16)...)

func TestValidate(t *testing.T) {
	tests := []struct {
		name, file, ct string
		size           int64
		ok             bool
	}{
		{"pdf", "hw.pdf", "application/pdf", 1000, true},
		{"markdown", "notes.md", "text/markdown", 10, true},
		{"latex", "main.tex", "application/x-tex", 10, true},
		{"bibtex", "refs.bib", "application/x-bibtex", 10, true},
		{"image", "photo.jpg", "image/jpeg", 1000, true},
		{"audio is not for chats", "talk.mp3", "audio/mpeg", 1000, false},
		{"slides are not for chats", "deck.pptx", "application/vnd.openxmlformats-officedocument.presentationml.presentation", 1000, false},
		{"extension mismatch", "hw.txt", "application/pdf", 10, false},
		{"path in name", "../hw.pdf", "application/pdf", 10, false},
		{"empty", "hw.pdf", "application/pdf", 0, false},
		{"too large", "hw.pdf", "application/pdf", MaxBytes + 1, false},
		{"image over vision limit", "photo.png", "image/png", maxImageBytes + 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := Validate(tt.file, tt.ct, tt.size); (err == nil) != tt.ok {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestConvertText(t *testing.T) {
	tests := []struct {
		name    string
		file    File
		want    string
		wantErr string
	}{
		{"markdown", File{"a.md", "text/markdown", []byte("# Notes\n\nTCP is reliable.\n")}, "# Notes\n\nTCP is reliable.", ""},
		{"latex", File{"a.tex", "text/x-tex; charset=utf-8", []byte(`\section{Routing}`)}, `\section{Routing}`, ""},
		{"empty body", File{"a.md", "text/markdown", nil}, "", "empty"},
		{"blank text", File{"a.txt", "text/plain", []byte("   \n ")}, "", "no readable text"},
		{"binary posing as text", File{"a.txt", "text/plain", []byte{0xff, 0xfe, 0x00}}, "", "does not match"},
		{"pdf that is not one", File{"a.pdf", "application/pdf", []byte("hello")}, "", "does not match"},
		{"broken pdf", File{"a.pdf", "application/pdf", []byte("%PDF-1.7 broken")}, "", "could not read the PDF"},
		{"not allowed", File{"a.mp3", "audio/mpeg", []byte("ID3")}, "", "not allowed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Convert(context.Background(), tt.file, nil)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v", err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("got %q err %v", got, err)
			}
		})
	}
}

func TestConvertImage(t *testing.T) {
	img := File{"board.png", "image/png", png}
	if _, err := Convert(context.Background(), img, nil); err == nil {
		t.Fatal("an image was read without a model")
	}
	reader := &fakeReader{text: "$$x^2$$"}
	got, err := Convert(context.Background(), img, reader)
	if err != nil || got != "$$x^2$$" || reader.calls != 1 {
		t.Fatalf("got %q err %v", got, err)
	}
	if _, err := Convert(context.Background(), img, &fakeReader{err: errors.New("vision down")}); err == nil {
		t.Fatal("a vision failure was swallowed")
	}
	if _, err := Convert(context.Background(), img, &fakeReader{text: "  "}); err == nil {
		t.Fatal("an empty transcription was accepted")
	}
}

func TestConvertPDF(t *testing.T) {
	for _, name := range []string{"native.pdf", "scanned.pdf"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile("../pdfdoc/testdata/" + name)
			if err != nil {
				t.Fatal(err)
			}
			reader := &fakeReader{text: "scanned text"}
			got, err := Convert(context.Background(), File{name, "application/pdf", raw}, reader)
			if err != nil || !strings.Contains(got, "[page 1]") {
				t.Fatalf("got %q err %v", got, err)
			}
			if name == "scanned.pdf" && (reader.calls == 0 || reader.calls > maxScannedPages || !strings.Contains(got, "scanned text")) {
				t.Fatalf("scanned pages: calls=%d text=%q", reader.calls, got)
			}
		})
	}
}

func TestConvertCapsText(t *testing.T) {
	body := []byte(strings.Repeat("é", MaxTextBytes))
	got, err := Convert(context.Background(), File{"long.txt", "text/plain", body}, nil)
	if err != nil || len(got) > MaxTextBytes || !strings.HasPrefix(got, "é") || strings.ContainsRune(got, '�') {
		t.Fatalf("len %d err %v", len(got), err)
	}
}

func TestCheckSize(t *testing.T) {
	if err := Check(File{"big.txt", "text/plain", make([]byte, MaxBytes+1)}); err == nil {
		t.Fatal("an oversized body was accepted")
	}
}
