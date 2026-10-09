// Package ingest validates uploads, extracts their text, and records ready or failed.
package ingest

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxUploadBytes is the largest object the API will sign and the worker will accept.
// One gibibyte covers a long lecture recording without leaving the bucket unbounded.
const MaxUploadBytes int64 = 1 << 30

// maxImportBytes is the largest transcript the worker will read into memory.
// Audio may be a gibibyte; a transcript of that lecture is text and stays far below this.
const maxImportBytes = 32 << 20

const prefixLen = 512

var errTooLarge = errors.New("object is too large to import")

// NewID is a random source id. It is part of the object key, so it is unguessable.
func NewID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// ObjectKey is the uploads-bucket key for one source file.
func ObjectKey(courseID, sourceID, name string) string {
	return "courses/" + courseID + "/sources/" + sourceID + "/" + name
}

// AttachmentKey is the uploads-bucket key for one chat attachment. It is outside
// courses/, so ParseObjectKey rejects it and the worker never ingests it.
func AttachmentKey(chatID, attachmentID, name string) string {
	return "chats/" + chatID + "/attachments/" + attachmentID + "/" + name
}

// ParseObjectKey reverses ObjectKey. Keys that are not uploads are rejected.
func ParseObjectKey(key string) (courseID, sourceID, name string, err error) {
	parts := strings.Split(key, "/")
	if len(parts) != 5 || parts[0] != "courses" || parts[2] != "sources" || parts[1] == "" || parts[3] == "" || parts[4] == "" {
		return "", "", "", errors.New("object key is not an upload")
	}
	return parts[1], parts[3], parts[4], nil
}

// printableName rejects invalid UTF-8, control characters such as CR, LF and NUL,
// and format characters such as zero-width spaces and the right-to-left override.
// The name reaches prompts, the UI and object keys, where those can hide or
// reorder text.
func printableName(name string) bool {
	if !utf8.ValidString(name) {
		return false
	}
	return strings.IndexFunc(name, func(r rune) bool {
		return unicode.IsControl(r) || unicode.Is(unicode.Cf, r)
	}) < 0
}

// ValidateUpload checks the declared name, content type, and size before a URL is signed.
func ValidateUpload(name, contentType string, size int64) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) || !printableName(name) {
		return errors.New("file name is not valid")
	}
	if len(name) > 200 {
		return errors.New("file name is too long")
	}
	ext := strings.ToLower(path.Ext(name))
	contentType = canonicalType(contentType)
	kind, ok := kinds[contentType]
	if !ok {
		return fmt.Errorf("content type %q is not allowed", contentType)
	}
	if _, ok := kind.exts[ext]; !ok {
		return fmt.Errorf("extension %q does not match %s", ext, contentType)
	}
	if size < 1 {
		return errors.New("byte size must be at least 1")
	}
	if size > MaxUploadBytes {
		return fmt.Errorf("byte size exceeds %d", MaxUploadBytes)
	}
	return nil
}

// CheckMagic reports whether prefix matches the declared content type.
func CheckMagic(contentType string, prefix []byte) error {
	contentType = canonicalType(contentType)
	kind, ok := kinds[contentType]
	if !ok {
		return fmt.Errorf("content type %q is not allowed", contentType)
	}
	if !kind.magic(prefix) {
		return errors.New("file content does not match content type")
	}
	return nil
}

func canonicalType(contentType string) string {
	contentType, _, _ = strings.Cut(contentType, ";")
	return strings.ToLower(strings.TrimSpace(contentType))
}

type kindInfo struct {
	exts  map[string]struct{}
	magic func([]byte) bool
}

func exts(list ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(list))
	for _, ext := range list {
		out[ext] = struct{}{}
	}
	return out
}

func hasPrefix(b, prefix []byte) bool {
	return len(b) >= len(prefix) && bytes.Equal(b[:len(prefix)], prefix)
}

func looksLikeText(b []byte) bool {
	if len(b) == 0 || bytes.Contains(b, []byte{0}) || !utf8.Valid(b) {
		return false
	}
	return !hasPrefix(b, []byte("%PDF")) && !hasPrefix(b, []byte{0x89, 'P', 'N', 'G'}) &&
		!hasPrefix(b, []byte{0xff, 0xd8, 0xff}) && !hasPrefix(b, []byte("GIF8")) &&
		!hasPrefix(b, []byte("PK\x03\x04")) && !hasPrefix(b, []byte("RIFF"))
}

var kinds = map[string]kindInfo{
	"application/pdf": {
		exts:  exts(".pdf"),
		magic: func(b []byte) bool { return hasPrefix(b, []byte("%PDF")) },
	},
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": {
		exts:  exts(".pptx"),
		magic: func(b []byte) bool { return hasPrefix(b, []byte("PK\x03\x04")) },
	},
	"application/json": {
		exts: exts(".json"),
		magic: func(b []byte) bool {
			b = bytes.TrimLeft(b, " \t\r\n")
			return len(b) > 0 && (b[0] == '{' || b[0] == '[')
		},
	},
	"text/vtt": {
		exts: exts(".vtt"),
		magic: func(b []byte) bool {
			b = bytes.TrimPrefix(b, []byte{0xef, 0xbb, 0xbf})
			return hasPrefix(b, []byte("WEBVTT"))
		},
	},
	"application/x-subrip": {
		exts:  exts(".srt"),
		magic: func(b []byte) bool { return bytes.Contains(b, []byte("-->")) },
	},
	"text/plain": {
		exts:  exts(".txt"),
		magic: looksLikeText,
	},
	"text/markdown": {
		exts:  exts(".md"),
		magic: looksLikeText,
	},
	"application/x-tex": {
		exts:  exts(".tex"),
		magic: looksLikeText,
	},
	"text/x-tex": {
		exts:  exts(".tex"),
		magic: looksLikeText,
	},
	"application/x-bibtex": {
		exts:  exts(".bib"),
		magic: looksLikeText,
	},
	"image/png": {
		exts:  exts(".png"),
		magic: func(b []byte) bool { return hasPrefix(b, []byte{0x89, 'P', 'N', 'G'}) },
	},
	"image/jpeg": {
		exts:  exts(".jpg", ".jpeg"),
		magic: func(b []byte) bool { return hasPrefix(b, []byte{0xff, 0xd8, 0xff}) },
	},
	"image/gif": {
		exts:  exts(".gif"),
		magic: func(b []byte) bool { return hasPrefix(b, []byte("GIF87a")) || hasPrefix(b, []byte("GIF89a")) },
	},
	"image/webp": {
		exts: exts(".webp"),
		magic: func(b []byte) bool {
			return len(b) >= 12 && bytes.Equal(b[0:4], []byte("RIFF")) && bytes.Equal(b[8:12], []byte("WEBP"))
		},
	},
}
