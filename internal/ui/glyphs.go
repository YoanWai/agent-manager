package ui

import (
	"github.com/YoanWai/agent-manager/internal/status"
	tea "github.com/charmbracelet/bubbletea"
)

type glyphMode string

const (
	glyphModeBasic    glyphMode = "basic"
	glyphModeAdvanced glyphMode = "advanced"
	// An unset preference follows this default; explicit choices survive a change.
	defaultGlyphMode = glyphModeAdvanced
	glyphModeSetting = "glyph_mode"
)

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

var advancedStartupFrames = [...]string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func (g glyphSet) statusGlyph(s string) string {
	switch s {
	case status.Working:
		return g.workingGlyph
	case status.Starting:
		return g.startingGlyph
	case status.Waiting:
		return g.waitingGlyph
	case status.Finished:
		return g.finishedGlyph
	case status.Errored, status.Dead:
		return g.erroredGlyph
	default:
		return g.idleGlyph
	}
}

func (m *Model) groupGlyph(entry treeRow) string {
	if entry.isRoot() {
		return " "
	}
	if m.collapsed[entry.group] {
		return m.glyphs().groupClosedGlyph
	}
	return m.glyphs().groupOpenGlyph
}

type glyphSet struct {
	workingGlyph, startingGlyph, waitingGlyph, finishedGlyph, erroredGlyph, idleGlyph string
	reorderGrip, inboxGlyph, branchGlyph, groupOpenGlyph, groupClosedGlyph            string
	shellGlyph, loaderHead, loaderTail, loaderRest                                    string
	startupFrames                                                                     []string
}

func glyphsFor(mode glyphMode) glyphSet {
	if mode.effective() == glyphModeAdvanced {
		return glyphSet{
			workingGlyph: "◐", startingGlyph: "◌", waitingGlyph: "◆", finishedGlyph: "●", erroredGlyph: "✕", idleGlyph: "○",
			reorderGrip: "⠿", inboxGlyph: "✉", branchGlyph: "⑂", groupOpenGlyph: "▾", groupClosedGlyph: "▸",
			shellGlyph: "❯", loaderHead: "●", loaderTail: "•", loaderRest: "·",
			startupFrames: advancedStartupFrames[:],
		}
	}
	return glyphSet{
		workingGlyph: workingGlyph, startingGlyph: startingGlyph, waitingGlyph: waitingGlyph,
		finishedGlyph: finishedGlyph, erroredGlyph: erroredGlyph, idleGlyph: idleGlyph,
		reorderGrip: reorderGrip, inboxGlyph: inboxGlyph, branchGlyph: branchGlyph,
		groupOpenGlyph: groupOpenGlyph, groupClosedGlyph: groupClosedGlyph,
		shellGlyph: ">", loaderHead: "*", loaderTail: ":", loaderRest: ".", startupFrames: startupFrames[:],
	}
}

func (m *Model) glyphs() glyphSet { return glyphsFor(m.glyphMode) }

func (mode glyphMode) effective() glyphMode {
	if mode == "" {
		return defaultGlyphMode
	}
	return mode
}

type glyphModeLoadedMsg struct {
	gen   int
	value string
	err   error
}

func (m *Model) loadGlyphMode() tea.Cmd {
	m.glyphModeGen++
	gen, st := m.glyphModeGen, m.store
	return func() tea.Msg {
		value, err := st.Setting(glyphModeSetting)
		return glyphModeLoadedMsg{gen: gen, value: value, err: err}
	}
}

func (m *Model) setGlyphMode(value string, err error) {
	if err != nil {
		m.errBar.text = "reading symbols setting: " + err.Error()
		return
	}
	switch glyphMode(value) {
	case "", glyphModeBasic, glyphModeAdvanced:
		m.glyphMode = glyphMode(value)
	default:
		m.errBar.text = "unknown symbols setting: " + value
	}
}

func (m *Model) glyphModeLabel() string {
	if m.glyphMode.effective() == glyphModeAdvanced {
		return "Advanced"
	}
	return "Compatible"
}

type glyphModeSavedMsg struct {
	update bool
	err    error
}

func (m *Model) saveGlyphMode(update bool) tea.Cmd {
	if !m.settings.glyphModeChanged {
		return nil
	}
	m.settings.glyphModeSaving = true
	mode, st := m.glyphMode, m.store
	return func() tea.Msg {
		return glyphModeSavedMsg{update: update, err: st.SetSetting(glyphModeSetting, string(mode))}
	}
}

func (g glyphSet) startupFrame(phase int) string {
	return g.startupFrames[phase%len(g.startupFrames)]
}
