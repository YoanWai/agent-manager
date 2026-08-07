package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/sysstat"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// railGutter is the left inset every rail line shares, and contentGutter
// the content column's. Consistent insets are what make an unbordered
// layout read as columns.
const (
	railGutter    = 2
	contentGutter = 2
	// railInset is the pad inside the rail's own column, one short of
	// railGutter because the edge column already occupies the first cell.
	railInset = railGutter - 1
)

// viewListFrame is the sessions rail beside the session content, both
// painted surfaces rather than drawn panels.
func (m *Model) viewListFrame() string {
	leftWidth, rightWidth := m.splitWidths()
	footer := m.viewFooter()
	bodyHeight := m.listBodyHeight()

	// The header is one full-width band over both columns, closed by a
	// rule; the seam between the rail and the content tees into that rule
	// and runs down to meet the footer's.
	contentWidth := rightWidth - 1

	frame := []string{}
	for _, line := range m.viewHeaderRows() {
		frame = append(frame, paint(line, m.width, backdropHex()))
	}
	// The rail's fill runs from the edge column through the seam column,
	// then bleeds half a cell further right, and half a cell above and
	// below its body rows — soft edges drawn with half blocks, the finest
	// step a character cell allows. The first column is drawn as foreground
	// blocks so the window margin beside it keeps the terminal's own
	// background and the fill's corners land exactly on the cell grid.
	bleedWidth := contentWidth - 1
	railWidth := leftWidth - 1
	m.pane.columnX = leftWidth + 2
	railRows := m.railLines(railWidth, bodyHeight)
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
	frame = append(frame, m.railTopRow(leftWidth+1, m.width))
	frame = append(frame, joinColumns(
		edge,
		paintContent(railRows, railWidth, bodyHeight, panelHex()),
		seam,
		m.bleedColumn(bodyHeight),
		paintContent(contentRows, bleedWidth, bodyHeight, backdropHex()),
	)...)
	bottom := m.boundedRuleRow(leftWidth+1, m.width, "▄")
	if m.mode == modeFocus && m.pane.box.ok {
		bottom = m.focusBottomRule(leftWidth+1, m.width)
	}
	frame = append(frame, bottom)
	for _, line := range splitLines(footer) {
		frame = append(frame, paint(line, m.width, backdropHex()))
	}
	return m.overlayTopRight(strings.Join(frame, "\n"), m.statusToast(), m.listChromeRows()+1)
}

// searchFieldLine is the live filter at the head of the rail: the typed
// query with a caret, and the key that closes it when there is room.
func (m *Model) searchFieldLine(width int) string {
	indent := strings.Repeat(" ", railInset)
	glyph := keyStyle.Render("⌕ ")
	caret := lipgloss.NewStyle().Foreground(colorAccent).Render("▏")
	hint := keyCapQuiet("esc", "close")
	chrome := railInset + ansi.StringWidth(glyph) + 1

	if m.search == "" {
		field := glyph + subtleStyle.Render("type to filter") + caret
		if gap := width - railInset - ansi.StringWidth(field) - ansi.StringWidth(hint) - 1; gap >= 2 {
			return indent + field + strings.Repeat(" ", gap) + hint
		}
		return indent + field
	}
	// A query longer than the rail keeps its end: that is where the caret is
	// and where the next keystroke lands.
	room := width - chrome - ansi.StringWidth(hint) - 2
	if room < 8 {
		hint, room = "", width-chrome
	}
	query := m.search
	if ansi.StringWidth(query) > room {
		query = "…" + string([]rune(query)[len([]rune(query))-max(room-1, 1):])
	}
	field := glyph + valueStyle.Render(query) + caret
	if hint == "" {
		return indent + field
	}
	gap := width - railInset - ansi.StringWidth(field) - ansi.StringWidth(hint) - 1
	return indent + field + strings.Repeat(" ", max(gap, 1)) + hint
}

