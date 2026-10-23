package ui

import (
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	tea "github.com/charmbracelet/bubbletea"
)

// moveSurface is the card that places a session or group. It holds the
// row being moved and its own group picker of targets, which the root
// shapes when the card opens.
type moveSurface struct {
	uidialogs.Move
	groupPicker
}

// moveHost is what the card reaches on the root beyond the stack.
type moveHost interface {
	pop()
	placeMove(move uidialogs.Move, p *groupPicker) tea.Cmd
	viewMove(p *groupPicker) string
}

func newMoveSurface(move uidialogs.Move, picker groupPicker) *moveSurface {
	return &moveSurface{Move: move, groupPicker: picker}
}

func (s *moveSurface) Key(h surfaceHost, msg tea.KeyMsg) (bool, tea.Cmd) {
	host := h.(moveHost)
	key := s.Move.Key(msg)
	switch key.Action {
	case uidialogs.MoveCancel:
		h.pop()
	case uidialogs.MoveCursor:
		s.step(key.Delta)
	case uidialogs.MoveSubmit:
		return true, host.placeMove(s.Move, &s.groupPicker)
	}
	return true, nil
}

// Click moves the picker's cursor to the target a click landed on, and
// moves the row there on a click on the target already under it.
func (s *moveSurface) Click(h surfaceHost, line int) tea.Cmd {
	switch {
	case line < 0 || line >= len(s.groups):
		return nil
	case line != s.groupIndex:
		s.step(line - s.groupIndex)
		return nil
	}
	return h.(moveHost).placeMove(s.Move, &s.groupPicker)
}

func (s *moveSurface) Wheel(h surfaceHost, arrow tea.KeyMsg) tea.Cmd {
	_, cmd := s.Key(h, arrow)
	return cmd
}

func (s *moveSurface) View(h surfaceHost, _ string) string {
	return h.(moveHost).viewMove(&s.groupPicker)
}

func (s *moveSurface) Footer() [][2]string { return uidialogs.MoveHints() }

func (s *moveSurface) Owner() inputOwner { return ownerManager }

func (s *moveSurface) Dismissible() bool { return true }

func (s *moveSurface) Leave(surfaceHost) {}
