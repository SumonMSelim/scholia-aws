// Package chunk turns extracted spans into parent and child chunks.
// Children are what retrieval searches. Answers quote the parent.
package chunk

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/sumonmselim/scholia-aws/internal/locator"
)

const (
	// KindParagraph is body text.
	KindParagraph = "paragraph"
	// KindHeading is a section title. Level is 1 for the top of the document.
	KindHeading = "heading"
	// KindTable is a markdown table. Its header is repeated when a row crosses a chunk boundary.
	KindTable = "table"
)

// Block is one extracted span: a heading, a paragraph, or a markdown table.
type Block struct {
	Kind     string
	Level    int
	Text     string
	Locators []locator.Locator
}

// Limits are rune budgets. A child is never larger than its parent.
type Limits struct {
	Parent int
	Child  int
}

// DefaultLimits fits a section in a parent and a few sentences in a child.
func DefaultLimits() Limits {
	return Limits{Parent: 2000, Child: 500}
}

// Profile is how the document is shaped, chosen before any split.
type Profile string

const (
	// ProfileProse has no headings and no tables.
	ProfileProse Profile = "prose"
	// ProfileHeadings splits on section titles.
	ProfileHeadings Profile = "headings"
	// ProfileTable keeps markdown tables intact and carries their header.
	ProfileTable Profile = "table"
	// ProfileMixed has both headings and tables, so both rules apply.
	ProfileMixed Profile = "mixed"
)

// Unit is one chunk before it is given an id. Parent is -1 for a parent chunk
// and the index of that parent for a child.
type Unit struct {
	Parent     int
	Text       string
	Breadcrumb string
	Locators   []locator.Locator
}

// ProfileOf inspects the blocks and chooses the split rules.
func ProfileOf(blocks []Block) Profile {
	var headings, tables bool
	for _, block := range blocks {
		switch block.Kind {
		case KindHeading:
			headings = true
		case KindTable:
			tables = true
		}
	}
	switch {
	case headings && tables:
		return ProfileMixed
	case headings:
		return ProfileHeadings
	case tables:
		return ProfileTable
	default:
		return ProfileProse
	}
}

// Split profiles the blocks, then cuts them into parents and children.
func Split(blocks []Block, limits Limits) ([]Unit, error) {
	if limits.Child < 1 || limits.Parent < limits.Child {
		return nil, errors.New("chunk limits: child must be >= 1 and parent must be >= child")
	}
	atoms := flatten(blocks, limits.Child, ProfileOf(blocks))
	if len(atoms) == 0 {
		return nil, nil
	}
	var units []Unit
	for _, parent := range pack(atoms, limits.Parent) {
		index := len(units)
		units = append(units, unitFrom(parent, -1))
		for _, child := range pack(parent, limits.Child) {
			units = append(units, unitFrom(child, index))
		}
	}
	return units, nil
}

type atom struct {
	text    string
	path    string
	locs    []locator.Locator
	heading bool
	row     bool
	header  string
	headLoc []locator.Locator
}

func flatten(blocks []Block, child int, profile Profile) []atom {
	var (
		stack []heading
		out   []atom
	)
	path := func() string {
		parts := make([]string, len(stack))
		for i, h := range stack {
			parts[i] = h.text
		}
		return strings.Join(parts, " › ")
	}
	for _, block := range blocks {
		text := strings.TrimSpace(block.Text)
		if text == "" {
			continue
		}
		switch block.Kind {
		case KindHeading:
			if profile != ProfileHeadings && profile != ProfileMixed {
				out = append(out, proseAtoms(text, path(), block.Locators, child)...)
				continue
			}
			level := block.Level
			if level < 1 {
				level = 1
			}
			for len(stack) >= level {
				stack = stack[:len(stack)-1]
			}
			stack = append(stack, heading{level: level, text: text})
			out = append(out, atom{text: text, path: path(), locs: block.Locators, heading: true})
		case KindTable:
			if profile != ProfileTable && profile != ProfileMixed {
				out = append(out, proseAtoms(text, path(), block.Locators, child)...)
				continue
			}
			out = append(out, tableAtoms(text, path(), block.Locators, child)...)
		default:
			out = append(out, proseAtoms(text, path(), block.Locators, child)...)
		}
	}
	return out
}

type heading struct {
	level int
	text  string
}

