package dialogs

import (
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	"github.com/YoanWai/agent-manager/internal/ui/surface"
	tea "github.com/charmbracelet/bubbletea"
)

// MoveSurface is the card that places a session or group. It holds the row
// being moved and its own group picker of targets, which the root shapes
// when the card opens.
type MoveSurface struct {
	Move
	picker spawn.Picker
	root   MoveRoot
}

type MoveRoot interface {
	PlaceMove(move Move, picker *spawn.Picker) tea.Cmd
	PaintMove(picker *spawn.Picker) string
}

// NewMoveSurface opens the card on the row it places, the picker the root
// shaped for it and the root it reaches beyond the stack.
func NewMoveSurface(root MoveRoot, move Move, picker spawn.Picker) *MoveSurface {
	return &MoveSurface{Move: move, picker: picker, root: root}
}

func (s *MoveSurface) Root() MoveRoot { return s.root }

func (s *MoveSurface) Picker() *spawn.Picker { return &s.picker }

func (s *MoveSurface) Key(h surface.Host, msg tea.KeyMsg) (bool, tea.Cmd) {
	key := s.Move.Key(msg)
	switch key.Action {
	case MoveCancel:
		h.Pop()
	case MoveCursor:
		s.picker.Step(key.Delta)
	case MoveSubmit:
		return true, s.root.PlaceMove(s.Move, &s.picker)
	}
	return true, nil
}

// Click moves the picker's cursor to the target a click landed on, and
// moves the row there on a click on the target already under it.
func (s *MoveSurface) Click(h surface.Host, line int) tea.Cmd {
	switch {
	case line < 0 || line >= len(s.picker.Groups):
		return nil
	case line != s.picker.GroupIndex:
		s.picker.Step(line - s.picker.GroupIndex)
		return nil
	}
	return s.root.PlaceMove(s.Move, &s.picker)
}

func (s *MoveSurface) Wheel(h surface.Host, arrow tea.KeyMsg) tea.Cmd {
	_, cmd := s.Key(h, arrow)
	return cmd
}

func (s *MoveSurface) View(h surface.Host, _ string) string {
	return s.root.PaintMove(&s.picker)
}

func (s *MoveSurface) Footer() [][2]string { return MoveHints() }

func (s *MoveSurface) Owner() surface.Owner { return surface.Manager }

func (s *MoveSurface) Dismissible() bool { return true }

func (s *MoveSurface) Leave(surface.Host) {}
