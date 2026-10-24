package ui

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	uihelp "github.com/YoanWai/agent-manager/internal/ui/help"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	uisurface "github.com/YoanWai/agent-manager/internal/ui/surface"
	tea "github.com/charmbracelet/bubbletea"
)

func helpOverList(t *testing.T) *Model {
	t.Helper()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	// The real trigger: the help key on the list.
	m.Update(runeKey("?"))
	if !helpOpen(m) {
		t.Fatal("? did not open the key map")
	}
	return m
}

func TestHelpSurfaceOpensOnTheListKey(t *testing.T) {
	m := helpOverList(t)
	if !m.onList() {
		t.Fatalf("help changed the base screen to %v", m.screenID())
	}
	if got := helpSurfaceOf(m).State().Content(m.helpContext(84)).Title; got == "? Review keys" {
		t.Fatalf("list help opened with review scope: %q", got)
	}
}

func TestHelpSurfaceClosesOnEscAndBackdrop(t *testing.T) {
	if !(&uihelp.Surface{}).Dismissible() {
		t.Fatal("help must be dismissible")
	}
	for name, close := range map[string]func(*Model) *Model{
		"esc": func(m *Model) *Model { m.Update(namedKey(tea.KeyEsc)); return m },
		"click off the card": func(m *Model) *Model {
			preparedView(m)
			return leftClick(m, 0, 0)
		},
	} {
		t.Run(name, func(t *testing.T) {
			m := close(helpOverList(t))
			if helpOpen(m) || m.effects.Quitting {
				t.Fatalf("help open = %t, quitting = %t", helpOpen(m), m.effects.Quitting)
			}
		})
	}
}

// Closing help over Review is a pop: Review stays the mode underneath, and
// its loader is the poller's to restart rather than a close side effect.
func TestHelpSurfaceOverReviewScopesToReviewAndPopsBack(t *testing.T) {
	m := helpModel()
	m.clearSurfaces()
	m.base = baseReview
	seedReviewForTest(m, uireview.Target{ID: "review"}, git.ScopeUncommitted, "/repo", diff.Set{}, false)
	m.openHelp()
	if got := helpSurfaceOf(m).State().Content(m.helpContext(84)).Title; got != "? Review keys" {
		t.Fatalf("help over review has title %q", got)
	}
	m.handleKey(namedKey(tea.KeyEsc))
	if helpOpen(m) || m.base != baseReview {
		t.Fatalf("esc left help open = %t, screen = %v", helpOpen(m), m.screenID())
	}
}

func TestHelpSurfaceHoldsTheScopeItWasOpenedWith(t *testing.T) {
	s := uihelp.NewSurface(helpRoot{helpModel()}, uihelp.Review)
	if !s.Dismissible() || s.Owner() != uisurface.Manager {
		t.Fatal("help must be a dismissible manager-owned card")
	}
}
