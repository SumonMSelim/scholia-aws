package transcript

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/sumonmselim/scholia-aws/internal/locator"
)

// Import parses a lecture transcript into timed sentences. kind is vtt, srt,
// or json (an Amazon Transcribe result).
func Import(kind string, raw []byte) ([]Sentence, error) {
	switch kind {
	case "vtt":
		return ParseVTT(raw)
	case "srt":
		return ParseSRT(raw)
	case "json":
		return Sentences(raw)
	default:
		return nil, fmt.Errorf("transcript format %q is not supported", kind)
	}
}

// ParseVTT reads a WebVTT file. Voice tags become the speaker. NOTE, STYLE,
// and REGION blocks are ignored.
func ParseVTT(raw []byte) ([]Sentence, error) {
	parts := blocks(normalize(raw))
	if len(parts) == 0 || !strings.HasPrefix(strings.TrimSpace(parts[0]), "WEBVTT") {
		return nil, errors.New("webvtt: missing WEBVTT header")
	}
	var cues []timedCue
	for _, block := range parts[1:] {
		switch firstLine(block) {
		case "NOTE", "STYLE", "REGION":
			continue
		}
		if strings.HasPrefix(firstLine(block), "NOTE ") {
			continue
		}
		cue, ok, err := parseCue(block)
		if err != nil {
			return nil, fmt.Errorf("webvtt: %w", err)
		}
		if ok {
			cues = append(cues, cue)
		}
	}
	return sentencesFromCues(cues)
}

// ParseSRT reads a SubRip file. SRT has no speaker field.
func ParseSRT(raw []byte) ([]Sentence, error) {
	text := normalize(raw)
	if !strings.Contains(text, "-->") {
		return nil, errors.New("srt: missing cue timing")
	}
	var cues []timedCue
	for _, block := range blocks(text) {
		cue, ok, err := parseCue(block)
		if err != nil {
			return nil, fmt.Errorf("srt: %w", err)
		}
		if ok {
			cues = append(cues, cue)
		}
	}
	return sentencesFromCues(cues)
}

type timedCue struct {
	speaker string
	text    string
	start   int64
	end     int64
}

func normalize(raw []byte) string {
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	return strings.ReplaceAll(text, "\r", "\n")
}

func blocks(text string) []string {
	var out []string
	for _, part := range strings.Split(text, "\n\n") {
		part = strings.Trim(part, "\n")
		if strings.TrimSpace(part) != "" {
			out = append(out, part)
		}
	}
	return out
}

func firstLine(block string) string {
	line, _, _ := strings.Cut(block, "\n")
	return strings.TrimSpace(line)
}

func parseCue(block string) (timedCue, bool, error) {
	lines := strings.Split(block, "\n")
	timing := -1
	for i, line := range lines {
		if strings.Contains(line, "-->") {
			timing = i
			break
		}
	}
	if timing < 0 {
		return timedCue{}, false, errors.New("cue has no timing")
	}
	start, end, err := parseTiming(lines[timing])
	if err != nil {
		return timedCue{}, false, err
	}
	payload := strings.TrimSpace(strings.Join(lines[timing+1:], "\n"))
	if payload == "" {
		return timedCue{}, false, nil
	}
	speaker, text := voiceAndText(payload)
	if text == "" {
		return timedCue{}, false, nil
	}
	return timedCue{speaker: speaker, text: text, start: start, end: end}, true, nil
}

func parseTiming(line string) (int64, int64, error) {
	left, right, ok := strings.Cut(line, "-->")
	if !ok {
		return 0, 0, errors.New("cue has no timing")
	}
	startRaw, err := firstField(left)
	if err != nil {
		return 0, 0, err
	}
	endRaw, err := firstField(right)
	if err != nil {
		return 0, 0, err
	}
	start, err := clockMS(startRaw)
	if err != nil {
		return 0, 0, err
	}
	end, err := clockMS(endRaw)
	if err != nil {
		return 0, 0, err
	}
	if end < start {
		return 0, 0, errors.New("cue end is before its start")
	}
	return start, end, nil
}

func firstField(value string) (string, error) {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return "", errors.New("cue time is missing")
	}
	return fields[0], nil
}

func clockMS(raw string) (int64, error) {
	raw = strings.ReplaceAll(strings.TrimSpace(raw), ",", ".")
	parts := strings.Split(raw, ":")
	var hours, minutes int64
	var seconds float64
	var err error
	switch len(parts) {
	case 3:
		hours, err = strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("cue time %q", raw)
		}
		minutes, seconds, err = minuteSecond(parts[1], parts[2])
	case 2:
		minutes, seconds, err = minuteSecond(parts[0], parts[1])
	default:
		err = fmt.Errorf("cue time %q", raw)
	}
	if err != nil {
		return 0, err
	}
	if hours < 0 || minutes < 0 || seconds < 0 {
		return 0, fmt.Errorf("cue time %q", raw)
	}
	return hours*3_600_000 + minutes*60_000 + int64(math.Round(seconds*1000)), nil
}

func minuteSecond(minute, second string) (int64, float64, error) {
	minutes, err := strconv.ParseInt(minute, 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("cue time %q", minute)
	}
	seconds, err := strconv.ParseFloat(second, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("cue time %q", second)
	}
	return minutes, seconds, nil
}

var (
	voiceTag = regexp.MustCompile(`(?i)<v(?:\.[^ >\t]+)?(?:[ \t]+([^>]+))?>`)
	anyTag   = regexp.MustCompile(`<[^>\n]+>`)
)

func voiceAndText(payload string) (string, string) {
	speaker := ""
	if match := voiceTag.FindStringSubmatch(payload); len(match) == 2 {
		speaker = strings.TrimSpace(match[1])
	}
	text := anyTag.ReplaceAllString(payload, "")
	return speaker, strings.Join(strings.Fields(text), " ")
}

func sentencesFromCues(cues []timedCue) ([]Sentence, error) {
	out := make([]Sentence, 0, len(cues))
	for _, cue := range cues {
		out = append(out, Sentence{
			Text:    cue.text,
			Speaker: cue.speaker,
			Locator: locator.Locator{Kind: locator.KindTime, StartMS: cue.start, EndMS: cue.end},
		})
	}
	if len(out) == 0 {
		return nil, errors.New("transcript has no sentences")
	}
	for i, sentence := range out {
		if err := sentence.Validate(); err != nil {
			return nil, fmt.Errorf("sentence %d: %w", i, err)
		}
	}
	return out, nil
}
