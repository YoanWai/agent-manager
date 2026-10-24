package ui

import (
	"strconv"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/termseq"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// paneTheme is the backdrop and text color an agent pane sits on: the
// theme's own, the same colors the terminal is set to and the capture is
// drawn over. Agents that auto-detect follow it, so a light theme hosts
// light agents.
func paneTheme() tmux.PaneTheme {
	current := uitheme.Current()
	fgbg := "15;0"
	if current.LightBackdrop() {
		fgbg = "0;15"
	}
	return tmux.PaneTheme{Foreground: current.Text, Background: current.Bg, ColorFgBg: fgbg}
}

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

// syncPaneTheme is the palette a Settings step asks for: the pane colors are
// published now, and the push to a running tmux server runs off the update
// path.
func (m *Model) syncPaneTheme() tea.Cmd {
	m.services.tmux.PublishPaneTheme(paneTheme())
	m.enqueueEffect(uitheme.PaneRequest{}, 0, false)
	return m.nextEffectCmd()
}

func statusColor(s string) lipgloss.Color {
	switch s {
	case status.Working, status.Starting:
		return uitheme.ColorWorking()
	case status.Waiting:
		return uitheme.ColorWaiting()
	case status.Finished:
		return uitheme.ColorFinished()
	case status.Errored, status.Dead:
		return uitheme.ColorErrored()
	default:
		return uitheme.ColorIdle()
	}
}

// statusLabel is the human text for a status; most match the raw value, but
// the transient launch state reads better spelled out.
func statusLabel(s string) string {
	if s == status.Starting {
		return "starting up"
	}
	return s
}

// padRight pads or clips a possibly-styled string to an exact display width.
func padRight(s string, width int) string {
	return presentation.PadRight(s, width)
}

// inboxBadge marks a session another agent has messages waiting for, in
// the manager's own accent so it cannot be read as a state the agent is in.
func inboxBadge(g presentation.GlyphSet, count int) string {
	return uitheme.InboxBadgeStyle().Render(g.InboxGlyph + strconv.Itoa(count))
}

// afterTurnGlyph makes a requested archive or kill visible while the agent
// finishes its turn, before the action takes effect.
func (m *Model) afterTurnGlyph(action string) string {
	switch action {
	case store.AfterTurnArchive:
		return m.glyphs().AfterTurnArchiveGlyph
	case store.AfterTurnKill:
		return m.glyphs().AfterTurnKillGlyph
	default:
		return ""
	}
}

var afterTurnNote = map[string]string{
	store.AfterTurnArchive: "archives when this turn ends",
	store.AfterTurnKill:    "killed when this turn ends",
}

func (m *Model) afterTurnBadge(action string) string {
	return uitheme.InboxBadgeStyle().Render(m.afterTurnGlyph(action))
}

func (m *Model) glyphs() presentation.GlyphSet {
	return presentation.GlyphsFor(m.prefs.glyphMode)
}

type preferences struct {
	glyphMode presentation.GlyphMode
	// focusOnEnter mirrors the persisted focus-key setting; the footer
	// reads it every frame, so it lives here instead of the store.
	focusOnEnter bool
	// arrowStep mirrors the persisted ←→ step-in/step-out setting, read
	// on every keypress.
	arrowStep bool
	// comfortableRows mirrors the persisted list density: entries paint
	// their meta on a second line instead of alongside the name. Every
	// rail frame reads it, so it lives here instead of the store.
	comfortableRows bool
	// fullLayout mirrors the persisted sessions layout: the rail owns the
	// whole width, with no preview column beside it. Every list frame
	// reads it, so it lives here instead of the store.
	fullLayout bool
	// Header and stats visibility stay cached because rendering and sizing
	// read them every frame.
	hideHeader bool
	hideStats  bool
	// mouseDisabled mirrors the persisted mouse-reporting setting: true gives
	// the rail and content column back to the terminal's own click-drag text
	// selection. Read on every Update via syncMouseCapture. Named for its off
	// polarity, like hideHeader/hideStats, so a bare Model{} in a test still
	// defaults to mouse reporting on.
	mouseDisabled bool
	// terminalBackground leaves the backdrop's cells on the terminal's own
	// colors, for translucent windows. Off polarity, so a bare Model{}
	// paints the backdrop like the default does.
	terminalBackground bool
	// baseFetchOff mirrors the persisted fetch-on-spawn setting, read on
	// every Update while a worktree spawn is being set up.
	baseFetchOff bool
}

// themeHost is the root as a pane-theme push reports back to it.
type themeHost struct{ m *Model }

func (h themeHost) ReportErr(text string) { h.m.reportErr(text) }

func (h themeHost) Services() uitheme.Services {
	return uitheme.Services{Tmux: h.m.services.tmux}
}
