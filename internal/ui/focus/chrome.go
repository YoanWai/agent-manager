package focus

import (
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Fact is one reading on the full screen focus line, Spare ranking how
// readily it gives up its room: the higher, the sooner it goes.
type Fact struct {
	Text  string
	Spare int
}

// FitFacts joins the facts that fit in room cells. They give way one at a
// time as the terminal narrows, the least telling first, so a tight line
// still carries what it has room for rather than dropping the lot.
func FitFacts(facts []Fact, sep string, room int) string {
	facts = slices.Clone(facts)
	right := joinFacts(facts, sep)
	for len(facts) > 0 && ansi.StringWidth(right) > room {
		facts = dropSparest(facts)
		right = joinFacts(facts, sep)
	}
	return right
}

func joinFacts(facts []Fact, sep string) string {
	if len(facts) == 0 {
		return ""
	}
	parts := make([]string, len(facts))
	for i, fact := range facts {
		parts[i] = fact.Text
	}
	return strings.Join(parts, sep) + " "
}

func dropSparest(facts []Fact) []Fact {
	sparest := 0
	for i, fact := range facts {
		if fact.Spare >= facts[sparest].Spare {
			sparest = i
		}
	}
	return append(facts[:sparest], facts[sparest+1:]...)
}

// RuleKeys are the key labels the split's top rule names. An empty Review
// or Editor label leaves that hint out.
type RuleKeys struct {
	Back   string
	Review string
	Editor string
}

// TopRule is the hairline that caps the focused pane in the split, where
// the detail head above it already names the session, so the rule spends
// its title on the keys instead.
func TopRule(width int, keys RuleKeys, title, edge lipgloss.Style) string {
	hints := []string{"focused", keys.Back + " back"}
	if keys.Review != "" {
		hints = append(hints, keys.Review+" review")
	}
	if keys.Editor != "" {
		hints = append(hints, keys.Editor+" editor")
	}
	text := " " + strings.Join(hints, " · ") + " "
	rule := title.Render(text)
	rest := width - lipgloss.Width(text)
	if rest > 0 {
		rule += edge.Render(strings.Repeat("─", rest))
	}
	return rule
}
