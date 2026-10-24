package ui

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/keybind"
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
	if got := helpSurfaceOf(m).State().Content(uihelp.ContextFor(helpHost{m}, 84)).Title; got == "? Review keys" {
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
	if got := helpSurfaceOf(m).State().Content(uihelp.ContextFor(helpHost{m}, 84)).Title; got != "? Review keys" {
		t.Fatalf("help over review has title %q", got)
	}
	m.handleKey(namedKey(tea.KeyEsc))
	if helpOpen(m) || m.base != baseReview {
		t.Fatalf("esc left help open = %t, screen = %v", helpOpen(m), m.screenID())
	}
}

func TestHelpSurfaceHoldsTheScopeItWasOpenedWith(t *testing.T) {
	s := uihelp.NewSurface(helpHost{helpModel()}, uihelp.Review)
	if !s.Dismissible() || s.Owner() != uisurface.Manager {
		t.Fatal("help must be a dismissible manager-owned card")
	}
}

func TestWheelScrollsHelp(t *testing.T) {
	m := helpModel()
	preparedView(m)
	if m = wheel(m, tea.MouseButtonWheelDown, 60, 15); helpLayoutOf(m).Content.Offset != 1 {
		t.Fatalf("a wheel notch down should scroll one row, offset %d", helpLayoutOf(m).Content.Offset)
	}
	if m = wheel(m, tea.MouseButtonWheelUp, 60, 15); helpLayoutOf(m).Content.Offset != 0 {
		t.Fatalf("a wheel notch up should scroll back, offset %d", helpLayoutOf(m).Content.Offset)
	}
	if m.layout.card.holds(1, 15) {
		t.Fatalf("test setup: the card covers the left edge: %+v", m.layout.card)
	}
	if m = wheel(m, tea.MouseButtonWheelDown, 1, 15); helpLayoutOf(m).Content.Offset != 0 || !helpOpen(m) {
		t.Fatalf("a wheel off the card should do nothing, offset %d mode %v", helpLayoutOf(m).Content.Offset, m.screenID())
	}
}

func TestClickOnHelpsSearchLineResumesTheSearch(t *testing.T) {
	m := helpModel()
	for _, msg := range []tea.KeyMsg{runeKey("/"), runeKey("fork"), namedKey(tea.KeyEnter)} {
		m.handleKey(msg)
	}
	x, y := paintedCell(t, m, "search fork")
	if m = leftClick(m, x, y); !helpSurfaceOf(m).State().Searching() || !helpOpen(m) {
		t.Fatalf("a click on the search line should resume typing, searching %v mode %v", helpSurfaceOf(m).State().Searching(), m.screenID())
	}
}

func runeKey(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func namedKey(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

func helpModel() *Model {
	m := &Model{
		layout: layoutState{
			width:  120,
			height: 30,
		},
		services: services{
			keys:     keybind.DefaultSession(),
			listKeys: keybind.DefaultList(),
		},
		prefs: preferences{
			arrowStep: true,
		},
	}
	m.openHelp()
	return m
}

// helpSurfaceOf is the key map on the stack, which a test reads state from.
func helpSurfaceOf(m *Model) *uihelp.Surface {
	s, ok := surfaceOf[*uihelp.Surface](m)
	if !ok {
		panic("the key map is not open")
	}
	return s
}

func helpLayoutOf(m *Model) uihelp.Card { return helpSurfaceOf(m).Card(m) }

func viewHelpOf(m *Model) string { return helpSurfaceOf(m).View(m, "") }

func helpOpen(m *Model) bool {
	_, ok := surfaceOf[*uihelp.Surface](m)
	return ok
}
