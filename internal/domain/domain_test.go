package domain

import (
	"strings"
	"testing"

	"github.com/sumonmselim/scholia-aws/internal/locator"
)

func TestValidate(t *testing.T) {
	okLoc := locator.Locator{Kind: locator.KindSlide, Slide: 1}
	tests := []struct {
		name    string
		err     error
		wantErr string
	}{
		{"course ok", Course{ID: "c", Title: "Nets"}.Validate(), ""},
		{"course missing id", Course{Title: "Nets"}.Validate(), "id is required"},
		{"course missing title", Course{ID: "c"}.Validate(), "title is required"},
		{"source queued", Source{ID: "s", CourseID: "c", Name: "a.mp3", Status: SourceQueued}.Validate(), ""},
		{"source failed", Source{ID: "s", CourseID: "c", Name: "a.mp3", Status: SourceFailed, FailureReason: "bad magic"}.Validate(), ""},
		{"source failed without reason", Source{ID: "s", CourseID: "c", Name: "a.mp3", Status: SourceFailed}.Validate(), "failure reason is required"},
		{"source reason while queued", Source{ID: "s", CourseID: "c", Name: "a.mp3", Status: SourceQueued, FailureReason: "x"}.Validate(), "only set when status is failed"},
		{"source bad status", Source{ID: "s", CourseID: "c", Name: "a.mp3", Status: "done"}.Validate(), "not valid"},
		{"source with video", Source{ID: "s", CourseID: "c", Name: "a.vtt", Status: SourceQueued, YouTubeID: "ZA-tUyM_y7s"}.Validate(), ""},
		{"source bad video id", Source{ID: "s", CourseID: "c", Name: "a.vtt", Status: SourceQueued, YouTubeID: "x\"><script>"}.Validate(), "youtube id is not valid"},
		{"chunk ok", Chunk{ID: "k", CourseID: "c", SourceID: "s", Locators: []locator.Locator{okLoc}}.Validate(), ""},
		{"chat ok", Chat{ID: "h", OwnerID: "u", CourseID: "c", Title: "New chat"}.Validate(), ""},
		{"chat missing id", Chat{OwnerID: "u", CourseID: "c", Title: "t"}.Validate(), "id is required"},
		{"chat missing owner", Chat{ID: "h", CourseID: "c", Title: "t"}.Validate(), "owner id is required"},
		{"chat missing course", Chat{ID: "h", OwnerID: "u", Title: "t"}.Validate(), "course id is required"},
		{"chat missing title", Chat{ID: "h", OwnerID: "u", CourseID: "c"}.Validate(), "title is required"},
		{"message ok", Message{ID: "m", ChatID: "h", Role: RoleUser, Mode: ModeAnswer}.Validate(), ""},
		{"message missing ids", Message{Role: RoleUser, Mode: ModeAnswer}.Validate(), "id and chat id"},
		{"message bad role", Message{ID: "m", ChatID: "h", Role: "system", Mode: ModeAnswer}.Validate(), "role"},
		{"message bad mode", Message{ID: "m", ChatID: "h", Role: RoleAssistant, Mode: "solve"}.Validate(), "mode"},
		{"chunk bad locator", Chunk{ID: "k", CourseID: "c", SourceID: "s", Locators: []locator.Locator{{Kind: locator.KindSlide}}}.Validate(), "slide must be"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErr == "" {
				if tt.err != nil {
					t.Fatalf("unexpected error: %v", tt.err)
				}
				return
			}
			if tt.err == nil || !strings.Contains(tt.err.Error(), tt.wantErr) {
				t.Fatalf("got error %v, want one mentioning %q", tt.err, tt.wantErr)
			}
		})
	}
}
