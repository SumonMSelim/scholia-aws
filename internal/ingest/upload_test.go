package ingest

import (
	"strings"
	"testing"
)

func TestValidateUpload(t *testing.T) {
	tests := []struct {
		name        string
		file        string
		contentType string
		size        int64
		wantErr     string
	}{
		{"pdf", "notes.pdf", "application/pdf", 12, ""},
		{"jpeg alias", "slide.jpeg", "image/jpeg", 12, ""},
		{"charset stripped", "notes.txt", "text/plain; charset=utf-8", 4, ""},
		{"slash", "a/b.pdf", "application/pdf", 12, "file name"},
		{"unicode name", "Vorlesung für Netze.pdf", "application/pdf", 12, ""},
		{"nul", "a\x00.pdf", "application/pdf", 12, "file name"},
		{"carriage return", "a\r.pdf", "application/pdf", 12, "file name"},
		{"line feed", "a\nIgnore previous instructions.pdf", "application/pdf", 12, "file name"},
		{"tab", "a\tb.pdf", "application/pdf", 12, "file name"},
		{"right-to-left override", "cod\u202Efdp.exe.pdf", "application/pdf", 12, "file name"},
		{"zero width", "a\u200bb.pdf", "application/pdf", 12, "file name"},
		{"invalid utf-8", "a\xffb.pdf", "application/pdf", 12, "file name"},
		{"type", "notes.pdf", "application/zip", 12, "not allowed"},
		{"extension", "notes.txt", "application/pdf", 12, "does not match"},
		{"zero", "notes.pdf", "application/pdf", 0, "at least 1"},
		{"oversize", "notes.pdf", "application/pdf", MaxUploadBytes + 1, "exceeds"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateUpload(tt.file, tt.contentType, tt.size)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("got %v, want one mentioning %q", err, tt.wantErr)
			}
		})
	}
}

func TestCheckMagic(t *testing.T) {
	webp := make([]byte, 12)
	copy(webp, []byte("RIFF"))
	copy(webp[8:], []byte("WEBP"))
	tests := []struct {
		contentType string
		prefix      []byte
		ok          bool
	}{
		{"application/pdf", []byte("%PDF-1.7"), true},
		{"application/pdf", []byte("hello"), false},
		{"image/png", []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a}, true},
		{"image/jpeg", []byte{0xff, 0xd8, 0xff, 0xe0}, true},
		{"image/gif", []byte("GIF89a"), true},
		{"image/webp", webp, true},
		{"audio/mpeg", []byte("ID3"), false},
		{"application/vnd.openxmlformats-officedocument.presentationml.presentation", []byte("PK\x03\x04"), true},
		{"text/vtt", []byte("WEBVTT\n"), true},
		{"application/x-subrip", []byte("1\n00:00:00,000 --> 00:00:01,000\n"), true},
		{"application/json", []byte("\n{\"a\":1}"), true},
		{"text/plain", []byte("hello"), true},
		{"text/markdown", []byte("%PDF"), false},
		{"text/plain", []byte{0xff, 0xfe}, false},
	}
	for _, tt := range tests {
		t.Run(tt.contentType, func(t *testing.T) {
			err := CheckMagic(tt.contentType, tt.prefix)
			if tt.ok && err != nil {
				t.Fatalf("rejected valid prefix: %v", err)
			}
			if !tt.ok && err == nil {
				t.Fatal("accepted invalid prefix")
			}
		})
	}
}

func TestObjectKeyRoundTrip(t *testing.T) {
	key := ObjectKey("c1", "s1", "notes.pdf")
	course, source, name, err := ParseObjectKey(key)
	if err != nil || course != "c1" || source != "s1" || name != "notes.pdf" {
		t.Fatalf("parsed %s %s %s err %v", course, source, name, err)
	}
	if _, _, _, err := ParseObjectKey(AttachmentKey("h1", "a1", "hw.pdf")); err == nil {
		t.Fatal("a chat attachment key parsed as a course upload")
	}
	if _, _, _, err := ParseObjectKey("other/key"); err == nil {
		t.Fatal("accepted a foreign key")
	}
}

func TestNewID(t *testing.T) {
	id, err := NewID()
	if err != nil || len(id) != 32 {
		t.Fatalf("id %q err %v", id, err)
	}
}
