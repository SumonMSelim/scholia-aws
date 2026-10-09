package pdfdoc

import (
	"os"
	"testing"
)

func FuzzPDF(f *testing.F) {
	f.Add([]byte("%PDF-1.4\n"))
	for _, name := range []string{"testdata/native.pdf", "testdata/scanned.pdf", "testdata/hybrid.pdf"} {
		raw, err := os.ReadFile(name)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(raw)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		_, _ = Extract(t.Context(), data)
	})
}
