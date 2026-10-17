package ui

import (
	"strings"

	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
)

const (
	railGutter    = 2
	contentGutter = 2
	railInset     = railGutter - 1
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
	glyph, label := m.messagesAlert()
	m.placeNoticeHit(glyph, label, footer, len(frame))
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
	glyph, label := m.messagesAlert()
	m.placeNoticeHit(glyph, label, footer, len(frame))
	m.quick.originY = len(frame)
	for _, line := range splitLines(footer) {
		frame = append(frame, paint(line, m.layout.width, backdropHex()))
	}
	return m.overlayTopRight(strings.Join(frame, "\n"), m.statusToast(), m.listChromeRows()+1)
}
