package ui

import (
	"github.com/YoanWai/agent-manager/internal/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"time"
)

// pasteSweepMsg carries the result of one pass over the pastes directory.
type pasteSweepMsg struct{ err error }

type pasteSweepTickMsg struct{}

// pasteSweepInterval keeps clearing old pasted images while the manager
// stays open. A manager left running for weeks would otherwise sweep once
// at startup and then let screenshots pile up in temp until the next
// restart.
const pasteSweepInterval = 24 * time.Hour

// sweepStalePastes is the seam tests swap for a fake sweep.
var sweepStalePastes = func() error { return clipboard.SweepStale(clipboard.StaleAfter) }

// sweepPastes clears images pasted long enough ago that no agent will open
// them again. It runs off the event loop: the pastes directory lives in
// temp, where a slow disk must not stall a keystroke.
func (m *Model) sweepPastes() tea.Msg {
	return pasteSweepMsg{err: sweepStalePastes()}
}

func (m *Model) pasteSweepTick() tea.Cmd {
	return tea.Tick(pasteSweepInterval, func(time.Time) tea.Msg { return pasteSweepTickMsg{} })
}
