package pptx

import (
	"os"
	"testing"
)

func FuzzPPTX(f *testing.F) {
	f.Add([]byte("PK\x03\x04"))
	raw, err := os.ReadFile("testdata/lecture.pptx")
	if err != nil {
		f.Fatal(err)
	}
	f.Add(raw)
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		_, _ = Extract(data)
	})
}
