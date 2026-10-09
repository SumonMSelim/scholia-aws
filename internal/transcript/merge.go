package transcript

import (
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/sumonmselim/scholia-aws/internal/locator"
)

// Token is one word or one punctuation mark from a recognizer.
// Punct is true for marks that attach to the previous word without a space.
// Break ends a sentence after this token, which is how a segment with no
// word-level punctuation still becomes its own line.
type Token struct {
	Text    string
	Speaker string
	StartMS int64
	EndMS   int64
	Punct   bool
	Break   bool
}

// Merge joins words into sentences. A sentence ends on . ! ? or when the
// speaker changes. Times cover the first word through the last word.
func Merge(tokens []Token) ([]Sentence, error) {
	var out []Sentence
	var cur *Sentence
	var b strings.Builder

	flush := func() {
		if cur == nil {
			return
		}
		cur.Text = b.String()
		if cur.Text != "" {
			out = append(out, *cur)
		}
		cur = nil
		b.Reset()
	}

	for _, tok := range tokens {
		text := strings.TrimSpace(tok.Text)
		if text == "" {
			continue
		}
		if tok.Punct {
			if cur == nil {
				continue
			}
			b.WriteString(text)
			if sentenceEnd(text) {
				flush()
			}
			continue
		}
		if tok.EndMS < tok.StartMS || tok.StartMS < 0 {
			return nil, errors.New("word end is before its start")
		}
		if cur != nil && tok.Speaker != "" && cur.Speaker != "" && tok.Speaker != cur.Speaker {
			flush()
		}
		if cur == nil {
			cur = &Sentence{Locator: locator.Locator{
				Kind: locator.KindTime, StartMS: tok.StartMS, EndMS: tok.EndMS,
			}}
		}
		if cur.Speaker == "" {
			cur.Speaker = tok.Speaker
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(text)
		if tok.StartMS < cur.Locator.StartMS {
			cur.Locator.StartMS = tok.StartMS
		}
		if tok.EndMS > cur.Locator.EndMS {
			cur.Locator.EndMS = tok.EndMS
		}
		if tok.Break {
			flush()
		}
	}
	flush()
	if len(out) == 0 {
		return nil, errors.New("transcript has no sentences")
	}
	return out, nil
}

func sentenceEnd(text string) bool {
	if text == "..." {
		return false
	}
	r, _ := utf8.DecodeLastRuneInString(text)
	switch r {
	case '.', '!', '?', '。', '！', '？':
		return true
	default:
		return false
	}
}
