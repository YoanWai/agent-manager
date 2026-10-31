package dialogs

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	tea "github.com/charmbracelet/bubbletea"
)

func testMoveSurface(root MoveRoot, groups ...string) *MoveSurface {
	options := make([]spawn.GroupOption, len(groups))
	for i, path := range groups {
		options[i] = spawn.GroupOption{Path: path}
	}
	return NewMoveSurface(root, MoveSession("shell"), spawn.Picker{Groups: options})
}

// A click on a target picks it, and a click on the target under the cursor
// places the row, the way ↵ does.
func TestMoveSurfaceClickPicksThenPlaces(t *testing.T) {
	root := &moveStub{}
	host := &stubHost{}
	s := testMoveSurface(root, "", "target")
	if cmd := s.Click(host, 1); cmd != nil || len(root.placed) != 0 {
		t.Fatal("a click on another target placed the row")
	}
	if got := s.Picker().GroupPath(); got != "target" {
		t.Fatalf("picked %q, want the clicked target", got)
	}
	s.Click(host, 1)
	if len(root.placed) != 1 || root.groups[0] != "target" {
		t.Fatalf("the second click placed %+v", root.groups)
	}
	if s.Click(host, len(s.Picker().Groups)) != nil {
		t.Fatal("a click past the last target placed the row")
	}
}

// The wheel steps the picker the way the arrows do, and the card paints
// through its root.
func TestMoveSurfaceWheelStepsAndPaints(t *testing.T) {
	root := &moveStub{}
	host := &stubHost{}
	s := testMoveSurface(root, "", "alpha")
	s.Wheel(host, tea.KeyMsg{Type: tea.KeyDown})
	if got := s.Picker().GroupPath(); got != "alpha" {
		t.Fatalf("the wheel picked %q, want the next target", got)
	}
	s.View(host, "")
	if !root.painted {
		t.Fatal("the card does not paint through its root")
	}
}

// A session move is dismissible: it holds no draft.
func TestMoveSurfaceIsDismissible(t *testing.T) {
	root := &moveStub{}
	host := &stubHost{}
	s := testMoveSurface(root, "", "alpha")
	if !s.Dismissible() {
		t.Fatal("the move card holds no draft and must be dismissible")
	}
	s.Key(host, tea.KeyMsg{Type: tea.KeyEsc})
	if host.popped != 1 || len(root.placed) != 0 {
		t.Fatalf("esc popped %d times and placed %d rows", host.popped, len(root.placed))
	}
	if got := s.Footer(); len(got) != len(MoveHints()) {
		t.Fatalf("footer = %v, want the card's hints", got)
	}
}
