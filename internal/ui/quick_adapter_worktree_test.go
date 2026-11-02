package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	uilaunch "github.com/YoanWai/agent-manager/internal/ui/spawn/launch"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestQuickWorktreePresentationSurvivesExpiry(t *testing.T) {
	t.Parallel()
	m := quickGroupModel(t, t.TempDir())
	m.openQuickMode()
	dir := m.quickTargetDir()
	m.ledger.spawner.WorktreeRepos = map[string]uilaunch.RepoAnswer{
		dir: {Capable: true, At: time.Now().Add(-24 * time.Hour)},
	}
	// An untouched bar inherits the group default rather than its raw choice.
	m.workspace.groupWorktrees = map[string]string{"grp": "on"}
	view := ansi.Strip(m.viewQuickBar(160, 6))
	if m.quick().WorktreeTouched() || !m.quickWorktreeOn() || !strings.Contains(view, m.glyphs().WorktreeGlyph+" on") {
		t.Fatal("expired repository lost the untouched bar's default-on state")
	}
	for _, on := range []bool{true, false} {
		m.quick().SetWorktree(on)
		view := ansi.Strip(m.viewQuickBar(160, 6))
		want := "off"
		if on {
			want = "on"
		}
		if m.quickWorktreeOn() != on || strings.Contains(view, "no repo") || !strings.Contains(view, m.glyphs().WorktreeGlyph+" "+want) {
			t.Fatalf("expired repository should show quick worktree %s: %q", want, view)
		}
	}
	m.quick().SetWorktree(true)
	m.ledger.spawner.WorktreeRepos[dir] = uilaunch.RepoAnswer{At: time.Now().Add(-24 * time.Hour)}
	if view := ansi.Strip(m.viewQuickBar(160, 6)); m.quickWorktreeOn() || !strings.Contains(view, "no repo") {
		t.Fatalf("expired negative should retain the quick bar's no-repo state: %q", view)
	}
}

func TestQuickToggleSeesARepoInitialisedWhileOpen(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir, seed := filepath.Join(root, "plain"), filepath.Join(root, "seed")
	for _, path := range []string{dir, seed} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// git init can outlast WorktreeLookupTTL on Windows, so the repo is built
	// before the answer is cached and only moved into place after.
	initGitRepo(t, seed)
	m := quickGroupModel(t, dir)
	if err := m.services.store.SetGroupWorktree("grp", "on"); err != nil {
		t.Fatalf("set worktree: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "grp")
	m.openQuickMode()
	m.applyTestMsg(t, m.quickWorktreeProbeCmd(false)())
	ageWorktreeAnswer(m, m.quickTargetDir())
	if view := ansi.Strip(m.viewQuickBar(160, 6)); !strings.Contains(view, "no repo") {
		t.Fatalf("a plain directory should read no repo after its answer ages: %q", view)
	}
	applyQuickKeyCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlT})
	if m.quickWorktreeOn() || !strings.Contains(m.errBar.text, "need a git repository") {
		t.Fatalf("a plain directory took the toggle: on %t error %q", m.quickWorktreeOn(), m.errBar.text)
	}

	if err := os.Rename(filepath.Join(seed, ".git"), filepath.Join(dir, ".git")); err != nil {
		t.Fatal(err)
	}
	ageWorktreeAnswer(m, m.quickTargetDir())
	applyQuickKeyCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlT})
	if view := ansi.Strip(m.viewQuickBar(160, 6)); !m.quickWorktreeOn() || !strings.Contains(view, m.glyphs().WorktreeGlyph+" on") {
		t.Fatalf("a repo initialised while the bar was open should take the toggle: %q", view)
	}
}

func ageWorktreeAnswer(m *Model, dir string) {
	answer := m.ledger.spawner.WorktreeRepos[dir]
	answer.At = answer.At.Add(-uilaunch.WorktreeLookupTTL)
	m.ledger.spawner.WorktreeRepos[dir] = answer
}
