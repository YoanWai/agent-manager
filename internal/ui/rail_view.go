package ui

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/keybind"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
)

const (
	railGutter    = 2
	contentGutter = 2
	railInset     = railGutter - 1
	shellGlyph    = "❯"
)

func (m *Model) viewListFrame() string {
	if m.fullFocus() {
		m.layout.displayedRail = uirail.Frame{}
		return m.viewFullFocusFrame()
	}
	if m.fullRows() {
		return m.viewFullListFrame()
	}
	leftWidth, rightWidth := m.splitWidths()
	footer := m.viewFooter()
	bodyHeight := m.listBodyHeight()
	contentWidth := rightWidth - 1

	frame := []string{}
	for _, line := range m.viewHeaderRows() {
		frame = append(frame, paint(line, m.layout.width, backdropHex()))
	}
	bleedWidth := contentWidth - 1
	railWidth := leftWidth - 1
	m.prepareRailFrame(railWidth, bodyHeight)
	railRows := railContentLines(m.layout.displayedRail.Lines)
	contentRows := m.contentLines(bleedWidth, bodyHeight)
	seam := make([]string, bodyHeight)
	edge := make([]string, bodyHeight)
	for i := range seam {
		seam[i] = m.seamCell(i < len(railRows) && railRows[i].rule)
		tone := panelHex()
		if i < len(railRows) && railRows[i].tone != "" {
			tone = railRows[i].tone
		}
		edge[i] = railEdgeCell(tone)
	}
	frame = append(frame, m.railTopRow(leftWidth+1, m.layout.width))
	frame = append(frame, joinColumns(
		edge,
		paintContent(railRows, railWidth, bodyHeight, panelHex()),
		seam,
		m.bleedColumn(bodyHeight),
		paintContent(contentRows, bleedWidth, bodyHeight, backdropHex()),
	)...)
	bottom := m.boundedRuleRow(leftWidth+1, m.layout.width, "▄")
	if m.mode == modeFocus && m.focus.pane.FrameBox().Valid {
		bottom = m.focusBottomRule(leftWidth+1, m.layout.width)
	}
	frame = append(frame, bottom)
	m.notices.placeHit(m, m.listGlyph(keybind.Messages), footer, len(frame))
	m.quick.originY = len(frame)
	for _, line := range splitLines(footer) {
		frame = append(frame, paint(line, m.layout.width, backdropHex()))
	}
	return m.overlayTopRight(strings.Join(frame, "\n"), m.statusToast(), m.listChromeRows()+1)
}

func (m *Model) fullRows() bool { return m.prefs.fullLayout && m.mode != modeFocus }

func (m *Model) viewFullListFrame() string {
	footer := m.viewFooter()
	bodyHeight := m.listBodyHeight()
	railWidth := m.layout.width - 1

	frame := []string{}
	for _, line := range m.viewHeaderRows() {
		frame = append(frame, paint(line, m.layout.width, backdropHex()))
	}
	m.prepareRailFrame(railWidth, bodyHeight)
	railRows := railContentLines(m.layout.displayedRail.Lines)
	edge := make([]string, bodyHeight)
	for i := range edge {
		tone := panelHex()
		if i < len(railRows) && railRows[i].tone != "" {
			tone = railRows[i].tone
		}
		edge[i] = railEdgeCell(tone)
	}
	frame = append(frame, m.railTopRow(railWidth, m.layout.width))
	frame = append(frame, joinColumns(edge, paintContent(railRows, railWidth, bodyHeight, panelHex()))...)
	frame = append(frame, m.boundedRuleRow(railWidth, m.layout.width, "▄"))
	m.notices.placeHit(m, m.listGlyph(keybind.Messages), footer, len(frame))
	m.quick.originY = len(frame)
	for _, line := range splitLines(footer) {
		frame = append(frame, paint(line, m.layout.width, backdropHex()))
	}
	return m.overlayTopRight(strings.Join(frame, "\n"), m.statusToast(), m.listChromeRows()+1)
}

func (m *Model) prepareRailFrame(width, height int) {
	footText := m.railFootLines(width)
	foot := make([]uirail.Line, len(footText))
	for i, line := range footText {
		foot[i].Text = line
	}
	rename := uirail.Rename{}
	if m.mode == modeRename {
		if m.rename.isGroup {
			rename.Target = uirail.Selection{Kind: uirail.GroupRow, Group: m.rename.path}
		} else {
			rename.Target = uirail.Selection{Kind: uirail.SessionRow, SessionID: m.rename.sessID}
			if session, ok := m.sessionByID(m.rename.sessID); ok {
				rename.Target.Group = session.Group
			}
		}
		fieldWidth := width - 4
		for _, row := range m.rail.Rows() {
			if row.Selection == rename.Target {
				fieldWidth -= 3 * row.Depth
				break
			}
		}
		if fieldWidth >= 5 {
			m.rename.input.Width = fieldWidth
		}
		rename.View = textInputView(m.rename.input)
	}
	ctx := uirail.RenderContext{
		Width:           width,
		Height:          max(height, 0),
		TerminalWidth:   m.layout.width,
		TerminalHeight:  m.layout.height,
		BodyOriginY:     m.listChromeRows(),
		Theme:           m.railTheme(),
		ListKeys:        m.services.listKeys,
		ComfortableRows: m.prefs.comfortableRows,
		MouseDisabled:   m.prefs.mouseDisabled,
		Focused:         m.mode == modeFocus,
		EnterFocuses:    m.enterFocuses(),
		StartupPhase:    m.startup.startupPhase,
		CursorMarker:    cursorAnchorMarker,
		Rename:          rename,
		Foot:            foot,
	}
	m.layout.displayedRail = m.rail.Render(ctx, m.layout.displayedRail)
}

func (m *Model) railTheme() uirail.Theme {
	return uirail.Theme{
		Bg: current.Bg, Surface: current.Surface, Overlay: current.Overlay, Border: current.Border,
		Bright: current.Bright, Text: current.Text, Dim: current.Dim, Subtle: current.Subtle,
		Accent: current.Accent, Accent2: current.Accent2,
		Working: current.Working, Waiting: current.Waiting, Finished: current.Finished,
		Errored: current.Errored, Idle: current.Idle,
	}
}

func railContentLines(lines []uirail.Line) []contentLine {
	out := make([]contentLine, len(lines))
	for i, line := range lines {
		out[i] = contentLine{text: line.Text, tone: line.Tone, rule: line.Rule, raw: line.Raw}
	}
	return out
}

func (m *Model) overlayRowMenu(frame string) string { return m.layout.displayedRail.Overlay(frame) }

func (m *Model) railLines(width, height int) []contentLine {
	m.prepareRailFrame(width, height)
	return railContentLines(m.layout.displayedRail.Lines)
}
