package focus

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestFitFactsDropsTheSparestFirst(t *testing.T) {
	facts := []Fact{{Text: "dir", Spare: 3}, {Text: "cpu", Spare: 1}, {Text: "started", Spare: 4}, {Text: "queued", Spare: 0}}
	if got := FitFacts(facts, " · ", 100); got != "dir · cpu · started · queued " {
		t.Fatalf("wide = %q", got)
	}
	if got := FitFacts(facts, " · ", 20); got != "dir · cpu · queued " {
		t.Fatalf("narrow = %q", got)
	}
	if got := FitFacts(facts, " · ", 3); got != "" {
		t.Fatalf("no room = %q", got)
	}
}

// Ties go to the later fact, so equally spare readings give way from the
// right edge inward.
func TestFitFactsBreaksTiesFromTheRight(t *testing.T) {
	facts := []Fact{{Text: "cpu", Spare: 1}, {Text: "ram", Spare: 1}}
	if got := FitFacts(facts, " ", 5); got != "cpu " {
		t.Fatalf("tie = %q", got)
	}
}

func TestTopRuleNamesOnlyBoundKeys(t *testing.T) {
	plain := lipgloss.NewStyle()
	full := TopRule(60, RuleKeys{Back: "ctrl+q", Review: "f2", Editor: "f3"}, plain, plain)
	if !strings.HasPrefix(full, " focused · ctrl+q back · f2 review · f3 editor ") {
		t.Fatalf("rule = %q", full)
	}
	if got := lipgloss.Width(full); got != 60 {
		t.Fatalf("rule width = %d, want 60", got)
	}
	bare := TopRule(10, RuleKeys{Back: "esc"}, plain, plain)
	if bare != " focused · esc back " {
		t.Fatalf("narrow rule = %q", bare)
	}
}
