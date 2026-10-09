package answer

import (
	"context"
	"strings"
	"testing"

	"github.com/sumonmselim/scholia-aws/internal/domain"
	"github.com/sumonmselim/scholia-aws/internal/model"
	"github.com/sumonmselim/scholia-aws/internal/websearch"
)

// blockedStream passes the classifier and then has the guardrail stop the reply.
type blockedStream struct {
	scripted
}

func (b *blockedStream) Stream(_ context.Context, _ model.Request, emit func(string) error) error {
	b.streams++
	_ = emit("Sorry, the model cannot answer that.")
	return model.ErrGuardrail
}

func TestGuardrailBecomesUnsafeRefusal(t *testing.T) {
	m := &blockedStream{scripted{verdict: `{"intent":"question","safety":"ok"}`}}
	out, err := (&Service{Search: fixedSearch{chunks: []domain.Chunk{routeChunk}}, Model: m}).Answer(context.Background(), ask("next hop"), drop)
	if err != nil || !out.Refused || out.Reason != domain.RefusalUnsafe || out.Detail != "guardrail" || !strings.Contains(out.Text, "Networks") {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}

// The logs tell a classifier refusal from a guardrail one by the detail.
func TestRefusalDetailNamesTheClassifierLabel(t *testing.T) {
	m := &scripted{verdict: `{"intent":"question","safety":"abuse"}`}
	out, err := (&Service{Search: fixedSearch{chunks: []domain.Chunk{routeChunk}}, Model: m}).Answer(context.Background(), ask("next hop"), drop)
	if err != nil || !out.Refused || out.Detail != "abuse" || m.streams != 0 {
		t.Fatalf("out=%+v err=%v streams=%d", out, err, m.streams)
	}
}

func TestWebAllowanceIsCounted(t *testing.T) {
	tests := []struct {
		name    string
		allow   bool
		queries int
	}{
		{"room left", true, 1},
		{"spent", false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			web := &websearch.Fake{Results: []websearch.Result{{Title: "TCP", URL: "https://example.test/tcp"}}}
			var meters []string
			svc := &Service{
				Search: fixedSearch{}, Model: &captureModel{}, Web: web,
				WebAllowed: func(_ context.Context, meter string) bool {
					meters = append(meters, meter)
					return tt.allow
				},
			}
			req := ask("What is TCP?")
			req.Meter = "guest-1"
			out, err := svc.Answer(context.Background(), req, drop)
			if err != nil {
				t.Fatal(err)
			}
			if len(web.Queries) != tt.queries || len(meters) != 1 || meters[0] != "guest-1" {
				t.Fatalf("queries=%v meters=%v", web.Queries, meters)
			}
			// With the web spent and no course material, the reply is the usual refusal.
			if !tt.allow && (!out.Refused || out.Reason != domain.RefusalNoMaterial) {
				t.Fatalf("out = %+v", out)
			}
		})
	}
}
