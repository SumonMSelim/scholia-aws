package httpapi

import (
	"strings"
	"unicode/utf8"
)

const maxModelRunes = 128

func modelID(id string) bool {
	if id == "" {
		return true
	}
	if utf8.RuneCountInString(id) > maxModelRunes || strings.ContainsAny(id, " \t\r\n") {
		return false
	}
	return true
}
