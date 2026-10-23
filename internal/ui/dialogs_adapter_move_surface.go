package ui

import (
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
)

func newMoveSurface(m *Model, move uidialogs.Move, picker spawn.Picker) *uidialogs.MoveSurface {
	return uidialogs.NewMoveSurface(moveRoot{m}, move, picker)
}
