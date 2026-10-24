package focus

import (
	"github.com/YoanWai/agent-manager/internal/ui/surface"
	tea "github.com/charmbracelet/bubbletea"
)

// Surface is a focused session: the list and its live preview stay on
// screen and the keyboard goes to the agent's pane. It paints nothing of its
// own; the frame draws the pane's box while it is mounted. The saved
// prompts list stacks over it, and leaving focus pops both.
type Surface struct {
	root Root
}

// Root is what focus reaches on the root beyond surface.Host: the keys it
// forwards, the mouse over the pane, and the completion fence.
type Root interface {
	// Key forwards msg to the agent, or acts on it when it is the
	// manager's: leaving focus, the editor or Review.
	Key(msg tea.KeyMsg) tea.Cmd
	// Mouse runs a mouse event on the pane, and on the rail under it.
	Mouse(msg tea.MouseMsg) tea.Cmd
	// TakeForeground marks the key as the user moving on, which fences a
	// completion started before it.
	TakeForeground()
}

func NewSurface(root Root) *Surface { return &Surface{root: root} }

func (s *Surface) Key(h surface.Host, msg tea.KeyMsg) (bool, tea.Cmd) {
	s.root.TakeForeground()
	return true, s.root.Key(msg)
}

// Mouse is the pane's, except a press on the rail, which leaves.
func (s *Surface) Mouse(_ surface.Host, msg tea.MouseMsg) tea.Cmd {
	return s.root.Mouse(msg)
}

func (s *Surface) Click(surface.Host, int) tea.Cmd { return nil }

func (s *Surface) Wheel(surface.Host, tea.KeyMsg) tea.Cmd { return nil }

func (s *Surface) View(_ surface.Host, below string) string { return below }

// Footer is empty: the focused legend is built from the session it reads.
func (s *Surface) Footer() [][2]string { return nil }

func (s *Surface) Owner() surface.Owner { return surface.Agent }

// Dismissible: ctrl+c over the agent's pane is the agent's, and esc too.
func (s *Surface) Dismissible() bool { return false }

func (s *Surface) Leave(surface.Host) {}
