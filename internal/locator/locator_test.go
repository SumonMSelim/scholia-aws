package locator

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLocatorRoundTrip(t *testing.T) {
	box := BBox{X0: 1, Y0: 2, X1: 3, Y1: 4}
	tests := []struct {
		name string
		in   Locator
		json string
	}{
		{
			name: "page",
			in:   Locator{Kind: KindPage, Page: 3, BBox: &box},
			json: `{"kind":"page","page":3,"bbox":{"x0":1,"y0":2,"x1":3,"y1":4}}`,
		},
		{
			name: "slide",
			in:   Locator{Kind: KindSlide, Slide: 4},
			json: `{"kind":"slide","slide":4}`,
		},
		{
			name: "time at start",
			in:   Locator{Kind: KindTime, StartMS: 0, EndMS: 1500},
			json: `{"kind":"time","start_ms":0,"end_ms":1500}`,
		},
		{
			name: "text at start",
			in:   Locator{Kind: KindText, Start: 0, End: 12},
			json: `{"kind":"text","start":0,"end":12}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := json.Marshal(tt.in)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(raw) != tt.json {
				t.Fatalf("json = %s, want %s", raw, tt.json)
			}
			var got Locator
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			again, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("remarshal: %v", err)
			}
			if string(again) != tt.json {
				t.Fatalf("round trip = %s, want %s", again, tt.json)
			}
		})
	}
}

func TestLocatorRejects(t *testing.T) {
	tests := []struct {
		name string
		json string
		want string
	}{
		{"unknown kind", `{"kind":"video"}`, "not valid"},
		{"page zero", `{"kind":"page","page":0,"bbox":{"x0":0,"y0":0,"x1":1,"y1":1}}`, "page must be"},
		{"page missing bbox", `{"kind":"page","page":1}`, "bbox"},
		{"page extra field", `{"kind":"page","page":1,"bbox":{"x0":0,"y0":0,"x1":1,"y1":1},"slide":2}`, "unknown field"},
		{"bbox inverted", `{"kind":"page","page":1,"bbox":{"x0":2,"y0":0,"x1":1,"y1":1}}`, "bbox"},
		{"slide zero", `{"kind":"slide","slide":0}`, "slide must be"},
		{"time reversed", `{"kind":"time","start_ms":5,"end_ms":4}`, "end_ms"},
		{"text reversed", `{"kind":"text","start":3,"end":1}`, "end must be"},
		{"not an object", `"page"`, "locator"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got Locator
			err := json.Unmarshal([]byte(tt.json), &got)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got error %v, want one mentioning %q", err, tt.want)
			}
		})
	}
}
