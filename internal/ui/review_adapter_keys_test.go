package ui

import (
	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	tea "github.com/charmbracelet/bubbletea"
	"testing"
)

// Ctrl+C quits from the comment editor and the send-confirm prompt, not just
// the base review keymap. The pickers are surfaces: review_surface_test.go
// shows ctrl+c closing the card and leaving Review as it was.
func TestReviewCtrlCQuitsFromSubmodes(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "subquit", gitRepoWithTwoChangedFiles(t))
	m.openAnnotate()
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil {
		t.Fatal("ctrl+c should quit while annotating")
	}
	m.review.AnnotationKey(tea.KeyMsg{Type: tea.KeyEsc})
	m.review.SetSendConfirm(true)
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil {
		t.Fatal("ctrl+c should quit from the send-confirm prompt")
	}
}

// Ctrl+C quits from review mode like it does from the list.
func TestReviewCtrlCQuits(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	m := buildModel(t)
	dir := gitTestRepo(t)
	openReviewOn(t, m, "helper", dir)
	m.handleDiffKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if !helpOpen(m) {
		t.Fatalf("? should open the key map, screen = %v", m.screenID())
	}
	if !m.review.Snapshot().Active {
		t.Fatal("opening help must not close the review")
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.base != baseReview || !m.review.Snapshot().Active {
		t.Fatalf("esc should return to review, screen = %v active = %v", m.screenID(), m.review.Snapshot().Active)
	}
}

func TestDiffHelpKeepsLoaderRunningOnReturn(t *testing.T) {
	m := helpModel()
	m.clearSurfaces()
	m.base = baseReview
	seedReviewForTest(m, uireview.Target{ID: "review"}, git.ScopeUncommitted, "/repo", diff.Set{}, false)
	if cmd := m.startStartupTick(); cmd == nil {
		t.Fatal("loading review should start the loader")
	}
	m.openHelp()
	m.Update(startupTickMsg{})
	if !m.startup.startupAnimating {
		t.Fatal("review is still loading under help, so the loader keeps ticking")
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if !m.startup.startupAnimating {
		t.Fatal("returning to a loading review should leave the loader running")
	}
}