// railLines is the sessions rail: the entry list on top, the machine
// meters and the messages card docked at the bottom behind their seam.
func (m *Model) railLines(width, height int) []contentLine {
	meters := m.railFootLines(width)
	listHeight := height - len(meters) - 1
	if listHeight < 3 {
		listHeight, meters = height, nil
	}
	var rows []contentLine
	// Both banners cost the list three rows each, so each one is only laid
	// while entries still have room under it: a rail that is all banner
	// says nothing about the fleet.
	const railBannerRows, railListMin = 3, 3
	room := func(cost int) bool { return listHeight-len(rows)-cost >= railListMin }
	// Search heads the list it filters, so the query sits over the entries it
	// is narrowing. It is also the field being typed into, so a rail too tight
	// for the padded block keeps the bare field rather than dropping it.
	if m.searching {
		field := contentLine{text: m.searchFieldLine(width)}
		switch {
		case room(railBannerRows):
			rows = append(rows, contentLine{}, field, contentLine{})
		case room(1):
			rows = append(rows, field)
		}
	}
	// The list starts straight under the pane's top edge; the empty state
	// centers itself in the full list area instead.
	if m.showArchived && room(railBannerRows) {
		rows = append(rows, contentLine{})
		rows = append(rows,
			contentLine{text: strings.Repeat(" ", railInset) + scopeBadgeStyle.Render("ARCHIVED") +
				subtleStyle.Render("  ") + keyCap("t", "back to active")},
			contentLine{})
	}
	rows = append(rows, m.entryLines(width, max(listHeight-len(rows), 0))...)
	for len(rows) < listHeight {
		rows = append(rows, contentLine{})
	}
	rows = rows[:listHeight]
	if meters != nil {
		rows = append(rows, contentLine{rule: true})
		for _, line := range meters {
			rows = append(rows, contentLine{text: line})
		}
	}
	return rows
}

