package help

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestContentDescribesGlobalAndReviewHelp(t *testing.T) {
	global := New(Global).Content(testContext(84))
	if global.Title != "? Keys" {
		t.Fatalf("global title = %q", global.Title)
	}
	if body := ansi.Strip(strings.Join(global.Lines, "\n")); !strings.Contains(body, "Tell your agent to manage sessions and terminals in Agent Manager") {
		t.Fatalf("global guidance missing:\n%s", body)
	}

	review := New(Review).Content(testContext(84))
	if review.Title != "? Review keys" {
		t.Fatalf("review title = %q", review.Title)
	}
	body := ansi.Strip(strings.Join(review.Lines, "\n"))
	for _, want := range []string{"Tell your agent what to review", "comment on the line"} {
		if !strings.Contains(body, want) {
			t.Errorf("review help missing %q:\n%s", want, body)
		}
	}
	for _, unwanted := range []string{"new session", "quick prompt", "messages (M)"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("review help includes %q:\n%s", unwanted, body)
		}
	}
}

func TestContentSearchUsesUnicodeCursorAndReportsNoMatches(t *testing.T) {
	state := State{scope: Global, query: "中文", searching: true, scroll: 7}
	content := state.Content(testContext(84))
	if len(content.Head) != 2 || !strings.Contains(content.Head[0], "中文") || !strings.Contains(content.Head[0], testCursorMarker) {
		t.Fatalf("search head = %#v", content.Head)
	}
	if content.Offset != 7 {
		t.Fatalf("offset = %d, want 7", content.Offset)
	}

	state.searching = false
	content = state.Content(testContext(84))
	if strings.Contains(strings.Join(content.Head, "\n"), testCursorMarker) {
		t.Fatal("finished search retained cursor marker")
	}

	state.query = "zzzz"
	content = state.Content(testContext(84))
	if got := ansi.Strip(strings.Join(content.Lines, "\n")); got != "no key matches that" {
		t.Fatalf("no-match body = %q", got)
	}
}

func TestContentSearchNarrowsTheBody(t *testing.T) {
	ctx := testContext(84)
	full := New(Global).Content(ctx)
	searched := State{scope: Global, query: "worktree"}.Content(ctx)
	if len(searched.Lines) >= len(full.Lines) {
		t.Fatalf("searched body has %d lines, full has %d", len(searched.Lines), len(full.Lines))
	}
}

func TestContentColumnsFitTheRequestedWidth(t *testing.T) {
	ctx := testContext(84)
	column := helpKeyColumn(ctx)
	for _, section := range helpSections(ctx) {
		for _, row := range section.rows {
			if width := ansi.StringWidth(row[0]); width >= column {
				t.Errorf("key %q is %d wide, column is %d", row[0], width, column)
			}
		}
	}
	for _, width := range []int{48, 52, 84} {
		ctx.Width = width
		for i, line := range New(Global).Content(ctx).Lines {
			if got := ansi.StringWidth(line); got > width {
				t.Errorf("width %d line %d is %d cells: %q", width, i, got, ansi.Strip(line))
			}
		}
	}
}

func TestDefaultDescriptionsFitTheDefaultCard(t *testing.T) {
	ctx := testContext(CardWidth(120) - 8)
	room := ctx.Width - helpKeyColumn(ctx)
	for _, section := range helpSections(ctx) {
		for _, row := range section.rows {
			if width := ansi.StringWidth(row[1]); width > room {
				t.Errorf("section %q: %q is %d wide, room is %d", section.title, row[1], width, room)
			}
		}
	}
}

func TestHighlightHandlesUnicodeCaseFoldingWithoutPanicking(t *testing.T) {
	// Folding "İ" lengthens it, which is the case that would slice out of
	// range if the run were measured by the raw query.
	ctx := testContext(60)
	for _, query := range []string{"İ", "ẞ", "", "  ", "the", "THE"} {
		highlightMatch("Tell your agent what to review", query, 60, ctx.Styles)
		for _, section := range helpSections(ctx) {
			for _, row := range section.rows {
				highlightMatch(row[1], query, 60, ctx.Styles)
			}
		}
	}
}

func TestContentUsesFreshStylesOnEveryRender(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })

	state := New(Global)
	first := testContext(84)
	first.Styles.Key = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff0000")).Bold(true)
	second := first
	second.Styles.Key = lipgloss.NewStyle().Foreground(lipgloss.Color("#00ff00")).Bold(true)

	red := strings.Join(state.Content(first).Lines, "\n")
	green := strings.Join(state.Content(second).Lines, "\n")
	if red == green {
		t.Fatal("changed style produced an identical rendered body")
	}
	if ansi.Strip(red) != ansi.Strip(green) {
		t.Fatal("style change altered help content")
	}
}

func TestContentFollowsCustomBindingLabels(t *testing.T) {
	ctx := testContext(84)
	ctx.ListKeys = ctx.ListKeys.With(keybind.NewSession, bindingOf(t, "N"))
	body := ansi.Strip(strings.Join(New(Global).Content(ctx).Lines, "\n"))
	found, stale := false, false
	for _, line := range strings.Split(body, "\n") {
		found = found || strings.HasPrefix(line, "N ") && strings.Contains(line, "new session")
		stale = stale || strings.HasPrefix(line, "n ") && strings.Contains(line, "new session")
	}
	if !found || stale {
		t.Fatalf("custom binding not reflected:\n%s", body)
	}
}

func TestCardWidthPreservesCurrentPolicy(t *testing.T) {
	for _, test := range []struct{ terminal, want int }{
		{20, 92},
		{28, 24},
		{60, 56},
		{120, 92},
		{200, 92},
	} {
		if got := CardWidth(test.terminal); got != test.want {
			t.Errorf("CardWidth(%d) = %d, want %d", test.terminal, got, test.want)
		}
	}
}