func proseAtoms(text, path string, locs []locator.Locator, limit int) []atom {
	parts := pieces(text, limit)
	out := make([]atom, 0, len(parts))
	for _, part := range parts {
		out = append(out, atom{text: part, path: path, locs: locs})
	}
	return out
}

func tableAtoms(text, path string, locs []locator.Locator, limit int) []atom {
	var header, rule string
	var rows []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if header == "" {
			header = line
			continue
		}
		if rule == "" && isRule(line) {
			rule = line
			continue
		}
		rows = append(rows, line)
	}
	if header == "" {
		return nil
	}
	headerText := header
	if rule != "" {
		headerText = header + "\n" + rule
	}
	out := []atom{{text: headerText, path: path, locs: locs, row: true}}
	for _, row := range rows {
		for _, part := range pieces(row, limit) {
			out = append(out, atom{
				text: part, path: path, locs: locs, row: true,
				header: headerText, headLoc: locs,
			})
		}
	}
	return out
}

func isRule(line string) bool {
	if !strings.Contains(line, "-") {
		return false
	}
	return strings.Trim(line, "|-: ") == ""
}

func pieces(text string, limit int) []string {
	rs := []rune(text)
	if len(rs) <= limit {
		return []string{text}
	}
	var out []string
	for len(rs) > 0 {
		n := limit
		if n > len(rs) {
			n = len(rs)
		}
		if n < len(rs) {
			window := string(rs[:n])
			if cut := strings.LastIndex(window, " "); cut > len(window)/2 {
				n = utf8.RuneCountInString(window[:cut])
			}
		}
		part := strings.TrimSpace(string(rs[:n]))
		if part != "" {
			out = append(out, part)
		}
		rs = rs[n:]
		for len(rs) > 0 && unicode.IsSpace(rs[0]) {
			rs = rs[1:]
		}
	}
	return out
}

func pack(atoms []atom, limit int) [][]atom {
	var groups [][]atom
	var cur []atom
	size := 0
	flush := func() {
		if len(cur) == 0 {
			return
		}
		groups = append(groups, cur)
		cur = nil
		size = 0
	}
	for _, item := range atoms {
		if item.heading && len(cur) > 0 {
			flush()
		}
		extra := 0
		if len(cur) > 0 {
			extra = separator(cur[len(cur)-1], item)
		}
		if len(cur) > 0 && size+extra+utf8.RuneCountInString(item.text) > limit {
			flush()
		}
		if len(cur) == 0 && item.header != "" {
			cur = append(cur, atom{text: item.header, path: item.path, locs: item.headLoc, row: true})
			size = utf8.RuneCountInString(item.header)
		}
		if len(cur) > 0 {
			size += separator(cur[len(cur)-1], item)
		}
		cur = append(cur, item)
		size += utf8.RuneCountInString(item.text)
	}
	flush()
	return groups
}

func separator(prev, next atom) int {
	if prev.row && next.row {
		return 1
	}
	return 2
}

func unitFrom(atoms []atom, parent int) Unit {
	path := ""
	if len(atoms) > 0 {
		path = atoms[len(atoms)-1].path
	}
	return Unit{Parent: parent, Text: join(atoms), Breadcrumb: path, Locators: locsOf(atoms)}
}

func join(atoms []atom) string {
	var b strings.Builder
	for i, item := range atoms {
		if i > 0 {
			if separator(atoms[i-1], item) == 1 {
				b.WriteByte('\n')
			} else {
				b.WriteString("\n\n")
			}
		}
		b.WriteString(item.text)
	}
	return b.String()
}

func locsOf(atoms []atom) []locator.Locator {
	var out []locator.Locator
	for _, item := range atoms {
		for _, loc := range item.locs {
			if !hasLocator(out, loc) {
				out = append(out, loc)
			}
		}
	}
	return out
}

func hasLocator(locs []locator.Locator, loc locator.Locator) bool {
	for _, have := range locs {
		if have.Kind != loc.Kind || have.Page != loc.Page || have.Slide != loc.Slide ||
			have.StartMS != loc.StartMS || have.EndMS != loc.EndMS || have.Start != loc.Start || have.End != loc.End {
			continue
		}
		if have.BBox == nil || loc.BBox == nil {
			if have.BBox == loc.BBox {
				return true
			}
			continue
		}
		if *have.BBox == *loc.BBox {
			return true
		}
	}
	return false
}
