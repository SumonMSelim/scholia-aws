package answer

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sumonmselim/scholia-aws/internal/domain"
	"github.com/sumonmselim/scholia-aws/internal/model"
)

type modelCourse struct {
	course domain.Course
	err    error
}

func (m modelCourse) GetCourse(context.Context, string) (domain.Course, error) {
	return m.course, m.err
}

type recordSearch struct {
	modelID string
}

func (r *recordSearch) Search(_ context.Context, _, modelID, _ string, _ int) ([]domain.Chunk, error) {
	r.modelID = modelID
	return []domain.Chunk{{ID: "p", CourseID: "c1", SourceID: "s1", Text: "Routing picks the next hop."}}, nil
}

// fakeProviders returns a fixed provider and records what it was asked for.
type fakeProviders struct {
	provider model.Provider
	err      error
	userID   string
	modelID  string
	calls    int
}

func (f *fakeProviders) For(_ context.Context, userID, modelID string) (model.Provider, string, error) {
	f.calls++
	f.userID, f.modelID = userID, modelID
	return f.provider, modelID, f.err
}

func TestBoundUsesTheCourse(t *testing.T) {
	search := &recordSearch{}
	chat := &captureModel{}
	bound := Bound{
		Service: &Service{Search: search, Model: chat, EmbedModel: "amazon.titan-embed-text-v2:0"},
		Courses: modelCourse{course: domain.Course{ID: "c1", Title: "Nets"}},
	}
	out, err := bound.Answer(context.Background(), Request{CourseID: "c1", Question: "next hop"}, drop)
	if err != nil || out.Refused {
		t.Fatalf("answer: %+v %v", out, err)
	}
	if search.modelID != "amazon.titan-embed-text-v2:0" || chat.req.Model != model.DefaultModel {
		t.Fatalf("embed %q chat %q", search.modelID, chat.req.Model)
	}
	if user := chat.req.Messages[1].Text; !strings.Contains(user, "Nets") {
		t.Fatalf("course title missing: %s", user)
	}
}

func TestBoundModelPrecedence(t *testing.T) {
	tests := []struct {
		name    string
		request string
		service string
		want    string
	}{
		{"request wins", "gpt-4.1", "svc", "gpt-4.1"},
		{"service default", "", "svc", "svc"},
		{"package default", "", "", model.DefaultModel},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chat := &captureModel{}
			bound := Bound{
				Service: &Service{Search: &recordSearch{}, Model: chat, ChatModel: tt.service},
				Courses: modelCourse{course: domain.Course{ID: "c1", Title: "Nets"}},
			}
			if _, err := (TutorMode{Bound: bound}).Answer(context.Background(), Request{CourseID: "c1", Question: "next hop", ChatModel: tt.request}, drop); err != nil {
				t.Fatal(err)
			}
			if chat.req.Model != tt.want {
				t.Fatalf("model %q, want %q", chat.req.Model, tt.want)
			}
			if chat.req.Messages[0].Text != systemPrompt(TaskExplain) {
				t.Fatal("tutor mode did not use the explain prompt")
			}
		})
	}
}

func TestBoundPicksTheCallersProvider(t *testing.T) {
	fallback := &captureModel{}
	keyed := &captureModel{}
	tests := []struct {
		name     string
		userID   string
		provider model.Provider
		err      error
		wantUsed *captureModel
		wantCall int
		wantErr  bool
	}{
		{"stored key answers", "u1", keyed, nil, keyed, 1, false},
		{"no key falls back", "u1", nil, nil, fallback, 1, false},
		{"anonymous asks for a server provider", "", nil, nil, fallback, 1, false},
		{"key failure stops", "u1", nil, errors.New("kms down"), nil, 1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			*fallback, *keyed = captureModel{}, captureModel{}
			providers := &fakeProviders{provider: tt.provider, err: tt.err}
			bound := Bound{
				Service:   &Service{Search: &recordSearch{}, Model: fallback},
				Courses:   modelCourse{course: domain.Course{ID: "c1", Title: "Nets"}},
				Providers: providers,
			}
			_, err := bound.Answer(context.Background(), Request{CourseID: "c1", Question: "next hop", UserID: tt.userID, ChatModel: "custom-model"}, drop)
			if (err != nil) != tt.wantErr || providers.calls != tt.wantCall {
				t.Fatalf("err=%v calls=%d", err, providers.calls)
			}
			if providers.userID != tt.userID || providers.modelID != "custom-model" {
				t.Fatalf("asked for %+v", providers)
			}
			if tt.wantUsed != nil && tt.wantUsed.calls != 1 {
				t.Fatal("the expected provider did not answer")
			}
		})
	}
}

func TestBoundWithoutTheCourse(t *testing.T) {
	chat := &captureModel{}
	bound := Bound{Service: &Service{Search: &recordSearch{}, Model: chat}, Courses: modelCourse{err: errors.New("missing")}}
	if _, err := bound.Answer(context.Background(), Request{CourseID: "c1", Question: "next hop"}, drop); err != nil {
		t.Fatal(err)
	}
	if chat.req.Model != model.DefaultModel {
		t.Fatalf("model %q", chat.req.Model)
	}
}
