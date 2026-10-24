package ui

import (
	"strings"

	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
)

const (
	contentGutter = 2
	railInset     = uiframe.RailGutter - 1
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
	footer := m.footer()
	bodyHeight := m.listBodyHeight()
	contentWidth := rightWidth - 1

	frame := []string{}
	for _, line := range m.viewHeaderRows() {
		frame = append(frame, uitheme.Paint(line, m.layout.width, uitheme.BackdropHex()))
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
		tone := uitheme.PanelHex()
		if i < len(railRows) && railRows[i].tone != "" {
			tone = railRows[i].tone
		}
		edge[i] = railEdgeCell(tone)
	}
	frame = append(frame, m.railTopRow(leftWidth+1, m.layout.width))
	frame = append(frame, uitheme.JoinColumns(
		edge,
		paintContent(railRows, railWidth, bodyHeight, uitheme.PanelHex()),
		seam,
		m.bleedColumn(bodyHeight),
		paintContent(contentRows, bleedWidth, bodyHeight, uitheme.BackdropHex()),
	)...)
	bottom := m.boundedRuleRow(leftWidth+1, m.layout.width, "▄")
	if m.focused() && m.focus.pane.FrameBox().Valid {
		bottom = m.focusBottomRule(leftWidth+1, m.layout.width)
	}
	frame = append(frame, bottom)
	m.placeListFooter(footer, len(frame))
	for _, line := range uitheme.SplitLines(footer.text) {
		frame = append(frame, uitheme.Paint(line, m.layout.width, uitheme.BackdropHex()))
	}
	return m.overlayTopRight(strings.Join(frame, "\n"), m.statusToast(), m.listChromeRows()+1)
}

func (m *Model) fullRows() bool { return m.prefs.fullLayout && !m.focused() }

func (m *Model) viewFullListFrame() string {
	footer := m.footer()
	bodyHeight := m.listBodyHeight()
	railWidth := m.layout.width - 1

	frame := []string{}
	for _, line := range m.viewHeaderRows() {
		frame = append(frame, uitheme.Paint(line, m.layout.width, uitheme.BackdropHex()))
	}
	m.prepareRailFrame(railWidth, bodyHeight)
	railRows := railContentLines(m.layout.displayedRail.Lines)
	edge := make([]string, bodyHeight)
	for i := range edge {
		tone := uitheme.PanelHex()
		if i < len(railRows) && railRows[i].tone != "" {
			tone = railRows[i].tone
		}
		edge[i] = railEdgeCell(tone)
	}
	frame = append(frame, m.railTopRow(railWidth, m.layout.width))
	frame = append(frame, uitheme.JoinColumns(edge, paintContent(railRows, railWidth, bodyHeight, uitheme.PanelHex()))...)
	frame = append(frame, m.boundedRuleRow(railWidth, m.layout.width, "▄"))
	m.placeListFooter(footer, len(frame))
	for _, line := range uitheme.SplitLines(footer.text) {
		frame = append(frame, uitheme.Paint(line, m.layout.width, uitheme.BackdropHex()))
	}
	return m.overlayTopRight(strings.Join(frame, "\n"), m.statusToast(), m.listChromeRows()+1)
}

// placeListFooter records where the list footer's keys and the quick bar
// it holds landed, with its first row at y.
func (m *Model) placeListFooter(footer legend, y int) {
	m.placeKeys(footer.hits, 0, y)
	if q := m.quick(); q != nil {
		q.OriginY = y
	}
}
