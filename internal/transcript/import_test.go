package transcript

import (
	"os"
	"reflect"
	"testing"
)

func TestGoldenImports(t *testing.T) {
	tests := []struct {
		kind string
		file string
		want string
	}{
		{"vtt", "lecture.vtt", "lecture.vtt.json"},
		{"srt", "lecture.srt", "lecture.srt.json"},
		{"json", "lecture.json", "lecture.json.json"},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			raw := readTestdata(t, tt.file)
			got, err := Import(tt.kind, raw)
			if err != nil {
				t.Fatal(err)
			}
			want, err := Decode(readTestdata(t, tt.want))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got %#v\nwant %#v", got, want)
			}
		})
	}
}

func TestGoldenRejects(t *testing.T) {
	tests := []struct {
		kind string
		file string
	}{
		{"vtt", "bad.vtt"},
		{"srt", "bad.srt"},
		{"json", "bad.json"},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			if _, err := Import(tt.kind, readTestdata(t, tt.file)); err == nil {
				t.Fatal("malformed transcript was accepted")
			}
		})
	}
	if _, err := Import("pdf", []byte("x")); err == nil {
		t.Fatal("unknown format was accepted")
	}
	if _, err := ParseVTT([]byte("NOT A VTT")); err == nil {
		t.Fatal("missing header was accepted")
	}
}

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
