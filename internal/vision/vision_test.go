package vision

import (
	"context"
	"errors"
	"testing"

	"github.com/sumonmselim/scholia-aws/internal/locator"
)

func TestShouldRead(t *testing.T) {
	cases := []struct {
		name string
		obs  Observation
		want bool
	}{
		{name: "none", obs: Observation{TextAmount: TextNone}},
		{name: "some", obs: Observation{TextAmount: TextSome}},
		{name: "block", obs: Observation{TextAmount: TextBlock}, want: true},
		{name: "chart", obs: Observation{TextAmount: TextNone, DataVisual: true}, want: true},
		{name: "unknown", obs: Observation{TextAmount: "lots"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if ShouldRead(tc.obs) != tc.want {
				t.Fatalf("ShouldRead(%+v) = %v", tc.obs, ShouldRead(tc.obs))
			}
		})
	}
}

func TestSanitize(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{name: "empty", in: ""},
		{name: "blank", in: " \n\t "},
		{name: "nulls", in: "\x00\x00"},
		{name: "prose", in: "  Hello\n", want: "Hello", ok: true},
		{name: "latex", in: "$$E = mc^2$$", want: "$$E = mc^2$$", ok: true},
		{name: "markdown", in: "| a | b |\n|---|---|\n| 1 | 2 |", want: "| a | b |\n|---|---|\n| 1 | 2 |", ok: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := Sanitize(tc.in)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("Sanitize(%q) = %q %v", tc.in, got, ok)
			}
		})
	}
}

type fakeModel struct {
	obs     Observation
	obsErr  error
	text    string
	textErr error
	reads   int
}

func (f *fakeModel) Observe(context.Context, string, []byte) (Observation, error) {
	return f.obs, f.obsErr
}

func (f *fakeModel) Read(context.Context, string, []byte) (string, error) {
	f.reads++
	return f.text, f.textErr
}

func TestInterpret(t *testing.T) {
	box := locator.BBox{X0: 0, Y0: 0, X1: 10, Y1: 20}
	page := locator.Locator{Kind: locator.KindPage, Page: 2, BBox: &box}
	pic := Picture{
		CourseID: "c", SourceID: "s", ParentID: "p", ChildID: "k",
		ContentType: "image/png", Image: []byte{1, 2, 3}, Locator: page,
	}

	t.Run("skip", func(t *testing.T) {
		model := &fakeModel{obs: Observation{TextAmount: TextSome, Caption: "a logo"}}
		_, _, ok, err := Interpret(context.Background(), model, pic)
		if err != nil || ok || model.reads != 0 {
			t.Fatalf("ok %v reads %d err %v", ok, model.reads, err)
		}
	})

	t.Run("latex", func(t *testing.T) {
		model := &fakeModel{
			obs:  Observation{TextAmount: TextBlock, Caption: "Energy"},
			text: "$$E = mc^2$$",
		}
		parent, child, ok, err := Interpret(context.Background(), model, pic)
		if err != nil || !ok || model.reads != 1 {
			t.Fatalf("ok %v reads %d err %v", ok, model.reads, err)
		}
		if child.ParentID != "p" || child.Text != "$$E = mc^2$$" || parent.Text != "Energy\n\n$$E = mc^2$$" {
			t.Fatalf("parent %q child %+v", parent.Text, child)
		}
		if len(child.Locators) != 1 || child.Locators[0].Page != 2 || child.Locators[0].BBox == nil {
			t.Fatalf("locator %+v", child.Locators)
		}
	})

	t.Run("chart", func(t *testing.T) {
		model := &fakeModel{obs: Observation{DataVisual: true}, text: "| a |\n|---|\n| 1 |"}
		slide := pic
		slide.Locator = locator.Locator{Kind: locator.KindSlide, Slide: 4}
		_, child, ok, err := Interpret(context.Background(), model, slide)
		if err != nil || !ok || child.Locators[0].Slide != 4 || child.Text != model.text {
			t.Fatalf("child %+v ok %v err %v", child, ok, err)
		}
	})

	t.Run("empty reading", func(t *testing.T) {
		model := &fakeModel{obs: Observation{TextAmount: TextBlock}, text: "  \n"}
		_, _, ok, err := Interpret(context.Background(), model, pic)
		if err != nil || ok || model.reads != 1 {
			t.Fatalf("ok %v reads %d err %v", ok, model.reads, err)
		}
	})

	t.Run("observe error", func(t *testing.T) {
		model := &fakeModel{obsErr: errors.New("throttled")}
		if _, _, _, err := Interpret(context.Background(), model, pic); err == nil || model.reads != 0 {
			t.Fatal("observe error was ignored")
		}
	})

	t.Run("read error", func(t *testing.T) {
		model := &fakeModel{obs: Observation{TextAmount: TextBlock}, textErr: errors.New("throttled")}
		if _, _, _, err := Interpret(context.Background(), model, pic); err == nil {
			t.Fatal("read error was ignored")
		}
	})

	t.Run("no model", func(t *testing.T) {
		if _, _, _, err := Interpret(context.Background(), nil, pic); err == nil {
			t.Fatal("nil model was accepted")
		}
	})

	t.Run("bad locator", func(t *testing.T) {
		bad := pic
		bad.Locator = locator.Locator{Kind: locator.KindPage}
		model := &fakeModel{obs: Observation{TextAmount: TextBlock}}
		if _, _, _, err := Interpret(context.Background(), model, bad); err == nil || model.reads != 0 {
			t.Fatal("bad locator was accepted")
		}
	})
}
