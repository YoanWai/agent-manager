package ui

import "github.com/YoanWai/agent-manager/internal/status"

// Row marks share a one-cell column. ASCII keeps that geometry without
// relying on font fallback.
const (
	workingGlyph  = "*"
	startingGlyph = "~"
	waitingGlyph  = "?"
	finishedGlyph = "+"
	erroredGlyph  = "x"
	idleGlyph     = "."

	reorderGrip      = "="
	inboxGlyph       = "@"
	branchGlyph      = "Y"
	groupOpenGlyph   = "v"
	groupClosedGlyph = ">"
)

var startupFrames = [...]string{"|", "/", "-", "\\"}

func statusGlyph(s string) string {
	switch s {
	case status.Working:
		return workingGlyph
	case status.Starting:
		return startingGlyph
	case status.Waiting:
		return waitingGlyph
	case status.Finished:
		return finishedGlyph
	case status.Errored, status.Dead:
		return erroredGlyph
	default:
		return idleGlyph
	}
}

func (m *Model) groupGlyph(entry treeRow) string {
	if entry.isRoot() {
		return " "
	}
	if m.collapsed[entry.group] {
		return groupClosedGlyph
	}
	return groupOpenGlyph
}
