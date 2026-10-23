package ui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// quickSurface is the quick bar over the list. It is not a card: the bar
// paints into the list's footer and the rail stays live under it, so the
// list keeps the mouse and the cursor the bar aims at. It holds a draft, so
// a stray click keeps it and only esc, a send that closes it, or a screen
// that takes the list's place drops it.
type quickSurface struct {
	quickBar
}

// quickHost is what the bar reaches on the root beyond the stack.
type quickHost interface {
	quickKey(msg tea.KeyMsg) (tea.Cmd, bool)
	listMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd)
}

func newQuickSurface(bar quickBar) *quickSurface { return &quickSurface{quickBar: bar} }

// quick is the open quick bar, nil when none is on the stack.
func (m *Model) quick() *quickSurface {
	q, _ := surfaceOf[*quickSurface](m)
	return q
}

// quickFor is the open bar when it is the one composerGen came from: a
// completion lands on the bar that asked or is dropped.
func (m *Model) quickFor(composerGen int) (*quickSurface, bool) {
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
		if q, ok := m.surfaces[i].(*quickSurface); ok {
			m.surfaces = append(m.surfaces[:i:i], m.surfaces[i+1:]...)
			q.Leave(m)
			return
		}
	}
}

func (s *quickSurface) Key(h surfaceHost, msg tea.KeyMsg) (bool, tea.Cmd) {
	cmd, handled := h.(quickHost).quickKey(msg)
	return handled, cmd
}

// Mouse is the list's: the rail moves the bar's target, the divider drags,
// and the list routes a press on the bar back here.
func (s *quickSurface) Mouse(h surfaceHost, msg tea.MouseMsg) tea.Cmd {
	_, cmd := h.(quickHost).listMouse(msg)
	return cmd
}

func (s *quickSurface) Click(surfaceHost, int) tea.Cmd { return nil }

func (s *quickSurface) Wheel(surfaceHost, tea.KeyMsg) tea.Cmd { return nil }

// View leaves the frame as it is: the list footer paints the bar.
func (s *quickSurface) View(_ surfaceHost, below string) string { return below }

// Footer is empty: the bar is the footer, painted by quickFooter.
func (s *quickSurface) Footer() [][2]string { return nil }

func (s *quickSurface) Owner() inputOwner { return ownerManager }

// Dismissible: the prompt is a draft, so a stray click keeps it.
func (s *quickSurface) Dismissible() bool { return false }

// Leave lets go of the images the prompt held; a send has already taken
// the ones it carries.
func (s *quickSurface) Leave(surfaceHost) { s.Composer.Release() }
