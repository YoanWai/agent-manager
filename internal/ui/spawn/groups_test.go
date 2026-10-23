package spawn

import (
	"slices"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/lipgloss"
)

func TestPickerWrapsAtTheEnds(t *testing.T) {
	t.Parallel()
	p := Picker{Groups: []GroupOption{{Path: ""}, {Path: "alpha"}, {Path: "beta"}}}

	p.Step(-1)
	if got := p.GroupPath(); got != "beta" {
		t.Fatalf("up past the first row should wrap to the last, got %q", got)
	}

	p.Step(1)
	if got := p.GroupPath(); got != "" {
		t.Fatalf("down past the last row should wrap to the first, got %q", got)
	}

	if (Picker{}).Step(1) {
		t.Fatal("an empty picker takes no step")
	}
}

func TestPickerReselectFallsBackToTheRoot(t *testing.T) {
	t.Parallel()
	p := Picker{Groups: []GroupOption{{Path: ""}, {Path: "alpha"}, {Path: "alpha/inner"}}}

	p.GroupIndex = 2
	p.Reselect("alpha", func(GroupOption) bool { return true })
	if got := p.GroupPath(); got != "alpha" {
		t.Fatalf("reselect = %q, want alpha", got)
	}

	p.Reselect("gone", func(GroupOption) bool { return true })
	if got := p.GroupPath(); got != "" {
		t.Fatalf("a path the picker dropped should land on the root, got %q", got)
	}

	p.GroupIndex = 1
	p.Reselect("alpha", func(GroupOption) bool { return false })
	if got := p.GroupPath(); got != "" {
		t.Fatalf("a row the match refuses should land on the root, got %q", got)
	}
}

func TestChildIndexRanksTheGroupsListThenTheName(t *testing.T) {
	t.Parallel()
	paths := map[string]bool{"alpha": true, "beta": true, "alpha/inner": true, "alpha/deep": true}
	children := ChildIndex(paths, []string{"beta", "alpha"})

	if got, want := children[""], []string{"beta", "alpha"}; !slices.Equal(got, want) {
		t.Fatalf("root children = %v, want %v", got, want)
	}
	if got, want := children["alpha"], []string{"alpha/deep", "alpha/inner"}; !slices.Equal(got, want) {
		t.Fatalf("alpha children = %v, want %v", got, want)
	}
}

func TestGroupClosureKeepsEveryAncestor(t *testing.T) {
	t.Parallel()
	paths := GroupClosure([]string{"a/b/c"}, []string{"d/e"})
	for _, want := range []string{"a", "a/b", "a/b/c", "d", "d/e"} {
		if !paths[want] {
			t.Fatalf("closure misses %q: %v", want, paths)
		}
	}
	if paths[""] {
		t.Fatal("the root is the empty path, not a row in the closure")
	}
}

func TestViewGroupOptionsNamesEachRow(t *testing.T) {
	t.Parallel()
	groups := []GroupOption{{Path: ""}, {Path: "alpha/beta", Depth: 1}, {Path: "alpha", Depth: 1, SessionID: "s1", Name: "shell"}}
	st := Styles{
		GlyphMode: presentation.GlyphModeBasic,
		Muted:     lipgloss.NewStyle(),
		Accent:    lipgloss.Color("4"),
		Accent2:   lipgloss.Color("6"),
	}

	lines := strings.Split(ViewGroupOptions(groups, 1, st), "\n")
	if len(lines) != len(groups) {
		t.Fatalf("view = %d lines, want %d: %q", len(lines), len(groups), lines)
	}
	if !strings.Contains(lines[1], "beta") || !strings.Contains(lines[2], "shell") {
		t.Fatalf("rows do not name their groups: %q", lines)
	}
	if !strings.Contains(lines[0], "root") {
		t.Fatalf("the empty path is the root row: %q", lines[0])
	}
}
