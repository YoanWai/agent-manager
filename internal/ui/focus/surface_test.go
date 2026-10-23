package focus

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	"github.com/YoanWai/agent-manager/internal/ui/surface"
	tea "github.com/charmbracelet/bubbletea"
)

// stubRoot is a focused session's root, and the saved list's: it records
// what a surface asked for instead of forwarding to an agent.
type stubRoot struct {
	keys   []string
	mouse  int
	fence  int
	pasted []spawn.SavedPick
	left   int
}

func (r *stubRoot) Key(msg tea.KeyMsg) tea.Cmd {
	r.keys = append(r.keys, msg.String())
	return nil
}

func (r *stubRoot) Mouse(tea.MouseMsg) tea.Cmd { r.mouse++; return nil }
func (r *stubRoot) TakeForeground()            { r.fence++ }

func (r *stubRoot) Keys() keybind.Table { return keybind.DefaultSession() }

func (r *stubRoot) InnerWidth(width int) int { return width - 8 }

func (r *stubRoot) PasteFocused(pick spawn.SavedPick) tea.Cmd {
	r.pasted = append(r.pasted, pick)
	return nil
}

func (r *stubRoot) LeaveFocus() tea.Cmd { r.left++; return nil }

// stubHost is the stack as a surface reads it: a size and a pop.
type stubHost struct{ popped int }

func (h *stubHost) CardSized(int, string, string, [][2]string) string { return "" }
func (h *stubHost) Size() (int, int)                                  { return 100, 40 }
func (h *stubHost) Pop()                                              { h.popped++ }

func TestSurfaceHandsEveryKeyToTheAgent(t *testing.T) {
	root := &stubRoot{}
	s := NewSurface(root)
	host := &stubHost{}
	for _, key := range []tea.KeyMsg{{Type: tea.KeyRunes, Runes: []rune("j")}, {Type: tea.KeyEnter}} {
		handled, _ := s.Key(host, key)
		if !handled {
			t.Fatalf("%q fell past the focused pane", key.String())
		}
	}
	if len(root.keys) != 2 || root.fence != 2 {
		t.Fatalf("root saw keys %v and %d fences, want both keys fenced", root.keys, root.fence)
	}
	if s.Owner() != surface.Agent || s.Dismissible() {
		t.Fatal("focus must hand plain keys to the agent and stay put on a click")
	}
	if s.View(host, "frame") != "frame" {
		t.Fatal("focus paints nothing of its own")
	}
}

func TestSavedPastesAPickAndCloses(t *testing.T) {
	root := &stubRoot{}
	s := NewSaved(root, 4, spawn.Styles{})
	host := &stubHost{}
	s.Load([]spawn.Saved{{Name: "Role", Text: "be brief"}}, nil)

	handled, cmd := s.Key(host, tea.KeyMsg{Type: tea.KeyEnter})
	run(cmd)
	if !handled || host.popped != 1 || len(root.pasted) != 1 || !root.pasted[0].Picked || root.pasted[0].Saved.Text != "be brief" {
		t.Fatalf("picked %v, popped %d, handled %t", root.pasted, host.popped, handled)
	}
}

func TestSavedKeepsTheLeaveKeyAndItsOwnClose(t *testing.T) {
	root := &stubRoot{}
	s := NewSaved(root, 0, spawn.Styles{})
	host := &stubHost{}

	s.Key(host, tea.KeyMsg{Type: tea.KeyCtrlQ})
	if root.left != 1 {
		t.Fatalf("the leave key ran %d times, want 1", root.left)
	}
	s.Key(host, tea.KeyMsg{Type: tea.KeyF4})
	if host.popped != 1 {
		t.Fatalf("the saved prompt key popped %d times, want 1", host.popped)
	}
	if !s.Dismissible() || s.Owner() != surface.Manager {
		t.Fatal("the list is a dismissible card the manager reads")
	}
}

func TestSavedClickPicksTheRowUnderThePointer(t *testing.T) {
	root := &stubRoot{}
	s := NewSaved(root, 0, spawn.Styles{})
	host := &stubHost{}
	s.Load([]spawn.Saved{{Name: "Role", Text: "be brief"}}, nil)
	s.View(host, "")
	s.Click(host, 1)
	if host.popped != 1 || len(root.pasted) != 1 || root.pasted[0].Saved.Name != "Role" {
		t.Fatalf("popped %d, pasted %+v", host.popped, root.pasted)
	}
}

// run drains a batch, so a test sees what the surface asked for.
func run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, one := range batch {
			run(one)
		}
	}
}
