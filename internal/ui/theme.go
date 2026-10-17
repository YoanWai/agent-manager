package ui

import (
	"github.com/YoanWai/agent-manager/internal/termseq"
	"github.com/YoanWai/agent-manager/internal/tmux"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Theme and the palettes live in ui/theme so feature packages read the
// same tokens; the root keeps the live styles derived from them.
type Theme = uitheme.Theme

var themes = uitheme.Palettes

func themeIndex(name string) int { return uitheme.Index(name) }

func mix(a, b string, ratio float64) string { return uitheme.Mix(a, b, ratio) }

// applyTheme publishes the styles for a token set; a theme switch repaints
// from the next frame on.
func applyTheme(t Theme) { activeStyles.Store(newThemeStyles(t)) }

// agentPaneTheme is the backdrop and text color an agent pane sits on: the
// theme's own, the same colors the terminal is set to and the capture is
// drawn over. Agents that auto-detect follow it, so a light theme hosts
// light agents.
func agentPaneTheme() tmux.PaneTheme {
	fgbg := "15;0"
	if current().LightBackdrop() {
		fgbg = "0;15"
	}
	return tmux.PaneTheme{Foreground: current().Text, Background: current().Bg, ColorFgBg: fgbg}
}

// SyncTerminalColors sets the terminal's own text and background colors to
// the theme's (OSC 10 and 11). The frame can only paint its cell grid; any
// window padding the terminal draws around that grid keeps the terminal's
// background, so the two must be the same color for the frame's edges to
// look exact, and text an agent prints in the default color must read on
// that background. Terminals without OSC 10 and 11 ignore them.
func SyncTerminalColors() {
	emitToTerminal("\x1b]10;" + current().Text + "\x07\x1b]11;" + current().Bg + "\x07")
}

// syncPaneTheme hands the tmux server the colors agent panes render on,
// so an agent that auto-detects its palette resolves to the same side the
// manager is drawing. Sessions that are already running keep whatever they
// resolved at startup; the theme reaches them on their next launch. The
// chosen theme is recorded synchronously so a session created right after
// still opens on it; the push to a running server shells out to tmux, so it
// runs as a command off the update path and surfaces a failure through errMsg.
func (m *Model) syncPaneTheme() tea.Cmd {
	m.services.tmux.PublishPaneTheme(agentPaneTheme())
	return func() tea.Msg {
		if err := m.services.tmux.PushPaneTheme(); err != nil {
			return errMsg{err}
		}
		return nil
	}
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

// bgSeq is the raw "set background" SGR for a hex color, for the few spots
// that paint a background around content lipgloss already styled. It goes
// through the live color profile, so a 256-color terminal gets an indexed
// sequence rather than a truecolor one lipgloss would have downsampled.
func bgSeq(hex string) string {
	return "\x1b[" + lipgloss.ColorProfile().Color(hex).Sequence(true) + "m"
}

// fgSeq is the raw "set foreground" SGR for a hex color.
func fgSeq(hex string) string {
	return "\x1b[" + lipgloss.ColorProfile().Color(hex).Sequence(false) + "m"
}
