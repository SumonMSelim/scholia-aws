package transcript

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Sentences reads an Amazon Transcribe result. Word items are merged into
// sentences. When the result has no words, each audio segment becomes one line
// using that segment's start and end.
func Sentences(raw []byte) ([]Sentence, error) {
	var doc struct {
		Results struct {
			Items         []item `json:"items"`
			SpeakerLabels struct {
				Segments []struct {
					Items []struct {
						StartTime    string `json:"start_time"`
						SpeakerLabel string `json:"speaker_label"`
					} `json:"items"`
				} `json:"segments"`
			} `json:"speaker_labels"`
			AudioSegments []struct {
				Transcript   string `json:"transcript"`
				StartTime    string `json:"start_time"`
				EndTime      string `json:"end_time"`
				SpeakerLabel string `json:"speaker_label"`
			} `json:"audio_segments"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("transcribe result: %w", err)
	}

	speakers := map[string]string{}
	for _, seg := range doc.Results.SpeakerLabels.Segments {
		for _, it := range seg.Items {
			if it.StartTime != "" && it.SpeakerLabel != "" {
				speakers[it.StartTime] = it.SpeakerLabel
			}
		}
	}

	if len(doc.Results.Items) > 0 {
		tokens := make([]Token, 0, len(doc.Results.Items))
		for _, it := range doc.Results.Items {
			tok, ok, err := it.token(speakers)
			if err != nil {
				return nil, err
			}
			if ok {
				tokens = append(tokens, tok)
			}
		}
		return Merge(tokens)
	}

	tokens := make([]Token, 0, len(doc.Results.AudioSegments))
	for _, seg := range doc.Results.AudioSegments {
		start, err := secondsToMS(seg.StartTime)
		if err != nil {
			return nil, fmt.Errorf("segment start: %w", err)
		}
		end, err := secondsToMS(seg.EndTime)
		if err != nil {
			return nil, fmt.Errorf("segment end: %w", err)
		}
		tokens = append(tokens, Token{
			Text: seg.Transcript, Speaker: seg.SpeakerLabel,
			StartMS: start, EndMS: end, Break: true,
		})
	}
	return Merge(tokens)
}

type item struct {
	StartTime    string `json:"start_time"`
	EndTime      string `json:"end_time"`
	Type         string `json:"type"`
	Alternatives []struct {
		Content string `json:"content"`
	} `json:"alternatives"`
}

func (it item) token(speakers map[string]string) (Token, bool, error) {
	if len(it.Alternatives) == 0 || strings.TrimSpace(it.Alternatives[0].Content) == "" {
		return Token{}, false, nil
	}
	text := it.Alternatives[0].Content
	if it.Type == "punctuation" {
		return Token{Text: text, Punct: true}, true, nil
	}
	start, err := secondsToMS(it.StartTime)
	if err != nil {
		return Token{}, false, fmt.Errorf("word %q start: %w", text, err)
	}
	end, err := secondsToMS(it.EndTime)
	if err != nil {
		return Token{}, false, fmt.Errorf("word %q end: %w", text, err)
	}
	return Token{
		Text: text, Speaker: speakers[it.StartTime], StartMS: start, EndMS: end,
	}, true, nil
}

func secondsToMS(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	secs, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("time %q: %w", raw, err)
	}
	if secs < 0 || math.IsNaN(secs) || math.IsInf(secs, 0) {
		return 0, fmt.Errorf("time %q is not a duration", raw)
	}
	return int64(math.Round(secs * 1000)), nil
}
