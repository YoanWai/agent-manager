package ui

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/keybind"
	tea "github.com/charmbracelet/bubbletea"
)

// resizeSurface holds the keyboard while the split's divider moves: after
// the resize key, whose arrows nudge it until enter or esc, or under a press
// on the divider, which any key ends where it stands. It paints nothing of
// its own; the seam and the status line read the split's gesture state,
// which stays on the root for the mouse.
type resizeSurface struct {
	keys keybind.Table
}

// resizeHost is what the gesture reaches on the root beyond the stack.
type resizeHost interface {
	splitGesture() *splitState
	exitResizeMode(commit bool) (tea.Model, tea.Cmd)
	nudgeSplit(delta int)
	quit() tea.Cmd
	takeForeground()
	listMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd)
}

func newResizeSurface(keys keybind.Table) *resizeSurface { return &resizeSurface{keys: keys} }

func (m *Model) splitGesture() *splitState { return &m.layout.split }

func (s *resizeSurface) Key(h surfaceHost, msg tea.KeyMsg) (bool, tea.Cmd) {
	host := h.(resizeHost)
	host.takeForeground()
	split := host.splitGesture()
	key := keybind.Normalize(msg.String())
	quit := s.keys.Binding(keybind.Quit).Has(key)
	// A drag armed by the mouse alone leaves the keyboard live, so a press
	// whose release never lands cannot strand the list: the next key ends
	// that drag where it stands, esc cancelling it exactly as the footer
	// says, and anything else then falls to the layer below.
	if !split.resizeMode {
		switch {
		case key == "esc":
			host.exitResizeMode(false)
			return true, nil
		case quit:
			return true, host.quit()
		}
		host.exitResizeMode(split.moved)
		return false, nil
	}
	switch {
	case key == "enter" || s.keys.Binding(keybind.Resize).Has(key):
		host.exitResizeMode(true)
	case key == "esc":
		host.exitResizeMode(false)
	case quit:
		return true, host.quit()
	case key == "left" || key == "h":
		host.nudgeSplit(-1)
	case key == "right" || key == "l":
		host.nudgeSplit(1)
	}
	return true, nil
}

// Mouse is the list's: the divider drags and the rest of the list holds
// still the way listMouse already keeps it during a resize.
func (s *resizeSurface) Mouse(h surfaceHost, msg tea.MouseMsg) tea.Cmd {
	_, cmd := h.(resizeHost).listMouse(msg)
	return cmd
}

func (s *resizeSurface) Click(surfaceHost, int) tea.Cmd { return nil }

func (s *resizeSurface) Wheel(surfaceHost, tea.KeyMsg) tea.Cmd { return nil }

func (s *resizeSurface) View(_ surfaceHost, below string) string { return below }

func (s *resizeSurface) Footer() [][2]string {
	commit := strings.TrimPrefix(s.keys.Binding(keybind.Resize).Glyph("/")+" / release", " / ")
	return [][2]string{{"←→", "nudge"}, {"drag", "divider"}, {commit, "commit"}, {"esc", "cancel"}}
}

func (s *resizeSurface) Owner() inputOwner { return ownerManager }

// Dismissible: esc cancels through Key, and a click is the drag's own.
func (s *resizeSurface) Dismissible() bool { return false }

// Leave ends the gesture. A ratio nobody committed goes back to where it
// was before the gesture started.
func (s *resizeSurface) Leave(h surfaceHost) {
	split := h.(resizeHost).splitGesture()
	if split.resizeMode || split.dragging {
		split.ratio = split.ratioBefore
	}
	split.resizeMode, split.dragging, split.moved = false, false, false
}
