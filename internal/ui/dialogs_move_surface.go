package ui

import (
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	tea "github.com/charmbracelet/bubbletea"
)

// moveSurface is the card that places a session or group. Its targets are
// the spawn form's group picker, which the root shapes when the card opens;
// the surface holds the row being moved and the keys.
type moveSurface struct {
	uidialogs.Move
}

// moveHost is what the card reaches on the root beyond the stack.
type moveHost interface {
	pop()
	moveGroupCursor(delta int) tea.Cmd
	placeMove(move uidialogs.Move) tea.Cmd
	movePicker() (rows, cursor int)
	viewMove() string
}

func newMoveSurface(move uidialogs.Move) *moveSurface { return &moveSurface{Move: move} }

func (s *moveSurface) Key(h surfaceHost, msg tea.KeyMsg) (bool, tea.Cmd) {
	host := h.(moveHost)
	key := s.Move.Key(msg)
	switch key.Action {
	case uidialogs.MoveCancel:
		h.pop()
	case uidialogs.MoveCursor:
		return true, host.moveGroupCursor(key.Delta)
	case uidialogs.MoveSubmit:
		return true, host.placeMove(s.Move)
	}
	return true, nil
}

// Click moves the picker's cursor to the target a click landed on, and
// moves the row there on a click on the target already under it.
func (s *moveSurface) Click(h surfaceHost, line int) tea.Cmd {
	host := h.(moveHost)
	rows, cursor := host.movePicker()
	switch {
	case line < 0 || line >= rows:
		return nil
	case line != cursor:
		return host.moveGroupCursor(line - cursor)
	}
	return host.placeMove(s.Move)
}

func (s *moveSurface) Wheel(h surfaceHost, arrow tea.KeyMsg) tea.Cmd {
	_, cmd := s.Key(h, arrow)
	return cmd
}

func (s *moveSurface) View(h surfaceHost, _ string) string { return h.(moveHost).viewMove() }

func (s *moveSurface) Footer() [][2]string { return uidialogs.MoveHints() }

func (s *moveSurface) Owner() inputOwner { return ownerManager }

func (s *moveSurface) Dismissible() bool { return true }

func (s *moveSurface) Leave(surfaceHost) {}
