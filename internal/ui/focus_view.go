package ui

import (
	"fmt"
	"strings"

	"github.com/YoanWai/agent-manager/internal/keybind"
	uifocus "github.com/YoanWai/agent-manager/internal/ui/focus"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// viewFullFocusFrame is a session opened from the full screen list: the
// captured pane owns the whole terminal body and the list waits behind
// it. The focus rules cap and close the pane the way they do in the
// split, stretched to the terminal's edges.
func (m *Model) viewFullFocusFrame() string {
	footer := m.footer()
	bodyHeight := m.listBodyHeight()
	// This frame paints no rail, so a click lands on no row: the list
	// frame's hits would otherwise select a row nobody pointed at.
	m.layout.displayedRail = uirail.Frame{}
	frame := []string{}
	for _, line := range m.viewHeaderRows() {
		frame = append(frame, paint(line, m.layout.width, backdropHex()))
	}
	frame = append(frame, paint(hrule(m.layout.width), m.layout.width, backdropHex()))
	frame = append(frame, paint(m.focusFactsLine(m.layout.width), m.layout.width, backdropHex()))
	frame = append(frame, paint(m.focusEdge(m.layout.width), m.layout.width, backdropHex()))
	paneRows := m.previewLines(m.layout.width, bodyHeight, strings.Repeat(" ", contentGutter))
	frame = append(frame, paintContent(paneRows, m.layout.width, bodyHeight, backdropHex())...)
	frame = append(frame, paint(m.focusEdge(m.layout.width), m.layout.width, backdropHex()))
	m.placeKeys(footer.hits, 0, len(frame))
	for _, line := range splitLines(footer.text) {
		frame = append(frame, paint(line, m.layout.width, backdropHex()))
	}
	return m.overlayTopRight(strings.Join(frame, "\n"), m.statusToast(), m.listChromeRows()+1)
}

// focusFactsLine says which session a full screen frame is showing, where
// nothing else on it does: the state dot and the name on the left, then
// where it runs and what it costs against the right edge, with the whole
// width to itself and a rule under it holding it off the pane. The keys
// the split's rule names are in the footer, so they stay there.
func (m *Model) focusFactsLine(width int) string {
	sess, ok := m.selected()
	if !ok {
		return ""
	}
	sep := subtleStyle().Render(" · ")
	left := " " + m.sessionGlyph(sess) + " " + valueStyle().Render(m.displayName(sess)) +
		sep + valueStyle().Render(sess.Tool) +
		sep + lipgloss.NewStyle().Foreground(statusColor(sess.Status)).Render(statusLabel(sess.Status)) +
		sep + subtleStyle().Render(relSince(lastActivity(sess)))
	facts := []uifocus.Fact{{Text: valueStyle().Render(truncateTail(shortHome(m.sessionDir(sess), m.env.homeDir), focusFactsDirCap)), Spare: 3}}
	if sess.WorktreeBranch != "" {
		facts = append(facts, uifocus.Fact{Text: subtleStyle().Render(m.glyphs().BranchGlyph+" ") + valueStyle().Render(sess.WorktreeBranch), Spare: 2})
	}
	if m.workspace.procFor == sess.ID && m.workspace.proc.OK {
		facts = append(facts,
			uifocus.Fact{Text: labelStyle().Render("cpu ") + valueStyle().Render(fmt.Sprintf("%.1f%%", m.workspace.proc.CPUPercent)), Spare: 1},
			uifocus.Fact{Text: labelStyle().Render("ram ") + valueStyle().Render(humanBytes(m.workspace.proc.RSS)), Spare: 1})
	}
	facts = append(facts, uifocus.Fact{Text: labelStyle().Render("started ") + valueStyle().Render(relSince(sess.CreatedAt)), Spare: 4})
	if queued := m.workspace.queuedMessages[sess.ID]; queued > 0 {
		facts = append(facts, uifocus.Fact{Text: valueStyle().Render(fmt.Sprintf("%d queued", queued)), Spare: 0})
	}

	right := uifocus.FitFacts(facts, sep, width-ansi.StringWidth(left)-focusFactsGap)
	if ansi.StringWidth(left) > width {
		return ansi.Truncate(left, max(width, 0), "…")
	}
	return rowColumns(left, right, width)
}

// focusEdge is the hairline holding the full screen pane off what sits
// above and below it, in the pane's own tone once it has a box to trace.
func (m *Model) focusEdge(width int) string {
	if m.focus.pane.FrameBox().Valid {
		return focusEdgeStyle().Render(strings.Repeat("─", max(width, 0)))
	}
	return hrule(width)
}

// focusFactsDirCap keeps a deep path from crowding the readings beside it,
// and focusFactsGap is the least space kept between the name and them.
const (
	focusFactsDirCap = 40
	focusFactsGap    = 2
)

// focusTopRule is the hairline that caps the focused pane in the split,
// naming the session's keys.
func focusTopRule(width int, keys keybind.Table) string {
	return uifocus.TopRule(width, uifocus.RuleKeys{
		Back:   keys.Binding(keybind.Detach).Label(),
		Review: keys.Binding(keybind.Review).Label(),
		Editor: keys.Binding(keybind.Editor).Label(),
	}, annotationStyle(), focusEdgeStyle())
}
