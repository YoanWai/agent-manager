package geometry

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/keybind"
	tea "github.com/charmbracelet/bubbletea"
)

// fakeRoot is the root as the gesture reaches it, holding the state the
// drag moves so the surface can be driven with no Model.
type fakeRoot struct {
	gesture Gesture
	nudges  []int
	exits   []bool
	quit    bool
}

func (r *fakeRoot) Gesture() Gesture { return r.gesture }

func (r *fakeRoot) SetGesture(g Gesture) { r.gesture = g }

func (r *fakeRoot) Exit(commit bool) { r.exits = append(r.exits, commit) }

func (r *fakeRoot) Nudge(delta int) { r.nudges = append(r.nudges, delta) }

func (r *fakeRoot) Quit() tea.Cmd {
	r.quit = true
	return nil
}

func (r *fakeRoot) TakeForeground() {}

func (r *fakeRoot) ListMouse(tea.MouseMsg) tea.Cmd { return nil }

var _ Root = (*fakeRoot)(nil)

func key(name string) tea.KeyMsg {
	switch name {
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
}

func armed() *fakeRoot {
	return &fakeRoot{gesture: Gesture{Ratio: 0.34, RatioBefore: 0.34, ResizeMode: true}}
}

// The arrows nudge, enter and esc end the gesture, and only enter commits.
func TestArmedGestureNudgesThenEnds(t *testing.T) {
	for name, want := range map[string]bool{"enter": true, "esc": false} {
		root := armed()
		s := NewSurface(root, keybind.DefaultList())
		s.Key(nil, key("right"))
		if len(root.nudges) != 1 || root.nudges[0] != 1 {
			t.Fatalf("%s: nudges = %v, want one to the right", name, root.nudges)
		}
		handled, _ := s.Key(nil, key(name))
		if !handled {
			t.Fatalf("%s: the gesture let the key fall through", name)
		}
		if len(root.exits) != 1 || root.exits[0] != want {
			t.Fatalf("%s: exits = %v, want one commit=%t", name, root.exits, want)
		}
	}
}

// A drag the mouse armed alone holds no keyboard: the first key ends it and
// still reaches the layer below, and esc cancels it in place.
func TestDragWithoutResizeModeEndsOnAnyKey(t *testing.T) {
	root := &fakeRoot{gesture: Gesture{Ratio: 0.34, RatioBefore: 0.34, Dragging: true, Moved: true}}
	s := NewSurface(root, keybind.DefaultList())
	if handled, _ := s.Key(nil, key("j")); handled {
		t.Fatal("the drag swallowed a key it does not use")
	}
	if len(root.exits) != 1 || !root.exits[0] {
		t.Fatalf("exits = %v, want the moved drag committed", root.exits)
	}
	if handled, _ := s.Key(nil, key("esc")); !handled {
		t.Fatal("esc should be the gesture's own")
	}
	if len(root.exits) != 2 || root.exits[1] {
		t.Fatalf("exits = %v, want esc to cancel", root.exits)
	}
}

// Quit is the gesture's while it is armed, and reaches the root's quit.
func TestQuitReachesTheRoot(t *testing.T) {
	root := armed()
	s := NewSurface(root, keybind.DefaultList())
	if handled, _ := s.Key(nil, key("q")); !handled || !root.quit {
		t.Fatalf("handled=%t quit=%t, want the gesture to hand q to the root", handled, root.quit)
	}
}

// Leave puts back the ratio nobody committed and clears the gesture.
func TestLeaveRestoresTheUncommittedRatio(t *testing.T) {
	root := armed()
	root.gesture.Ratio = 0.5
	root.gesture.Dragging = true
	root.gesture.Moved = true
	NewSurface(root, keybind.DefaultList()).Leave(nil)
	if root.gesture.Ratio != 0.34 {
		t.Fatalf("ratio = %v, want the pre-gesture 0.34", root.gesture.Ratio)
	}
	if root.gesture.ResizeMode || root.gesture.Dragging || root.gesture.Moved {
		t.Fatalf("gesture left armed: %+v", root.gesture)
	}
}
