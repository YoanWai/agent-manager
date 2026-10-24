package ui

import (
	"path/filepath"
	"testing"

	"github.com/YoanWai/agent-manager/internal/execution"
	"github.com/YoanWai/agent-manager/internal/keybind"
	uigeometry "github.com/YoanWai/agent-manager/internal/ui/geometry"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestBodyYRangeMatchesListChrome(t *testing.T) {
	m := &Model{layout: layoutState{width: 120, height: 40, split: splitState{ratio: uigeometry.DefaultSplitRatio}}, services: services{listKeys: keybind.DefaultList()}}
	start, end := m.bodyYRange()
	if start != m.listChromeRows() {
		t.Fatalf("start = %d want listChromeRows=%d", start, m.listChromeRows())
	}
	// No transient status is showing, so its row is not reserved.
	wantH := m.layout.height - m.listChromeRows() - 1 - lipgloss.Height(m.viewFooter())
	if wantH < 3 {
		wantH = 3
	}
	if m.listBodyHeight() != wantH {
		t.Fatalf("listBodyHeight = %d want %d", m.listBodyHeight(), wantH)
	}
	if end != m.listChromeRows()+wantH {
		t.Fatalf("end = %d want %d", end, m.listChromeRows()+wantH)
	}
}

func resizeOpen(m *Model) bool {
	_, ok := surfaceOf[*uigeometry.Surface](m)
	return ok
}

// resizeModel drives the gesture through Update, which syncs the poller's
// input on every message, so the fixture carries one over a scratch store.
func resizeModel(t *testing.T) *Model {
	t.Helper()
	st := openTestStore(t, filepath.Join(t.TempDir(), "state.db"))
	t.Cleanup(func() { st.Close() })
	return &Model{
		layout:   layoutState{split: splitState{ratio: 0.34}, width: 100, height: 40},
		poller:   &poller{runner: execution.New(execution.Dependencies{Store: st}, execution.Options{})},
		services: services{store: st, listKeys: keybind.DefaultList()},
	}
}

// The resize key pushes the gesture, esc pops it with the ratio put back,
// and a stray click keeps it: the surface holds a gesture, not a card.
func TestResizeIsASurface(t *testing.T) {
	m := resizeModel(t)
	m.Update(key("|"))
	if !resizeOpen(m) || !m.layout.split.resizeMode {
		t.Fatal("the resize key did not push the gesture")
	}
	if mustTop(t, m).Dismissible() {
		t.Fatal("resize must not close on a click off a card it does not paint")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if resizeOpen(m) || m.layout.split.resizeMode || m.layout.split.ratio != 0.34 {
		t.Fatalf("esc left open=%t resize=%t ratio=%v", resizeOpen(m), m.layout.split.resizeMode, m.layout.split.ratio)
	}
}

// A press on the divider pushes the gesture without the keyboard, and the
// next key ends the drag and still reaches the list below.
func TestDividerDragIsASurfaceAKeyEnds(t *testing.T) {
	m := resizeModel(t)
	y0, _ := m.bodyYRange()
	m.handleMouse(tea.MouseMsg{X: m.dividerX(), Y: y0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if !resizeOpen(m) || m.layout.split.resizeMode {
		t.Fatalf("divider press: open=%t resize=%t, want the gesture without the keyboard", resizeOpen(m), m.layout.split.resizeMode)
	}
	if m.cardOpen() {
		t.Fatal("the drag counts as a card, so the mouse would skip the divider")
	}
	m.Update(key("j"))
	if resizeOpen(m) || m.layout.split.dragging {
		t.Fatal("a key did not end the drag")
	}
}
