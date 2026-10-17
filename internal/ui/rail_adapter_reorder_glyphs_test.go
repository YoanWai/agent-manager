package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestNamesContainingTheHandleStillSelectAndDrag(t *testing.T) {
	m := buildModel(t)
	name := "界e\u0301 " + m.glyphs().ReorderGrip + " notes"
	createSession(t, m, name, t.TempDir(), "")
	createSession(t, m, "other", t.TempDir(), "")
	m.View()
	line := paintedRailLines(t, m, name)[0]
	y0, _ := m.bodyYRange()
	frame := strings.Split(ansi.Strip(m.View()), "\n")
	head := frame[y0+line]
	nameAt := strings.Index(head, name)
	if nameAt < 0 {
		t.Fatalf("row lost its Unicode name: %q", head)
	}
	x := ansi.StringWidth(head[:nameAt]) + ansi.StringWidth("界e\u0301 ")
	m.handleMouse(tea.MouseMsg{X: x, Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if m.rail.Reordering() {
		t.Fatal("clicking punctuation in the name lifted the row")
	}
	m.handleMouse(tea.MouseMsg{X: x, Y: y0 + line, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	m = liftByHandle(t, m, name)
	if !m.rail.Reordering() || railSelectedSession(m).Name != name {
		t.Fatal("the real handle did not lift the named row")
	}
}