// entryLines renders the visible slice of the tree. Entries are two lines
// tall, so the window is measured in lines rather than rows. Each line
// carries the tone its entry painted, which the edge column matches.
func (m *Model) entryLines(width, height int) []contentLine {
	// Root alone is still an empty list: it says what the rail holds, not
	// what to do about it being empty.
	if rest := m.rowsBelowRoot(); len(rest) == 0 {
		var lines []contentLine
		for _, entry := range m.rows {
			for _, line := range splitLines(m.renderTreeRow(entry, m.cursor == 0, width, 0, panelHex())) {
				lines = append(lines, contentLine{text: line})
			}
		}
		for _, line := range m.emptyRailLines(width, height-len(lines)) {
			lines = append(lines, contentLine{text: line})
		}
		return lines
	}
	heights := make([]int, len(m.rows))
	for i := range heights {
		heights[i] = m.entryHeight(m.rows[i])
	}
	start, end := lineWindow(heights, m.cursor, height)

	var lines []contentLine
	for i := start; i < end; i++ {
		selected := i == m.cursor
		entry := m.rows[i]
		tone := panelHex()
		if selected || m.renamingRow(entry) {
			tone = selectedHex()
		}
		for _, line := range splitLines(m.renderTreeRow(entry, selected, width, i, tone)) {
			lines = append(lines, contentLine{text: line, tone: tone})
		}
	}
	// The counters ride in whatever room the entries leave. Claiming a line
	// they do not have would trim an entry's last line away, and half a
	// two-line entry reads as a whole one that lost its meta.
	spare := height - len(lines)
	if start > 0 && spare > 0 {
		lines = append([]contentLine{{text: subtleStyle.Render(strings.Repeat(" ", railInset) + fmt.Sprintf("↑ %d more", start))}}, lines...)
		spare--
	}
	if end < len(m.rows) && spare > 0 {
		lines = append(lines, contentLine{text: subtleStyle.Render(strings.Repeat(" ", railInset) + fmt.Sprintf("↓ %d more", len(m.rows)-end))})
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return lines
}

// entryHeight is how many lines an entry paints: one in the compact list,
// two once the comfortable density unstacks the meta onto its own line.
// Groups match sessions either way, since a ragged list of one- and
// two-line rows reads as gaps rather than as rhythm.
func (m *Model) entryHeight(treeRow) int {
	if m.comfortableRows {
		return 2
	}
	return 1
}

// lineWindow keeps the cursor's entry fully visible inside a line budget,
// scrolling by whole entries so an entry is never cut in half.
func lineWindow(heights []int, cursor, budget int) (int, int) {
	if len(heights) == 0 || budget <= 0 {
		return 0, 0
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= len(heights) {
		cursor = len(heights) - 1
	}
	total := 0
	for _, h := range heights {
		total += h
	}
	if total <= budget {
		return 0, len(heights)
	}
	// Grow a window around the cursor, preferring to keep entries above it
	// on screen so the list does not jump when stepping down.
	start, end, used := cursor, cursor+1, heights[cursor]
	for {
		grew := false
		if end < len(heights) && used+heights[end] <= budget-1 {
			used += heights[end]
			end++
			grew = true
		}
		if start > 0 && used+heights[start-1] <= budget-1 {
			start--
			used += heights[start]
			grew = true
		}
		if !grew {
			break
		}
	}
	return start, end
}

func (m *Model) emptyRailLines(width, height int) []string {
	title := "no sessions yet"
	hint := keyCap("n", "starts one")
	if m.showArchived {
		title = "nothing archived"
		hint = keyCap("t", "back to active")
	}
	if m.statusFilter.active() {
		title = "nothing needs " + m.statusFilter.label()
		hint = keyCap("w", "show all")
	}
	if search := strings.TrimSpace(m.search); search != "" {
		title = "no matches"
		hint = subtleStyle.Render("for \"" + search + "\"")
	}
	titleLine := centerLine(
		lipgloss.NewStyle().Bold(true).Foreground(colorBright).Render(title),
		width,
	)
	hintLine := centerLine(hint, width)
	block := []string{titleLine, "", hintLine}
	if height <= 0 {
		return block
	}
	if height < len(block) {
		return block[:height]
	}
	out := make([]string, height)
	start := (height - len(block)) / 2
	copy(out[start:], block)
	return out
}

// centerLine pads a styled string so its visible text sits in the middle
// of width columns.
func centerLine(s string, width int) string {
	if width <= 0 {
		return s
	}
	w := ansi.StringWidth(s)
	if w >= width {
		return ansi.Truncate(s, width, "…")
	}
	left := (width - w) / 2
	return strings.Repeat(" ", left) + s
}

// treeGuidesAt is the ancestry trail left of a nested entry: a branch
// connector — ├─ mid-list, ╰─ for the last child — behind one guide per
// ancestor, so a group's children hang off its branch the way the legacy
// tree drew them. A slot goes quiet once its level has no further
// siblings below, which is what closes a branch off.
func (m *Model) treeGuidesAt(index int) string {
	if index < 0 || index >= len(m.rows) {
		return ""
	}
	depth := m.rows[index].depth
	if depth <= 0 {
		return ""
	}
	var guides strings.Builder
	for slot := 1; slot <= depth; slot++ {
		continues := m.slotContinues(index, slot)
		switch {
		case slot < depth && continues:
			guides.WriteString("│  ")
		case slot < depth:
			guides.WriteString("   ")
		case continues:
			guides.WriteString("├─ ")
		default:
			guides.WriteString("╰─ ")
		}
	}
	return subtleStyle.Render(guides.String())
}

// treeGuideTrail is the same ancestry read one line lower: every branch
// the entry's own row connected to carries straight down past its second
// line, so a two-line entry cannot leave a gap in the tree.
func (m *Model) treeGuideTrail(index int) string {
	if index < 0 || index >= len(m.rows) {
		return ""
	}
	depth := m.rows[index].depth
	if depth <= 0 {
		return ""
	}
	var guides strings.Builder
	for slot := 1; slot <= depth; slot++ {
		if m.slotContinues(index, slot) {
			guides.WriteString("│  ")
			continue
		}
		guides.WriteString("   ")
	}
	return subtleStyle.Render(guides.String())
}

// slotContinues reports whether the level named by slot has another entry
// below index. A slot goes quiet once its level has no further siblings,
// which is what closes a branch off.
func (m *Model) slotContinues(index, slot int) bool {
	for j := index + 1; j < len(m.rows); j++ {
		if m.rows[j].depth < slot {
			return false
		}
		if m.rows[j].depth == slot {
			return true
		}
	}
	return false
}

// renderTreeRow paints one entry: a status dot, the name, and what the
// entry is doing set against the row's far edge. The selected entry lifts
// onto its own band instead of wearing a marker.
func (m *Model) renderTreeRow(entry treeRow, selected bool, width, index int, bg string) string {
	pad := strings.Repeat(" ", railInset)
	guides := m.treeGuidesAt(index)
	trail := m.treeGuideTrail(index)

	if m.renamingRow(entry) {
		line := pad + guides + m.renameRowInput(entry, width-railGutter-ansi.StringWidth(guides))
		row := paint(line, width, selectedHex())
		if m.comfortableRows {
			row += "\n" + paint(pad+trail, width, selectedHex())
		}
		return row
	}

	if entry.isGroup {
		return m.renderGroupEntry(entry, selected, width, pad, guides, trail, bg)
	}
	return m.renderSessionEntry(entry, selected, width, pad, guides, trail, bg)
}

func (m *Model) renderSessionEntry(entry treeRow, selected bool, width int, pad, guides, trail, bg string) string {
	sess := entry.sess
	dot := lipgloss.NewStyle().Foreground(statusColor(sess.Status)).Render(statusGlyph(sess.Status))
	nameStyle := valueStyle
	if selected {
		nameStyle = lipgloss.NewStyle().Foreground(colorBright).Bold(true)
	}
	head := pad + guides + dot + " " + nameStyle.Render(sess.Name)
	focused := selected && m.mode == modeFocus
	if focused {
		head += " " + focusBadgeStyle.Render(" FOCUS ")
	}

	metaStyle := subtleStyle
	if selected {
		metaStyle = mutedStyle
	}
	// A session names its state in words as well as in its dot; a group,
	// whose row rolls several states together, is left to its dots.
	meta := lipgloss.NewStyle().Foreground(statusColor(sess.Status)).Render(statusLabel(sess.Status)) +
		metaStyle.Render(" · "+sess.Tool+" · "+relSince(lastActivity(sess)))

	if m.comfortableRows {
		return stackedRow(head, metaIndent(pad, trail)+meta, width, bg)
	}
	return paint(rowColumns(head, meta, width-railGutter), width, bg)
}

// metaIndent lines a second row line up under the name on the first, past
// the entry's guides and the glyph column ahead of it.
func metaIndent(pad, trail string) string {
	return pad + trail + "  "
}

func stackedRow(head, meta string, width int, bg string) string {
	return paint(head, width, bg) + "\n" + paint(meta, width, bg)
}

func (m *Model) renderGroupEntry(entry treeRow, selected bool, width int, pad, guides, trail, bg string) string {
	marker := "▾"
	if m.collapsed[entry.group] {
		marker = "▸"
	}
	nameStyle := lipgloss.NewStyle().Foreground(colorAccent2).Bold(true)
	if selected {
		nameStyle = nameStyle.Foreground(colorBright)
	}
	name := baseName(entry.group)
	if entry.isRoot() {
		// Nothing nests under root, so the marker is a blank that holds the column.
		marker, name = " ", "root"
		if !selected {
			nameStyle = nameStyle.Foreground(lipgloss.Color(mix(current.Accent2, current.Subtle, 0.5)))
		}
	}
	head := pad + guides + subtleStyle.Render(marker) + " " + nameStyle.Render(name)

	// What the group is doing rides on the same line as its name, so a
	// folded group still reports its subtree without being opened. It is
	// written in dots rather than words: the counts state the size too.
	meta := m.groupStatusGlyphs(entry.group)
	if meta == "" {
		meta = subtleStyle.Render("no agents yet")
	}

	if m.comfortableRows {
		return stackedRow(head, metaIndent(pad, trail)+meta, width, bg)
	}
	return paint(rowColumns(head, meta, width-railGutter), width, bg)
}

// computerLines is the machine block docked at the rail's foot: a label
// and one thin meter per resource.
func (m *Model) computerLines(width int) []string {
	snap := m.snap
	pad := strings.Repeat(" ", railInset)
	barWidth := width - 22
	if barWidth < 4 {
		barWidth = 4
	}
	if barWidth > 10 {
		barWidth = 10
	}

	meter := func(label string, percent float64, ok bool, extra string) string {
		if !ok {
			return pad + labelStyle.Width(5).Render(label) + subtleStyle.Render("n/a")
		}
		line := pad + labelStyle.Width(5).Render(label) + gauge(percent, barWidth) +
			valueStyle.Render(fmt.Sprintf(" %3.0f%%", percent))
		if extra != "" {
			line += subtleStyle.Render(" " + extra)
		}
		return line
	}

	lines := []string{pad + subtleStyle.Render("computer")}
	lines = append(lines,
		meter("cpu", snap.CPUPercent, snap.CPUOK, ""),
		meter("mem", snap.MemPercent, snap.MemOK, humanBytes(snap.MemUsed)+"/"+humanBytes(snap.MemTotal)),
	)
	if snap.SwapOK && snap.SwapTotal > 0 {
		// Percent is used/total of the current swap allocation (macOS
		// grows the file under pressure; Linux uses the fixed swap size).
		lines = append(lines, meter("swap", snap.SwapPercent, true,
			humanBytes(snap.SwapUsed)+"/"+humanBytes(snap.SwapTotal)))
	}
	if snap.DiskOK {
		lines = append(lines, meter("disk", snap.DiskPercent, true,
			humanBytes(snap.DiskFree)+" free"))
	} else {
		lines = append(lines, meter("disk", 0, false, ""))
	}
	if temps := tempReadings(snap); temps != "" {
		lines = append(lines, pad+labelStyle.Width(5).Render("temp")+temps)
	}
	if m.net.rates {
		lines = append(lines, pad+labelStyle.Width(5).Render("net")+
			valueStyle.Render("↓ "+humanBytes(m.net.down)+"/s")+
			subtleStyle.Render("  ↑ "+humanBytes(m.net.up)+"/s"))
	}
	return append(lines, "")
}

func tempReadings(snap sysstat.Snapshot) string {
	var parts []string
	if snap.CPUTempOK {
		parts = append(parts, valueStyle.Render(fmt.Sprintf("cpu %.0f°C", snap.CPUTemp)))
	}
	if snap.GPUTempOK {
		parts = append(parts, valueStyle.Render(fmt.Sprintf("gpu %.0f°C", snap.GPUTemp)))
	}
	if snap.SoCTempOK {
		parts = append(parts, valueStyle.Render(fmt.Sprintf("soc %.0f°C", snap.SoCTemp)))
	}
	return strings.Join(parts, subtleStyle.Render("  "))
}

// contentLines is the right column: what the cursor is on, then its live
// pane, with the quick prompt docked at the foot when it is open. width is
// the whole column; our own blocks sit inside its gutters, while the
// captured pane spans it edge to edge.
func (m *Model) contentLines(width, height int) []contentLine {
	gutter := strings.Repeat(" ", contentGutter)
	inner := width - 2*contentGutter
	ours := func(lines []string) []contentLine {
		out := make([]contentLine, len(lines))
		for i, line := range lines {
			out[i] = contentLine{text: gutter + line}
		}
		return out
	}

	var bar []contentLine
	if m.quick.active {
		bar = append([]contentLine{{}}, ours(splitLines(m.viewQuickBar(inner)))...)
	}
	body := ours(splitLines(m.viewDetail(inner)))
	rest := height - len(body) - len(bar) - 1
	if rest >= 3 {
		if group, ok := m.selectedGroup(); ok {
			body = append(body, contentLine{rule: true})
			body = append(body, ours(splitLines(m.viewGroupAgents(group, inner, rest)))...)
		} else {
			separator := contentLine{rule: true}
			if m.mode == modeFocus {
				separator = contentLine{text: focusTopRule(width), raw: true}
			}
			body = append(body, separator)
			m.previewBodyOffset = len(body)
			body = append(body, m.previewLines(width, rest, gutter)...)
		}
	}
	for len(body)+len(bar) < height {
		body = append(body, contentLine{})
	}
	return append(body[:max(height-len(bar), 0)], bar...)
}

// focusTopRule is the hairline that caps the focused pane, titled so the
// mode names itself where the eye already is.
func focusTopRule(width int) string {
	title := " focused · ctrl+q back "
	rule := annotationStyle.Render(title)
	rest := width - lipgloss.Width(title)
	if rest > 0 {
		rule += focusEdgeStyle.Render(strings.Repeat("─", rest))
	}
	return rule
}

// previewLines is the captured pane, filling every row under the detail
// separator. The captured rows are marked raw and drawn without the
// column's gutters: painting our backdrop behind an agent's own CLI colors
// would replace the background it drew itself, and insetting its output
// would put a margin around a terminal that has its own.
func (m *Model) previewLines(width, height int, gutter string) []contentLine {
	var lines []contentLine
	pane := paneExact(m.preview, height)
	if len(pane) == 0 {
		// No rows painted means nothing to hit-test: a box left over from
		// the previous session would catch clicks on empty space.
		m.pane.box = paneBox{}
		return append(lines, contentLine{text: gutter + mutedStyle.Render("(no output yet)")})
	}
	// Record where these rows land so mouse hit-testing reads the same
	// geometry the paint used.
	m.pane.box = paneBox{
		x:      m.paneOriginX(),
		y:      m.listChromeRows() + m.previewBodyOffset,
		width:  width,
		height: len(pane),
		ok:     true,
	}
	for i, line := range pane {
		lines = append(lines, contentLine{text: m.renderPaneRow(i, line, width), raw: true})
	}
	// Rows past the capture stay raw too: a painted tail under unpainted
	// output would read as a box drawn around the agent's last line.
	for len(lines) < height {
		lines = append(lines, contentLine{raw: true})
	}
	return lines
}

// detailLabelWidth is the column every fact label in the content head is
// padded to, so the values under it line up as one column.
const detailLabelWidth = 7

// factRow is one line of a detail head: a quiet label, its value, and a
// reading set against the right edge. The value is rendered against the
// columns it actually gets, and the reading is dropped rather than pushed
// off the edge when the column is too narrow to hold both.
func factRow(label string, value func(room int) string, right string, width int) string {
	room := width - detailLabelWidth - ansi.StringWidth(right) - 2
	if room < 12 {
		right, room = "", width-detailLabelWidth
	}
	return rowColumns(labelStyle.Render(padRight(label, detailLabelWidth))+value(max(room, 1)), right, width)
}

// plainValue is a factRow value that is already short enough to stand as it
// is, for facts whose text does not vary with the terminal.
func plainValue(value string) func(int) string {
	return func(int) string { return value }
}

// trimmedValue is a factRow value cut to the columns it gets, for readings
// that grow with the fleet rather than with the terminal.
func trimmedValue(value string) func(int) string {
	return func(room int) string { return ansi.Truncate(value, room, "…") }
}

// fitColumns lays the richest pair of readings that fits: rights are tried
// from richest to plainest, and for each, the lefts in turn. Nothing fitting
// means the plainest left is trimmed to what the column has.
func fitColumns(lefts, rights []string, width int) string {
	for _, right := range rights {
		for _, left := range lefts {
			if ansi.StringWidth(left)+ansi.StringWidth(right)+2 <= width {
				return rowColumns(left, right, width)
			}
		}
	}
	// Nothing fits whole, so the plainest left is trimmed to keep the richest
	// reading that still leaves it something readable.
	last := lefts[len(lefts)-1]
	for _, right := range rights {
		if room := width - ansi.StringWidth(right) - 2; room >= 8 {
			return rowColumns(ansi.Truncate(last, room, "…"), right, width)
		}
	}
	return rowColumns(ansi.Truncate(last, max(width, 1), "…"), "", width)
}

// viewDetail heads the content column: the selected session's name, its
// state, and the facts that place it (group, directory, age, usage).
func (m *Model) viewDetail(width int) string {
	sess, ok := m.selected()
	if !ok {
		if group, isGroup := m.selectedGroup(); isGroup {
			return m.viewGroupDetail(group, width)
		}
		return mutedStyle.Render("Select a session to inspect it.")
	}
	tool := sess.Tool
	if m.mode == modeRename && !m.rename.isGroup && m.rename.sessID == sess.ID {
		if picked := m.renameTool(); picked != "" {
			tool = picked
		}
	}

	state := lipgloss.NewStyle().Foreground(statusColor(sess.Status)).
		Render(statusGlyph(sess.Status)+" "+statusLabel(sess.Status)) +
		subtleStyle.Render(" · "+relSince(lastActivity(sess)))
	// The branch a worktree session lives on is the fact that tells it apart
	// from its siblings, so it rides beside the tool while the row has room,
	// and the tool chip goes before the name does.
	name := lipgloss.NewStyle().Foreground(colorBright).Bold(true).Render(sess.Name)
	withTool := name + "  " + chipStyle.Render(tool)
	heads := []string{withTool, name}
	if sess.WorktreeBranch != "" {
		heads = append([]string{withTool + " " + chipStyle.Render("⑂ "+sess.WorktreeBranch)}, heads...)
	}

	usage := ""
	if m.procFor == sess.ID && m.proc.OK {
		usage = labelStyle.Render("cpu ") + valueStyle.Render(fmt.Sprintf("%.1f%%", m.proc.CPUPercent)) +
			subtleStyle.Render(" · ") + labelStyle.Render("ram ") +
			valueStyle.Render(fmt.Sprintf("%.1f%%", m.proc.RamPercent)) +
			subtleStyle.Render(" · ") + valueStyle.Render(humanBytes(m.proc.RSS))
	}
	started := subtleStyle.Render("started " + relSince(sess.CreatedAt))
	group := lipgloss.NewStyle().Foreground(colorAccent2).Render(displayGroup(sess.Group))
	dir := func(room int) string { return mutedStyle.Render(truncateTail(sess.Cwd, room)) }
	return fitColumns(heads, []string{state}, width) + "\n" +
		factRow("group", plainValue(group), started, width) + "\n" +
		factRow("dir", dir, usage, width)
}

// viewGroupDetail heads the content column for a group: its name, how many
// agents sit under it, where they start, and what they are all doing.
func (m *Model) viewGroupDetail(group string, width int) string {
	count := m.groupSessionCount(group)
	countLabel := fmt.Sprintf("%d agents", count)
	if count == 1 {
		countLabel = "1 agent"
	}
	title := lipgloss.NewStyle().Foreground(colorAccent2).Bold(true).Render(displayGroup(group))
	head := fitColumns([]string{title + "  " + chipStyle.Render(countLabel), title}, []string{""}, width)

	if m.renamingGroup(group) {
		pathLabel := labelStyle
		if m.rename.focus == 1 {
			pathLabel = lipgloss.NewStyle().Foreground(colorAccent)
		}
		worktreeLabel := labelStyle
		if m.rename.focus == 2 {
			worktreeLabel = lipgloss.NewStyle().Foreground(colorAccent)
		}
		if fieldWidth := width - 12; fieldWidth >= 10 {
			m.rename.dir.Width = fieldWidth
		}
		out := head + "\n" + pathLabel.Width(10).Render("path") + m.rename.dir.View()
		if m.rename.focus == 1 && m.pathSugg.active() {
			out += "\n" + m.viewPathSuggestions()
		}
		out += "\n" + worktreeLabel.Width(10).Render("worktree") +
			subtleStyle.Render("◂ ") + valueStyle.Render(groupWorktreeOptions[m.rename.worktreeIndex]) + subtleStyle.Render(" ▸")
		return out
	}

	path := m.groupPaths[group]
	source := ""
	if path == "" {
		path = m.groupDefaultDir(group)
		source = subtleStyle.Render("inherited")
	}
	dir := func(room int) string { return mutedStyle.Render(truncateTail(path, room)) }
	lines := []string{head, factRow("dir", dir, source, width)}
	if breakdown := m.groupStatusBreakdown(group); breakdown != "" {
		lines = append(lines, factRow("state", trimmedValue(breakdown), "", width))
	}
	return strings.Join(lines, "\n")
}

// rosterToolColumn is the column a roster's tool names start at, so the
// roster reads as a table rather than as ragged pairs, and rosterNameMin
// the width under which a name column stops being worth reading.
const (
	rosterToolColumn = 14
	rosterNameMin    = 8
)

// viewGroupAgents lists a group's sessions where a session's pane preview
// would sit, so a group reads as a roster: one row per agent, its name, the
// CLI running it, and what it is doing.
func (m *Model) viewGroupAgents(group string, width, height int) string {
	total := m.groupSessionCount(group)
	if total == 0 {
		return subtleStyle.Render("agents") + "\n" +
			mutedStyle.Render("(none yet — press space to spawn one)")
	}

	type rosterRow struct{ name, tool, state string }
	var rows []rosterRow
	overflow := ""
	shown := 0
	for _, sess := range m.listedSessions() {
		if !inGroupSubtree(sess.Group, group) {
			continue
		}
		if shown >= height-2 && total > shown+1 {
			overflow = subtleStyle.Render(fmt.Sprintf("… %d more", total-shown))
			break
		}
		tint := lipgloss.NewStyle().Foreground(statusColor(sess.Status))
		rows = append(rows, rosterRow{
			name:  tint.Render(statusGlyph(sess.Status)) + " " + valueStyle.Render(sess.Name),
			tool:  subtleStyle.Render(sess.Tool),
			state: tint.Render(statusLabel(sess.Status)) + subtleStyle.Render(" · "+relSince(lastActivity(sess))),
		})
		shown++
	}

	// The name column is as wide as the longest name allows, bounded by what
	// the tool and state columns need, so tools land on one column and the
	// states share a right edge. A column too narrow for all three gives up
	// the tool first and the state second: a roster of names still answers
	// "who is in this group".
	nameWidth, toolWidth, stateWidth := rosterToolColumn, 0, 0
	for _, row := range rows {
		if w := ansi.StringWidth(row.name) + 2; w > nameWidth {
			nameWidth = w
		}
		if w := ansi.StringWidth(row.tool); w > toolWidth {
			toolWidth = w
		}
		if w := ansi.StringWidth(row.state); w > stateWidth {
			stateWidth = w
		}
	}
	showTool, showState := true, true
	if width-toolWidth-stateWidth-3 < rosterNameMin {
		showTool, toolWidth = false, 0
	}
	if width-stateWidth-2 < rosterNameMin {
		showState, stateWidth = false, 0
	}
	if room := width - toolWidth - stateWidth - 3; nameWidth > room {
		nameWidth = max(room, rosterNameMin)
	}

	head := padRight(subtleStyle.Render("agent"), nameWidth)
	if showTool {
		head += subtleStyle.Render("tool")
	}
	activity := ""
	if showState {
		activity = subtleStyle.Render("last activity")
	}
	lines := []string{rowColumns(head, activity, width)}
	for _, row := range rows {
		// A name trimmed to the column exactly would touch the tool beside
		// it, so the trim leaves the column's last cell as the gap.
		line := padRight(ansi.Truncate(row.name, max(nameWidth-1, 1), "…"), nameWidth)
		if showTool {
			line += row.tool
		}
		state := row.state
		if !showState {
			state = ""
		}
		lines = append(lines, rowColumns(line, state, width))
	}
	if overflow != "" {
		lines = append(lines, overflow)
	}
	return strings.Join(lines, "\n")
}

// lastActivity is when a session last changed state: the agent answering,
// finishing, erroring, or the moment a prompt set it working. It is what
// "how long since anything happened here" means to someone scanning the
// rail, where uptime says nothing about whether an agent is stuck.
func lastActivity(sess store.Session) time.Time {
	if sess.LastStatusAt.IsZero() {
		return sess.CreatedAt
	}
	return sess.LastStatusAt
}

// viewQuickBar is the docked prompt: enter answers the selected session, or
// spawns a fresh agent when a group is selected.
func (m *Model) viewQuickBar(width int) string {
	label := func(text string) string { return labelStyle.Render(padRight(text, detailLabelWidth)) }
	target := rowColumns(label("target")+mutedStyle.Render("no selection"), "", width)
	if entry, ok := m.selectedRow(); ok {
		if entry.isGroup {
			// Spawning: the tool and the worktree choice decide what gets
			// created, so they sit where the eye lands before typing.
			worktree := subtleStyle.Render("worktree off")
			switch {
			case !m.worktreeCapable(m.quickTargetDir()):
				worktree = subtleStyle.Render(worktreeUnavailable)
			case m.quickWorktreeOn():
				worktree = lipgloss.NewStyle().Foreground(colorAccent2).Render("worktree on")
			}
			tool := chipStyle.Render(m.quickTool())
			target = fitColumns(
				[]string{label("new") + lipgloss.NewStyle().Foreground(colorAccent2).Render(displayGroup(entry.group))},
				[]string{tool + " " + worktree, tool, ""}, width)
		} else {
			sess := entry.sess
			state := lipgloss.NewStyle().Foreground(statusColor(sess.Status)).
				Render(statusGlyph(sess.Status) + " " + statusLabel(sess.Status))
			target = fitColumns(
				[]string{label("answer") + lipgloss.NewStyle().Foreground(colorBright).Bold(true).Render(sess.Name)},
				[]string{state + " " + chipStyle.Render(sess.Tool), state, ""}, width)
		}
	}
	m.quick.input.SetWidth(width)
	m.quick.input.SetHeight(m.quickBarRows(width - 2))
	// Chips are tokens inside the typed text, so they wrap and reflow with
	// the words around them; painting happens on the rendered prompt.
	return target + "\n" + m.renderQuickChips(m.quick.input.View())
}

// viewHeaderRows is the full-width band over both columns: the wordmark
// on the left, and the richest reading of the fleet that fits set against
// the right edge (scope and rollup, then a compact rollup, then the scope
// alone).
func (m *Model) viewHeaderRows() []string {
	left := m.viewBanner()[0]
	if m.update.latest != "" {
		left += subtleStyle.Render("  ") +
			lipgloss.NewStyle().Foreground(colorAccent).Render("↑ "+m.update.latest+" available")
	}
	sep := subtleStyle.Render("   ")
	scope := m.headerScope()
	agents := m.headerAgents()
	for _, right := range []string{
		joinHeaderPieces(sep, scope, m.viewStatusCounts(false), agents),
		joinHeaderPieces(sep, scope, m.viewStatusCounts(true), agents),
		joinHeaderPieces(sep, scope, m.viewStatusCounts(true), ""),
		scope,
		"",
	} {
		gap := m.width - railGutter - ansi.StringWidth(left) - ansi.StringWidth(right)
		if right == "" || gap < 2 {
			continue
		}
		return []string{left + strings.Repeat(" ", gap) + right + strings.Repeat(" ", railGutter)}
	}
	return []string{left}
}

// joinHeaderPieces joins the header's non-empty readings with a separator.
func joinHeaderPieces(sep string, pieces ...string) string {
	kept := pieces[:0:0]
	for _, piece := range pieces {
		if piece != "" {
			kept = append(kept, piece)
		}
	}
	return strings.Join(kept, sep)
}

// headerScope names what the list is showing. The count is of the same
// sessions the rollup beside it breaks down, so the two lines always add
// up; counting painted rows instead would drop everything folded inside
// a collapsed group.
func (m *Model) headerScope() string {
	count := len(m.listedSessions())
	label := " sessions"
	if count == 1 {
		label = " session"
	}
	scope := subtleStyle.Render(" · active")
	if m.showArchived {
		scope = subtleStyle.Render(" · ") + scopeBadgeStyle.Render("ARCHIVED")
	}
	if m.statusFilter.active() {
		scope += subtleStyle.Render(" · ") +
			scopeBadgeStyle.Render(strings.ToUpper(m.statusFilter.label()))
	}
	return valueStyle.Render(fmt.Sprintf("%d", count)) + subtleStyle.Render(label) + scope
}

// headerAgents is the fleet's process cost as shares of this machine,
// empty when nothing is running. RAM shows both percent and absolute size.
func (m *Model) headerAgents() string {
	if m.agents.count == 0 {
		return ""
	}
	title := lipgloss.NewStyle().Foreground(colorBright).Bold(true).Render("agents total usage:")
	return title + " " +
		labelStyle.Render("cpu ") + valueStyle.Render(fmt.Sprintf("%.0f%%", m.agents.cpu)) +
		subtleStyle.Render(" · ") + labelStyle.Render("ram ") +
		valueStyle.Render(fmt.Sprintf("%.0f%%", m.agents.ram)) +
		subtleStyle.Render(" · ") + valueStyle.Render(humanBytes(m.agents.rss))
}

// viewStatusCounts is the fleet-at-a-glance strip: a tinted dot and count
// per state present among the listed sessions.
func (m *Model) viewStatusCounts(compact bool) string {
	counts := map[string]int{}
	for _, sess := range m.listedSessions() {
		counts[sess.Status]++
	}
	var parts []string
	for _, st := range []string{status.Waiting, status.Working, status.Finished, status.Idle, status.Errored, status.Dead} {
		if counts[st] == 0 {
			continue
		}
		dot := lipgloss.NewStyle().Foreground(statusColor(st)).Render(statusGlyph(st))
		label := fmt.Sprintf(" %d %s", counts[st], st)
		if compact {
			label = fmt.Sprintf(" %d", counts[st])
		}
		parts = append(parts, dot+subtleStyle.Render(label))
	}
	return strings.Join(parts, subtleStyle.Render("  "))
}
