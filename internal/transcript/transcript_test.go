package transcript

import (
	"strings"
	"testing"

	"github.com/sumonmselim/scholia-aws/internal/locator"
)

func TestMergeSentences(t *testing.T) {
	sentences, err := Merge([]Token{
		{Text: "Hello", Speaker: "spk_0", StartMS: 0, EndMS: 300},
		{Text: "world", Speaker: "spk_0", StartMS: 400, EndMS: 900},
		{Text: ".", Punct: true},
		{Text: "Next", Speaker: "spk_0", StartMS: 1200, EndMS: 1500},
		{Text: "line", Speaker: "spk_0", StartMS: 1600, EndMS: 2000},
		{Text: ".", Punct: true},
		{Text: "Other", Speaker: "spk_1", StartMS: 2100, EndMS: 2500},
		{Text: "voice", Speaker: "spk_1", StartMS: 2600, EndMS: 3000},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []Sentence{
		{Text: "Hello world.", Speaker: "spk_0", Locator: locator.Locator{Kind: locator.KindTime, StartMS: 0, EndMS: 900}},
		{Text: "Next line.", Speaker: "spk_0", Locator: locator.Locator{Kind: locator.KindTime, StartMS: 1200, EndMS: 2000}},
		{Text: "Other voice", Speaker: "spk_1", Locator: locator.Locator{Kind: locator.KindTime, StartMS: 2100, EndMS: 3000}},
	}
	if len(sentences) != len(want) {
		t.Fatalf("got %#v", sentences)
	}
	for i := range want {
		if sentences[i] != want[i] {
			t.Fatalf("sentence %d = %#v, want %#v", i, sentences[i], want[i])
		}
	}
}

func TestMergeRejects(t *testing.T) {
	if _, err := Merge(nil); err == nil {
		t.Fatal("empty token list was accepted")
	}
	if _, err := Merge([]Token{{Text: "nope", StartMS: 5, EndMS: 1}}); err == nil {
		t.Fatal("backwards word was accepted")
	}
}

func TestSentencesFromTranscribe(t *testing.T) {
	raw := []byte(`{
	  "results": {
	    "items": [
	      {"start_time": "0.0", "end_time": "0.3", "type": "pronunciation", "alternatives": [{"content": "Hello"}]},
	      {"start_time": "0.4", "end_time": "0.9", "type": "pronunciation", "alternatives": [{"content": "world"}]},
	      {"type": "punctuation", "alternatives": [{"content": "."}]},
	      {"start_time": "1.25", "end_time": "2.0", "type": "pronunciation", "alternatives": [{"content": "Next"}]}
	    ],
	    "speaker_labels": {
	      "segments": [{
	        "items": [
	          {"start_time": "0.0", "speaker_label": "spk_0"},
	          {"start_time": "0.4", "speaker_label": "spk_0"},
	          {"start_time": "1.25", "speaker_label": "spk_0"}
	        ]
	      }]
	    }
	  }
	}`)
	sentences, err := Sentences(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(sentences) != 2 {
		t.Fatalf("got %#v", sentences)
	}
	if sentences[0].Text != "Hello world." || sentences[0].Speaker != "spk_0" ||
		sentences[0].Locator.StartMS != 0 || sentences[0].Locator.EndMS != 900 {
		t.Fatalf("first = %#v", sentences[0])
	}
	if sentences[1].Text != "Next" || sentences[1].Locator.StartMS != 1250 || sentences[1].Locator.EndMS != 2000 {
		t.Fatalf("second = %#v", sentences[1])
	}

	encoded, err := Encode(sentences)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"start_ms":0`) {
		t.Fatalf("zero start was dropped: %s", encoded)
	}
	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded[0] != sentences[0] || decoded[1] != sentences[1] {
		t.Fatalf("round trip %#v", decoded)
	}
}

func TestSentencesFromSegments(t *testing.T) {
	raw := []byte(`{
	  "results": {
	    "audio_segments": [
	      {"transcript": "Opening remark.", "start_time": "0.5", "end_time": "1.5", "speaker_label": "spk_1"}
	    ]
	  }
	}`)
	sentences, err := Sentences(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(sentences) != 1 || sentences[0].Text != "Opening remark." || sentences[0].Speaker != "spk_1" ||
		sentences[0].Locator.StartMS != 500 || sentences[0].Locator.EndMS != 1500 {
		t.Fatalf("got %#v", sentences)
	}
}

func TestSentencesRejectsBadInput(t *testing.T) {
	if _, err := Sentences([]byte(`{`)); err == nil {
		t.Fatal("broken json was accepted")
	}
	if _, err := Sentences([]byte(`{"results":{}}`)); err == nil {
		t.Fatal("empty result was accepted")
	}
	if _, err := Sentences([]byte(`{"results":{"items":[{"type":"pronunciation","alternatives":[{"content":"Hi"}]}]}}`)); err == nil {
		t.Fatal("word without a time was accepted")
	}
}

func TestEncodeRejects(t *testing.T) {
	if _, err := Encode(nil); err == nil {
		t.Fatal("empty transcript was encoded")
	}
	bad := Sentence{Text: "x", Locator: locator.Locator{Kind: locator.KindSlide, Slide: 1}}
	if _, err := Encode([]Sentence{bad}); err == nil {
		t.Fatal("slide locator was encoded as a sentence")
	}
	if _, err := Decode([]byte(`{"sentences":[]}`)); err == nil {
		t.Fatal("empty document was decoded")
	}
}

func TestObjectKey(t *testing.T) {
	key := ObjectKey("c1", "s1")
	if key != "courses/c1/sources/s1/derived/transcript.json" {
		t.Fatalf("key %s", key)
	}
}
