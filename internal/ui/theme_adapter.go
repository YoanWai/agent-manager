package ui

import (
	"github.com/YoanWai/agent-manager/internal/termseq"
	"github.com/YoanWai/agent-manager/internal/tmux"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
)

// applyTheme publishes the styles for a token set; a theme switch repaints
// from the next frame on.
func applyTheme(t uitheme.Theme) { uitheme.Apply(t) }

// SyncTerminalColors sets the terminal's own text and background colors to
// the theme's (OSC 10 and 11). The frame can only paint its cell grid; any
// window padding the terminal draws around that grid keeps the terminal's
// background, so the two must be the same color for the frame's edges to
// look exact, and text an agent prints in the default color must read on
// that background. Terminals without OSC 10 and 11 ignore them.
func SyncTerminalColors() {
	emitToTerminal("\x1b]10;" + uitheme.Current().Text + "\x07\x1b]11;" + uitheme.Current().Bg + "\x07")
}

// ResetTerminalColors restores the terminal's own text and background colors
// (OSC 110 and 111) when the manager exits.
func ResetTerminalColors() {
	emitToTerminal("\x1b]110\x07\x1b]111\x07")
}

// emitToTerminal sends a control sequence to whatever is actually drawing
// the window. Run under tmux, only the passthrough envelope goes out: a
// plain OSC would make tmux recolor our pane's default background, and a
// recolored pane paints as explicit color while the terminal's padding
// keeps blending its own — the exact ring the backdrop scheme exists to
// avoid. EnableTerminalPassthrough must have opened the envelope first.
func emitToTerminal(seq string) {
	_ = termseq.Emit(seq)
}

// EnableTerminalPassthrough opens the tmux passthrough envelope the backdrop
// sync and the clipboard fallback both write through.
func EnableTerminalPassthrough() {
	termseq.EnablePassthrough()
}

// syncPaneTheme hands the tmux server the colors agent panes render on, so
// an agent that auto-detects its palette resolves to the same side the
// manager is drawing. The chosen theme is recorded synchronously so a
// session created right after still opens on it; the push to a running
// server shells out to tmux, so it runs as a job off the update path and
// surfaces a failure through the status bar.
func (m *Model) syncPaneTheme() tea.Cmd {
	m.services.tmux.PublishPaneTheme(agentPaneTheme())
	m.enqueueEffect(uitheme.PaneRequest{}, 0, false)
	return m.nextEffectCmd()
}

// agentPaneTheme is the backdrop and text color an agent pane sits on: the
// theme's own, the same colors the terminal is set to and the capture is
// drawn over. Agents that auto-detect follow it, so a light theme hosts
// light agents.
func agentPaneTheme() tmux.PaneTheme {
	current := uitheme.Current()
	fgbg := "15;0"
	if current.LightBackdrop() {
		fgbg = "0;15"
	}
	return tmux.PaneTheme{Foreground: current.Text, Background: current.Bg, ColorFgBg: fgbg}
}

// applyPaneTheme reports a push that failed; the panes already up keep the
// palette they resolved at startup either way.
func (m *Model) applyPaneTheme(err error) tea.Cmd {
	if err != nil {
		m.reportErr(err.Error())
	}
	return nil
}

func (m *Model) themeServices() uitheme.Services {
	return uitheme.Services{Tmux: m.services.tmux}
}
