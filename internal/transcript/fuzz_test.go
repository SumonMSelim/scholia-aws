package transcript

import (
	"os"
	"testing"
)

func FuzzVTT(f *testing.F) {
	addCorpus(f, "testdata/lecture.vtt", "testdata/bad.vtt")
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		_, _ = Import("vtt", data)
	})
}

func FuzzSRT(f *testing.F) {
	addCorpus(f, "testdata/lecture.srt", "testdata/bad.srt")
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		_, _ = Import("srt", data)
	})
}

func FuzzTranscribeJSON(f *testing.F) {
	addCorpus(f, "testdata/lecture.json", "testdata/bad.json")
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		_, _ = Import("json", data)
	})
}

// addCorpus seeds the fuzzer with the committed golden files.
// The empty input keeps the first mutation from being only a well-formed lecture.
func addCorpus(f *testing.F, names ...string) {
	f.Helper()
	f.Add([]byte(nil))
	for _, name := range names {
		raw, err := os.ReadFile(name)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(raw)
	}
}
