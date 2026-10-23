package ui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// focusSurface is a focused session: the list and its live preview stay on
// screen and the keyboard goes to the agent's pane. It paints nothing of its
// own; the frame draws the pane's box while it is mounted. The saved
// prompts list stacks over it, and leaving focus pops both.
type focusSurface struct{}

// focusHost is what focus reaches on the root beyond the stack.
type focusHost interface {
	handleFocusKey(msg tea.KeyMsg) (tea.Model, tea.Cmd)
	focusMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd)
	takeForeground()
}

// focused is whether a session is focused, with or without a card over it.
func (m *Model) focused() bool {
	_, ok := surfaceOf[*focusSurface](m)
	return ok
}

func (s *focusSurface) Key(h surfaceHost, msg tea.KeyMsg) (bool, tea.Cmd) {
	host := h.(focusHost)
	host.takeForeground()
	_, cmd := host.handleFocusKey(msg)
	return true, cmd
}

// Mouse is the pane's, except a press on the rail, which leaves.
func (s *focusSurface) Mouse(h surfaceHost, msg tea.MouseMsg) tea.Cmd {
	_, cmd := h.(focusHost).focusMouse(msg)
	return cmd
}

func (s *focusSurface) Click(surfaceHost, int) tea.Cmd { return nil }

func (s *focusSurface) Wheel(surfaceHost, tea.KeyMsg) tea.Cmd { return nil }

func (s *focusSurface) View(_ surfaceHost, below string) string { return below }

// Footer is empty: the focused legend is built from the session it reads.
func (s *focusSurface) Footer() [][2]string { return nil }

func (s *focusSurface) Owner() inputOwner { return ownerAgent }

// Dismissible: ctrl+c over the agent's pane is the agent's, and esc too.
func (s *focusSurface) Dismissible() bool { return false }

func (s *focusSurface) Leave(surfaceHost) {}
