package ui

import (
	uiquick "github.com/YoanWai/agent-manager/internal/ui/quick"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	tea "github.com/charmbracelet/bubbletea"
)

// quickRoot is the root as the bar's surface reaches it: the key the bar
// types into the list, and the mouse the list keeps under it.
type quickRoot struct{ *Model }

func (r quickRoot) Key(msg tea.KeyMsg) (tea.Cmd, bool) { return r.quickKey(msg) }

func (r quickRoot) Mouse(msg tea.MouseMsg) tea.Cmd {
	_, cmd := r.listMouse(msg)
	return cmd
}

var _ uiquick.SurfaceRoot = quickRoot{}

// newQuickSurface builds a bar behind the root; the caller fills in its
// Bar, prompt, aim and origin before putting it on the stack.
func newQuickSurface(m *Model) *uiquick.Surface {
	return uiquick.NewSurface(quickRoot{m})
}

// quick is the open quick bar, nil when none is on the stack.
func (m *Model) quick() *uiquick.Surface {
	q, _ := surfaceOf[*uiquick.Surface](m)
	return q
}

// quickFor is the open bar when it is the one composerGen came from: a
// completion lands on the bar that asked or is dropped.
func (m *Model) quickFor(composerGen int) (*uiquick.Surface, bool) {
	q := m.quick()
	return q, q != nil && q.Gen == composerGen
}

// quickHolds is whether the bar composerGen came from still shows draft
// and no image: a send or spawn of exactly that prompt may clear it.
func (m *Model) quickHolds(composerGen int, draft string) bool {
	q, ok := m.quickFor(composerGen)
	return ok && q.Input.Value() == draft && len(q.Attachments) == 0
}

// closeQuick takes the bar off the stack wherever it sits: a send can
// complete under a card opened since.
func (m *Model) closeQuick() {
	for i := len(m.surfaces) - 1; i >= 0; i-- {
		if q, ok := m.surfaces[i].(*uiquick.Surface); ok {
			m.surfaces = append(m.surfaces[:i:i], m.surfaces[i+1:]...)
			q.Leave(m)
			return
		}
	}
}

// quickAim and railSelection convert the bar's aim: it keeps only the
// fields that tell a row it was pointed at from the one a refresh moved the
// cursor to, so quick never names the rail's row type.
func quickAim(selection uirail.Selection) uiquick.Aim {
	return uiquick.Aim{
		Kind:      uint8(selection.Kind),
		SessionID: selection.SessionID,
		Group:     selection.Group,
		Host:      selection.Host,
	}
}

func railSelection(aim uiquick.Aim) uirail.Selection {
	return uirail.Selection{
		Kind:      uirail.RowKind(aim.Kind),
		SessionID: aim.SessionID,
		Group:     aim.Group,
		Host:      aim.Host,
	}
}
