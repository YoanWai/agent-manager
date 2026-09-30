package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	"testing"
)

// Ctrl+C quits from the comment editor and the send-confirm prompt, not just
// the base review keymap.
func TestReviewCtrlCQuitsFromSubmodes(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "subquit", gitRepoWithTwoChangedFiles(t))
	m.openAnnotate()
	if _, cmd := m.handleDiffKey(tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil {
		t.Fatal("ctrl+c should quit while annotating")
	}
	m.diff.annotating = false
	m.diff.sendConfirm = true
	if _, cmd := m.handleDiffKey(tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil {
		t.Fatal("ctrl+c should quit from the send-confirm prompt")
	}
	m.diff.sendConfirm = false
	m.diff.repoRoots = []string{"/tmp/one", "/tmp/two"}
	m.openRepoPick()
	if _, cmd := m.handleRepoPickKey(tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil {
		t.Fatal("ctrl+c should quit while the repo picker is open")
	}
}

// Ctrl+C quits from review mode like it does from the list.
func TestReviewCtrlCQuits(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "quitter", gitRepoWithTwoChangedFiles(t))
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("ctrl+c in review should return a command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("ctrl+c in review should quit")
	}
}

func TestDiffHelpReturnsToReview(t *testing.T) {
	m := buildModel(t)
	dir := gitTestRepo(t)
	openReviewOn(t, m, "helper", dir)
	m.handleDiffKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if m.mode != modeHelp {
		t.Fatalf("? should open the key map, mode = %v", m.mode)
	}
	if !m.diff.active {
		t.Fatal("opening help must not close the review")
	}
	m.handleHelpKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != modeDiff || !m.diff.active {
		t.Fatalf("esc should return to review, mode = %v active = %v", m.mode, m.diff.active)
	}
}

func TestDiffHelpRestartsLoaderOnReturn(t *testing.T) {
	m := helpModel()
	m.mode = modeDiff
	m.diff = diffState{active: true, loading: true}
	if cmd := m.startStartupTick(); cmd == nil {
		t.Fatal("loading review should start the loader")
	}
	m.openHelp()
	m.Update(startupTickMsg{})
	if m.startup.startupAnimating {
		t.Fatal("loader should stop while help covers the review")
	}
	_, cmd := m.handleHelpKey(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil || !m.startup.startupAnimating {
		t.Fatal("returning to a loading review should restart the loader")
	}
}
