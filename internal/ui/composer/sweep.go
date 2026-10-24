package composer

import (
	"time"

	"github.com/YoanWai/agent-manager/internal/clipboard"
	tea "github.com/charmbracelet/bubbletea"
)

type SweepMsg struct{ Err error }

// SweepTickMsg asks for another sweep and the tick after it.
type SweepTickMsg struct{}

// SweepInterval keeps clearing old pasted images while the manager stays
// open. A manager left running for weeks would otherwise sweep once at
// startup and then let screenshots pile up in temp until the next restart.
const SweepInterval = 24 * time.Hour

// SweepStale is the seam tests swap for a fake sweep.
var SweepStale = func() error { return clipboard.SweepStale(clipboard.StaleAfter) }

// Sweep clears images pasted long enough ago that no agent will open them
// again. It runs off the event loop: the pastes directory lives in temp,
// where a slow disk must not stall a keystroke.
func Sweep() tea.Msg { return SweepMsg{Err: SweepStale()} }

func SweepCmd() tea.Msg { return Sweep() }

func SweepTick() tea.Cmd {
	return tea.Tick(SweepInterval, func(time.Time) tea.Msg { return SweepTickMsg{} })
}
