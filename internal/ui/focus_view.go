package ui

import (
	"fmt"
	"github.com/YoanWai/agent-manager/internal/keybind"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"strings"
)

// viewFullFocusFrame is a session opened from the full screen list: the
// captured pane owns the whole terminal body and the list waits behind
// it. The focus rules cap and close the pane the way they do in the
// split, stretched to the terminal's edges.
func (m *Model) viewFullFocusFrame() string {
	footer := m.viewFooter()
	bodyHeight := m.listBodyHeight()
	// This frame paints no rail, so a click lands on no row: the list
	// frame's hits would otherwise select a row nobody pointed at.
	m.displayedRail = uirail.Frame{}
	m.notices.noticeHit = noticeHit{}
	frame := []string{}
	for _, line := range m.viewHeaderRows() {
		frame = append(frame, paint(line, m.width, backdropHex()))
	}
	frame = append(frame, paint(hrule(m.width), m.width, backdropHex()))
	frame = append(frame, paint(m.focusFactsLine(m.width), m.width, backdropHex()))
	frame = append(frame, paint(m.focusEdge(m.width), m.width, backdropHex()))
	paneRows := m.previewLines(m.width, bodyHeight, strings.Repeat(" ", contentGutter))
	frame = append(frame, paintContent(paneRows, m.width, bodyHeight, backdropHex())...)
	frame = append(frame, paint(m.focusEdge(m.width), m.width, backdropHex()))
	for _, line := range splitLines(footer) {
		frame = append(frame, paint(line, m.width, backdropHex()))
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
	sep := subtleStyle.Render(" · ")
	left := " " + m.sessionGlyph(sess) + " " + valueStyle.Render(m.displayName(sess)) +
		sep + valueStyle.Render(sess.Tool) +
		sep + lipgloss.NewStyle().Foreground(statusColor(sess.Status)).Render(statusLabel(sess.Status)) +
		sep + subtleStyle.Render(relSince(lastActivity(sess)))
	// The facts give way one at a time as the terminal narrows, the least
	// telling first, so a tight line still carries what it has room for
	// rather than dropping the lot.
	facts := []focusFact{{text: valueStyle.Render(truncateTail(shortHome(sess.Cwd), focusFactsDirCap)), spare: 3}}
	if sess.WorktreeBranch != "" {
		facts = append(facts, focusFact{text: subtleStyle.Render("⑂ ") + valueStyle.Render(sess.WorktreeBranch), spare: 2})
	}
	if m.workspace.procFor == sess.ID && m.workspace.proc.OK {
		facts = append(facts,
			focusFact{text: labelStyle.Render("cpu ") + valueStyle.Render(fmt.Sprintf("%.1f%%", m.workspace.proc.CPUPercent)), spare: 1},
			focusFact{text: labelStyle.Render("ram ") + valueStyle.Render(humanBytes(m.workspace.proc.RSS)), spare: 1})
	}
	facts = append(facts, focusFact{text: labelStyle.Render("started ") + valueStyle.Render(relSince(sess.CreatedAt)), spare: 4})
	if queued := m.workspace.queuedMessages[sess.ID]; queued > 0 {
		facts = append(facts, focusFact{text: valueStyle.Render(fmt.Sprintf("%d queued", queued)), spare: 0})
	}

	right := joinFacts(facts, sep)
	for len(facts) > 0 && ansi.StringWidth(left)+focusFactsGap+ansi.StringWidth(right) > width {
		facts = dropSparest(facts)
		right = joinFacts(facts, sep)
	}
	if ansi.StringWidth(left) > width {
		return ansi.Truncate(left, max(width, 0), "…")
	}
	return rowColumns(left, right, width)
}

// focusFact is one reading on the full screen focus line, spare ranking
// how readily it gives up its room: the higher, the sooner it goes.
type focusFact struct {
	text  string
	spare int
}

func joinFacts(facts []focusFact, sep string) string {
	if len(facts) == 0 {
		return ""
	}
	parts := make([]string, len(facts))
	for i, fact := range facts {
		parts[i] = fact.text
	}
	return strings.Join(parts, sep) + " "
}

func dropSparest(facts []focusFact) []focusFact {
	sparest := 0
	for i, fact := range facts {
		if fact.spare >= facts[sparest].spare {
			sparest = i
		}
	}
	return append(facts[:sparest], facts[sparest+1:]...)
}

// focusEdge is the hairline holding the full screen pane off what sits
// above and below it, in the pane's own tone once it has a box to trace.
func (m *Model) focusEdge(width int) string {
	if m.focusPane.FrameBox().Valid {
		return focusEdgeStyle.Render(strings.Repeat("─", max(width, 0)))
	}
	return hrule(width)
}

// focusFactsDirCap keeps a deep path from crowding the readings beside it,
// and focusFactsGap is the least space kept between the name and them.
const (
	focusFactsDirCap = 40
	focusFactsGap    = 2
)

func focusTopRule(width int, keys keybind.Table) string {
	hints := []string{"focused", keys.Binding(keybind.Detach).Label() + " back"}
	if label := keys.Binding(keybind.Review).Label(); label != "" {
		hints = append(hints, label+" review")
	}
	if label := keys.Binding(keybind.Editor).Label(); label != "" {
		hints = append(hints, label+" editor")
	}
	title := " " + strings.Join(hints, " · ") + " "
	rule := annotationStyle.Render(title)
	rest := width - lipgloss.Width(title)
	if rest > 0 {
		rule += focusEdgeStyle.Render(strings.Repeat("─", rest))
	}
	return rule
}
