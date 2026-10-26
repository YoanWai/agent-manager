package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestQuickWorktreePresentationSurvivesExpiry(t *testing.T) {
	t.Parallel()
	m := quickGroupModel(t, t.TempDir())
	m.openQuickMode()
	dir := m.quickTargetDir()
	m.ledger.worktreeRepos = map[string]repoAnswer{
		dir: {capable: true, at: time.Now().Add(-24 * time.Hour)},
	}
	// An untouched bar inherits the group default rather than its raw choice.
	m.workspace.groupWorktrees = map[string]string{"grp": "on"}
	view := ansi.Strip(m.viewQuickBar(160, 6))
	if m.quick.WorktreeTouched() || !m.quickWorktreeOn() || !strings.Contains(view, m.glyphs().WorktreeGlyph+" on") {
		t.Fatal("expired repository lost the untouched bar's default-on state")
	}
	for _, on := range []bool{true, false} {
		m.quick.SetWorktree(on)
		view := ansi.Strip(m.viewQuickBar(160, 6))
		want := "off"
		if on {
			want = "on"
		}
		if m.quickWorktreeOn() != on || strings.Contains(view, "no repo") || !strings.Contains(view, m.glyphs().WorktreeGlyph+" "+want) {
			t.Fatalf("expired repository should show quick worktree %s: %q", want, view)
		}
	}
	m.quick.SetWorktree(true)
	m.ledger.worktreeRepos[dir] = repoAnswer{at: time.Now().Add(-24 * time.Hour)}
	if view := ansi.Strip(m.viewQuickBar(160, 6)); m.quickWorktreeOn() || !strings.Contains(view, "no repo") {
		t.Fatalf("expired negative should retain the quick bar's no-repo state: %q", view)
	}
}
