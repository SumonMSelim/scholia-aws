package chunk

import (
	"strings"
	"testing"

	"github.com/sumonmselim/scholia-aws/internal/locator"
)

func TestProfile(t *testing.T) {
	if ProfileOf(nil) != ProfileProse {
		t.Fatal("empty document was not prose")
	}
	if ProfileOf([]Block{{Kind: KindHeading, Text: "A"}}) != ProfileHeadings {
		t.Fatal("heading document was not profiled")
	}
	if ProfileOf([]Block{{Kind: KindTable, Text: "| a |"}}) != ProfileTable {
		t.Fatal("table document was not profiled")
	}
	if ProfileOf([]Block{{Kind: KindHeading, Text: "A"}, {Kind: KindTable, Text: "| a |"}}) != ProfileMixed {
		t.Fatal("mixed document was not profiled")
	}
}

func TestHeadingSplits(t *testing.T) {
	blocks := []Block{
		{Kind: KindHeading, Level: 1, Text: "Nets"},
		{Kind: KindParagraph, Text: "Packets travel."},
		{Kind: KindHeading, Level: 2, Text: "Routing"},
		{Kind: KindParagraph, Text: "Tables pick the next hop."},
	}
	units, err := Split(blocks, Limits{Parent: 400, Child: 200})
	if err != nil {
		t.Fatal(err)
	}
	parents := parentsOf(units)
	if len(parents) != 2 {
		t.Fatalf("parents %+v", parents)
	}
	if parents[0].Breadcrumb != "Nets" || !strings.Contains(parents[0].Text, "Packets travel.") {
		t.Fatalf("first %+v", parents[0])
	}
	if parents[1].Breadcrumb != "Nets › Routing" || !strings.Contains(parents[1].Text, "Tables pick") {
		t.Fatalf("second %+v", parents[1])
	}
	if strings.Contains(parents[0].Text, "Routing") {
		t.Fatalf("first section kept the next heading %q", parents[0].Text)
	}
}

func TestTableHeaderCarry(t *testing.T) {
	table := "| Hop | Owner |\n| --- | --- |\n| 1 | edge |\n| 2 | core |\n| 3 | peer |"
	units, err := Split([]Block{{Kind: KindTable, Text: table}}, Limits{Parent: 200, Child: 45})
	if err != nil {
		t.Fatal(err)
	}
	children := childrenOf(units, 0)
	if len(children) < 2 {
		t.Fatalf("children %+v", children)
	}
	if !strings.HasPrefix(children[1].Text, "| Hop | Owner |") {
		t.Fatalf("continuation lost the header %q", children[1].Text)
	}
	if !strings.Contains(children[1].Text, "| 2 | core |") && !strings.Contains(children[1].Text, "| 3 | peer |") {
		t.Fatalf("continuation lost a row %q", children[1].Text)
	}
}

func TestParentChildLink(t *testing.T) {
	blocks := []Block{
		{Kind: KindParagraph, Text: "alpha beta"},
		{Kind: KindParagraph, Text: "gamma delta"},
	}
	units, err := Split(blocks, Limits{Parent: 40, Child: 12})
	if err != nil {
		t.Fatal(err)
	}
	if len(units) < 3 || units[0].Parent != -1 {
		t.Fatalf("units %+v", units)
	}
	for _, child := range units[1:] {
		if child.Parent != 0 {
			t.Fatalf("child parent %d", child.Parent)
		}
		if child.Text == "" || strings.Contains(units[0].Text, child.Text) == false {
			t.Fatalf("parent %q does not quote child %q", units[0].Text, child.Text)
		}
	}
}

func TestLocatorBoundary(t *testing.T) {
	first := locator.Locator{Kind: locator.KindTime, StartMS: 0, EndMS: 1000}
	second := locator.Locator{Kind: locator.KindTime, StartMS: 1000, EndMS: 2000}
	third := locator.Locator{Kind: locator.KindTime, StartMS: 2000, EndMS: 3000}
	blocks := []Block{
		{Kind: KindParagraph, Text: "aaaa", Locators: []locator.Locator{first}},
		{Kind: KindParagraph, Text: "bbbb", Locators: []locator.Locator{second}},
		{Kind: KindParagraph, Text: "cccc", Locators: []locator.Locator{third}},
	}
	units, err := Split(blocks, Limits{Parent: 10, Child: 10})
	if err != nil {
		t.Fatal(err)
	}
	parents := parentsOf(units)
	if len(parents) != 2 {
		t.Fatalf("parents %+v", parents)
	}
	if !hasTime(parents[0].Locators, 0, 1000) || !hasTime(parents[0].Locators, 1000, 2000) || hasTime(parents[0].Locators, 2000, 3000) {
		t.Fatalf("first locators %+v", parents[0].Locators)
	}
	if hasTime(parents[1].Locators, 0, 1000) || !hasTime(parents[1].Locators, 2000, 3000) {
		t.Fatalf("second locators %+v", parents[1].Locators)
	}

	long := locator.Locator{Kind: locator.KindPage, Page: 3, BBox: &locator.BBox{X0: 1, Y0: 2, X1: 3, Y1: 4}}
	units, err = Split([]Block{{Kind: KindParagraph, Text: strings.Repeat("w", 30), Locators: []locator.Locator{long}}}, Limits{Parent: 40, Child: 10})
	if err != nil {
		t.Fatal(err)
	}
	children := childrenOf(units, 0)
	if len(children) < 2 {
		t.Fatalf("pieces %+v", children)
	}
	for _, child := range children {
		if len(child.Locators) != 1 || child.Locators[0].Page != 3 {
			t.Fatalf("split lost the page locator %+v", child.Locators)
		}
	}
}

func TestSplitRejectsLimits(t *testing.T) {
	if _, err := Split(nil, Limits{Parent: 2, Child: 4}); err == nil {
		t.Fatal("child larger than parent was accepted")
	}
}

func parentsOf(units []Unit) []Unit {
	var out []Unit
	for _, unit := range units {
		if unit.Parent < 0 {
			out = append(out, unit)
		}
	}
	return out
}

func childrenOf(units []Unit, parent int) []Unit {
	var out []Unit
	for _, unit := range units {
		if unit.Parent == parent {
			out = append(out, unit)
		}
	}
	return out
}

func hasTime(locs []locator.Locator, start, end int64) bool {
	for _, loc := range locs {
		if loc.Kind == locator.KindTime && loc.StartMS == start && loc.EndMS == end {
			return true
		}
	}
	return false
}
