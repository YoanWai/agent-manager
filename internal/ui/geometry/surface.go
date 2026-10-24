// Package geometry holds the split's resize surface: the keyboard grab the
// divider takes while it moves.
package geometry

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/ui/surface"
	tea "github.com/charmbracelet/bubbletea"
)

// Gesture is the split's divider gesture as the surface reads and moves it.
// The root holds it, because the mouse drags the same divider.
type Gesture struct {
	// Ratio is the left panel's share of the width, as the drag left it.
	Ratio       float64
	RatioBefore float64
	// ResizeMode arms the arrow keys; a drag can arm without it.
	ResizeMode bool
	Dragging   bool
	// Moved separates a drag from a plain click on the seam, which commits
	// nothing.
	Moved bool
}

// Surface holds the keyboard while the split's divider moves: after the
// resize key, whose arrows nudge it until enter or esc, or under a press on
// the divider, which any key ends where it stands. It paints nothing of its
// own; the seam and the status line read the gesture state the root holds.
type Surface struct {
	keys keybind.Table
	root Root
}

// Root is what the gesture reaches on the root beyond surface.Host.
type Root interface {
	// Gesture is the divider gesture as it stands.
	Gesture() Gesture
	// SetGesture stores it back.
	SetGesture(Gesture)
	// Exit ends the gesture, committing the ratio or putting back the one
	// it started from.
	Exit(commit bool)
	// Nudge moves the divider by delta columns.
	Nudge(delta int)
	Quit() tea.Cmd
	TakeForeground()
	// ListMouse runs the mouse on the list, which the drag is part of.
	ListMouse(msg tea.MouseMsg) tea.Cmd
}

// NewSurface opens the gesture with the key table that names the resize,
// quit and foreground bindings.
func NewSurface(root Root, keys keybind.Table) *Surface {
	return &Surface{keys: keys, root: root}
}

func (s *Surface) Key(_ surface.Host, msg tea.KeyMsg) (bool, tea.Cmd) {
	s.root.TakeForeground()
	gesture := s.root.Gesture()
	key := keybind.Normalize(msg.String())
	quit := s.keys.Binding(keybind.Quit).Has(key)
	// A drag armed by the mouse alone leaves the keyboard live, so a press
	// whose release never lands cannot strand the list: the next key ends
	// that drag where it stands, esc cancelling it exactly as the footer
	// says, and anything else then falls to the layer below.
	if !gesture.ResizeMode {
		switch {
		case key == "esc":
			s.root.Exit(false)
			return true, nil
		case quit:
			return true, s.root.Quit()
		}
		s.root.Exit(gesture.Moved)
		return false, nil
	}
	switch {
	case key == "enter" || s.keys.Binding(keybind.Resize).Has(key):
		s.root.Exit(true)
	case key == "esc":
		s.root.Exit(false)
	case quit:
		return true, s.root.Quit()
	case key == "left" || key == "h":
		s.root.Nudge(-1)
	case key == "right" || key == "l":
		s.root.Nudge(1)
	}
	return true, nil
}

// Mouse is the list's: the divider drags and the rest of the list holds
// still the way the list's own mouse keeps it during a resize.
func (s *Surface) Mouse(_ surface.Host, msg tea.MouseMsg) tea.Cmd { return s.root.ListMouse(msg) }

func (s *Surface) Click(surface.Host, int) tea.Cmd { return nil }

func (s *Surface) Wheel(surface.Host, tea.KeyMsg) tea.Cmd { return nil }

func (s *Surface) View(_ surface.Host, below string) string { return below }

func (s *Surface) Footer() [][2]string {
	commit := strings.TrimPrefix(s.keys.Binding(keybind.Resize).Glyph("/")+" / release", " / ")
	return [][2]string{{"←→", "nudge"}, {"drag", "divider"}, {commit, "commit"}, {"esc", "cancel"}}
}

func (s *Surface) Owner() surface.Owner { return surface.Manager }

// Dismissible: esc cancels through Key, and a click is the drag's own.
func (s *Surface) Dismissible() bool { return false }

// Leave ends the gesture. A ratio nobody committed goes back to where it
// was before the gesture started.
func (s *Surface) Leave(surface.Host) {
	gesture := s.root.Gesture()
	if gesture.ResizeMode || gesture.Dragging {
		gesture.Ratio = gesture.RatioBefore
	}
	gesture.ResizeMode, gesture.Dragging, gesture.Moved = false, false, false
	s.root.SetGesture(gesture)
}
