package embed

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
)

func TestFakeIsStableAndNonZero(t *testing.T) {
	f := &Fake{}
	got, err := f.Embed(context.Background(), DefaultModel, "packets")
	if err != nil {
		t.Fatal(err)
	}
	again, err := f.Embed(context.Background(), DefaultModel, "packets")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 || len(again) != 4 {
		t.Fatalf("dim %d and %d", len(got), len(again))
	}
	for i := range got {
		if got[i] == 0 || got[i] != again[i] {
			t.Fatalf("value %v", got)
		}
	}
	other, err := f.Embed(context.Background(), "other-model", "packets")
	if err != nil {
		t.Fatal(err)
	}
	same := true
	for i := range got {
		if got[i] != other[i] {
			same = false
		}
	}
	if same {
		t.Fatal("model id did not change the vector")
	}
	if f.Calls != 3 || f.LastModel != "other-model" || f.LastText != "packets" {
		t.Fatalf("fake recorded %+v", f)
	}
}

func TestFakeRejects(t *testing.T) {
	f := &Fake{Err: errors.New("down")}
	if _, err := f.Embed(context.Background(), DefaultModel, "packets"); err == nil {
		t.Fatal("expected error")
	}
	f.Err = nil
	f.Dim = -1
	if _, err := f.Embed(context.Background(), DefaultModel, "packets"); err == nil {
		t.Fatal("expected dimension error")
	}
	f.Dim = 4
	if _, err := f.Embed(context.Background(), " ", " "); err == nil {
		t.Fatal("expected missing input error")
	}
}

type stubModel struct {
	body []byte
	err  error
	got  *bedrockruntime.InvokeModelInput
}

func (s *stubModel) InvokeModel(_ context.Context, params *bedrockruntime.InvokeModelInput, _ ...func(*bedrockruntime.Options)) (*bedrockruntime.InvokeModelOutput, error) {
	s.got = params
	if s.err != nil {
		return nil, s.err
	}
	return &bedrockruntime.InvokeModelOutput{Body: s.body}, nil
}

func TestEmbed(t *testing.T) {
	stub := &stubModel{body: []byte(`{"embedding":[0.25,0.5],"inputTextTokenCount":1}`)}
	c := &Client{api: stub}
	got, err := c.Embed(context.Background(), DefaultModel, "packets")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != 0.25 || got[1] != 0.5 {
		t.Fatalf("embedding %v", got)
	}
	if stub.got == nil || stub.got.ModelId == nil || *stub.got.ModelId != DefaultModel {
		t.Fatalf("model id %+v", stub.got)
	}
	if !strings.Contains(string(stub.got.Body), `"inputText":"packets"`) {
		t.Fatalf("body %s", stub.got.Body)
	}
}

func TestEmbedRejects(t *testing.T) {
	stub := &stubModel{err: errors.New("down")}
	c := &Client{api: stub}
	cases := []struct {
		name    string
		modelID string
		text    string
		body    []byte
		wantErr string
	}{
		{name: "missing", modelID: "", text: "packets", wantErr: "required"},
		{name: "family", modelID: "cohere.embed-english-v3", text: "packets", wantErr: "titan"},
		{name: "invoke", modelID: DefaultModel, text: "packets", wantErr: "down"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.Embed(context.Background(), tc.modelID, tc.text)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err %v", err)
			}
		})
	}
	stub.err = nil
	stub.body = []byte(`not-json`)
	if _, err := c.Embed(context.Background(), DefaultModel, "packets"); err == nil {
		t.Fatal("expected bad json")
	}
	stub.body = []byte(`{"embedding":[]}`)
	if _, err := c.Embed(context.Background(), DefaultModel, "packets"); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("err %v", err)
	}
}

func TestOpenSetsEndpoint(t *testing.T) {
	withEndpoint := Open(aws.Config{Region: "us-east-1"}, "http://127.0.0.1:9")
	if withEndpoint == nil || withEndpoint.api == nil {
		t.Fatal("missing client")
	}
	plain := Open(aws.Config{Region: "us-east-1"}, "")
	if plain == nil || plain.api == nil {
		t.Fatal("missing client")
	}
}
