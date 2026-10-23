package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

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
