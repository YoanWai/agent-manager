package ui

import (
	"strconv"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/lipgloss"
)

func statusColor(s string) lipgloss.Color {
	switch s {
	case status.Working, status.Starting:
		return colorWorking()
	case status.Waiting:
		return colorWaiting()
	case status.Finished:
		return colorFinished()
	case status.Errored, status.Dead:
		return colorErrored()
	default:
		return colorIdle()
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
	return inboxBadgeStyle().Render(g.InboxGlyph + strconv.Itoa(count))
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
	return inboxBadgeStyle().Render(m.afterTurnGlyph(action))
}

func (m *Model) glyphs() presentation.GlyphSet {
	return presentation.GlyphsFor(m.prefs.glyphMode)
}
