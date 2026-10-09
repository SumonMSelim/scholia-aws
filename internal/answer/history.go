package answer

import (
	"strings"
	"unicode/utf8"

	"github.com/sumonmselim/scholia-aws/internal/domain"
	"github.com/sumonmselim/scholia-aws/internal/model"
)

// History budget. Mock exams and follow-ups need earlier turns; the budget
// keeps the prompt bounded. The oldest turns go first and a turn is never cut.
const (
	MaxHistoryTurns = 12
	MaxHistoryChars = 24000
)

// Turn is one earlier message in a chat. Role is domain.RoleUser or domain.RoleAssistant.
type Turn struct {
	Role string
	Text string
}

// TrimHistory keeps the newest turns that fit the budget, in order.
func TrimHistory(turns []Turn) []Turn {
	kept := 0
	chars := 0
	start := len(turns)
	for i := len(turns) - 1; i >= 0; i-- {
		size := utf8.RuneCountInString(turns[i].Text)
		if kept == MaxHistoryTurns || chars+size > MaxHistoryChars {
			break
		}
		kept++
		chars += size
		start = i
	}
	return turns[start:]
}

// conversation turns history plus the new user text into model messages that
// alternate and start with the user, which Bedrock Converse requires. Empty
// turns are dropped and neighbours with the same role are joined. question ends the
// last user message as its guarded part, so a guardrail reads the student's words
// and not the passages or earlier turns around them.
func conversation(system string, history []Turn, user, question string) []model.Message {
	msgs := []model.Message{{Role: model.RoleSystem, Text: system}}
	add := func(role model.Role, text string) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		last := &msgs[len(msgs)-1]
		if last.Role == role {
			last.Text += "\n\n" + text
			return
		}
		if last.Role == model.RoleSystem && role == model.RoleAssistant {
			return
		}
		msgs = append(msgs, model.Message{Role: role, Text: text})
	}
	for _, turn := range history {
		role := model.RoleUser
		if turn.Role == domain.RoleAssistant {
			role = model.RoleAssistant
		}
		add(role, turn.Text)
	}
	add(model.RoleUser, user)
	msgs[len(msgs)-1].Guard = strings.TrimSpace(question)
	return msgs
}

func clipRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}
