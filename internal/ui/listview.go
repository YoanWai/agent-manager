package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
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
	railRows := m.railLines(railWidth, bodyHeight)
	contentRows := m.contentLines(bleedWidth, bodyHeight)
	seam := make([]string, bodyHeight)
	edge := make([]string, bodyHeight)
	for i := range seam {
		leftRule := i < len(railRows) && railRows[i].rule
		rightRule := i < len(contentRows) && contentRows[i].rule
		seam[i] = m.seamCell(leftRule, rightRule)
		tone := panelHex()
		if i < len(railRows) && railRows[i].tone != "" {
			tone = railRows[i].tone
		}
		edge[i] = railEdgeCell(tone)
	}
	frame = append(frame, m.boundedRuleRow(leftWidth+1, m.width, "▀"))
	frame = append(frame, joinColumns(
		edge,
		paintContent(railRows, railWidth, bodyHeight, panelHex()),
		seam,
		m.bleedColumn(bodyHeight),
		paintContent(contentRows, bleedWidth, bodyHeight, backdropHex()),
	)...)
	frame = append(frame,
		m.boundedRuleRow(leftWidth+1, m.width, "▄"),
		paint(m.viewStatus(), m.width, backdropHex()),
	)
	for _, line := range splitLines(footer) {
		frame = append(frame, paint(line, m.width, backdropHex()))
	}
	return strings.Join(frame, "\n")
}

// railLines is the sessions rail: the entry list on top, the machine
// meters docked at the bottom behind their seam.
func (m *Model) railLines(width, height int) []contentLine {
	meters := m.computerLines(width)
	listHeight := height - len(meters) - 1
	if listHeight < 3 {
		listHeight, meters = height, nil
	}
	var rows []contentLine
	// A populated list keeps a breath of air at the top; the empty state
	// centers itself in the full list area instead.
	if len(m.rows) > 0 {
		rows = append(rows, contentLine{})
	}
	if m.showArchived {
		if len(rows) == 0 {
			rows = append(rows, contentLine{})
		}
		rows = append(rows,
			contentLine{text: strings.Repeat(" ", railInset) + scopeBadgeStyle.Render("ARCHIVED") +
				subtleStyle.Render("  ") + keyCap("t", "back to active")},
			contentLine{})
	}
	rows = append(rows, m.entryLines(width, listHeight-len(rows))...)
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
	if len(m.rows) == 0 {
		var lines []contentLine
		for _, line := range m.emptyRailLines(width, height) {
			lines = append(lines, contentLine{text: line})
		}
		return lines
	}
	heights := make([]int, len(m.rows))
	for i := range heights {
		heights[i] = entryHeight(m.rows[i])
	}
	start, end := lineWindow(heights, m.cursor, height)

	var lines []contentLine
	if start > 0 {
		lines = append(lines, contentLine{text: subtleStyle.Render(strings.Repeat(" ", railInset) + fmt.Sprintf("↑ %d more", start))})
	}
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
	if end < len(m.rows) {
		lines = append(lines, contentLine{text: subtleStyle.Render(strings.Repeat(" ", railInset) + fmt.Sprintf("↓ %d more", len(m.rows)-end))})
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return lines
}

// entryHeight is how many lines an entry paints. Every entry is one line,
// groups included: a ragged list of one- and two-line rows reads as gaps
// rather than as rhythm.
func entryHeight(treeRow) int { return 1 }

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
		continues := false
		for j := index + 1; j < len(m.rows); j++ {
			if m.rows[j].depth < slot {
				break
			}
			if m.rows[j].depth == slot {
				continues = true
				break
			}
		}
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

// renderTreeRow paints one entry: a status dot, the name, and what the
// entry is doing set against the row's far edge. The selected entry lifts
// onto its own band instead of wearing a marker.
func (m *Model) renderTreeRow(entry treeRow, selected bool, width, index int, bg string) string {
	pad := strings.Repeat(" ", railInset)
	guides := m.treeGuidesAt(index)

	if m.renamingRow(entry) {
		line := pad + guides + m.renameRowInput(entry, width-railGutter-ansi.StringWidth(guides))
		return paint(line, width, selectedHex())
	}

	if entry.isGroup {
		return m.renderGroupEntry(entry, selected, width, pad, guides, bg)
	}
	return m.renderSessionEntry(entry, selected, width, pad, guides, bg)
}

func (m *Model) renderSessionEntry(entry treeRow, selected bool, width int, pad, guides, bg string) string {
	sess := entry.sess
	dot := lipgloss.NewStyle().Foreground(statusColor(sess.Status)).Render(statusGlyph(sess.Status))
	nameStyle := valueStyle
	if selected {
		nameStyle = lipgloss.NewStyle().Foreground(colorBright).Bold(true)
	}
	head := pad + guides + dot + " " + nameStyle.Render(sess.Name)

	metaStyle := subtleStyle
	if selected {
		metaStyle = mutedStyle
	}
	// A session names its state in words as well as in its dot; a group,
	// whose row rolls several states together, is left to its dots.
	meta := lipgloss.NewStyle().Foreground(statusColor(sess.Status)).Render(statusLabel(sess.Status)) +
		metaStyle.Render(" · "+sess.Tool+" · "+relSince(lastActivity(sess)))

	return paint(rowColumns(head, meta, width-railGutter), width, bg)
}

func (m *Model) renderGroupEntry(entry treeRow, selected bool, width int, pad, guides, bg string) string {
	marker := "▾"
	if m.collapsed[entry.group] {
		marker = "▸"
	}
	nameStyle := lipgloss.NewStyle().Foreground(colorAccent2).Bold(true)
	if selected {
		nameStyle = nameStyle.Foreground(colorBright)
	}
	head := pad + guides + subtleStyle.Render(marker) + " " + nameStyle.Render(baseName(entry.group))

	// What the group is doing rides on the same line as its name, so a
	// folded group still reports its subtree without being opened. It is
	// written in dots rather than words: the counts state the size too.
	meta := m.groupStatusGlyphs(entry.group)
	if meta == "" {
		meta = subtleStyle.Render("no agents yet")
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
	if m.netRates {
		lines = append(lines, pad+labelStyle.Width(5).Render("net")+
			valueStyle.Render("↓ "+humanBytes(m.netDown)+"/s")+
			subtleStyle.Render("  ↑ "+humanBytes(m.netUp)+"/s"))
	}
	return append(lines, "")
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
		body = append(body, contentLine{rule: true})
		if group, ok := m.selectedGroup(); ok {
			body = append(body, ours(splitLines(m.viewGroupAgents(group, inner, rest)))...)
		} else {
			body = append(body, m.previewLines(width, rest, gutter)...)
		}
	}
	for len(body)+len(bar) < height {
		body = append(body, contentLine{})
	}
	return append(body[:max(height-len(bar), 0)], bar...)
}

// previewLines is the captured pane under its label. The captured rows are
// marked raw and drawn without the column's gutters: painting our backdrop
// behind an agent's own CLI colors would replace the background it drew
// itself, and insetting its output would put a margin around a terminal
// that has its own. Only the label, which is ours, keeps the gutter.
func (m *Model) previewLines(width, height int, gutter string) []contentLine {
	lines := []contentLine{
		{text: gutter + subtleStyle.Render("preview")},
		{raw: true},
	}
	rows := height - len(lines)
	if rows < 1 {
		return lines
	}
	pane := paneExact(m.preview, rows)
	if len(pane) == 0 {
		return append(lines, contentLine{text: gutter + mutedStyle.Render("(no output yet)")})
	}
	for _, line := range pane {
		lines = append(lines, contentLine{text: previewLine(line, width), raw: true})
	}
	// Rows past the capture stay raw too: a painted tail under unpainted
	// output would read as a box drawn around the agent's last line.
	for len(lines) < height {
		lines = append(lines, contentLine{raw: true})
	}
	return lines
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

	title := lipgloss.NewStyle().Foreground(colorBright).Bold(true).Render(sess.Name)
	state := lipgloss.NewStyle().Foreground(statusColor(sess.Status)).
		Render(statusGlyph(sess.Status)+" "+statusLabel(sess.Status)) +
		subtleStyle.Render(" · "+relSince(lastActivity(sess)))

	facts := []string{tool, displayGroup(sess.Group), "started " + relSince(sess.CreatedAt)}
	if m.procFor == sess.ID && m.proc.OK {
		facts = append(facts, fmt.Sprintf("cpu %.1f%% · ram %.1f%% · %s",
			m.proc.CPUPercent, m.proc.RamPercent, humanBytes(m.proc.RSS)))
	}
	meta := subtleStyle.Render(strings.Join(facts, " · "))
	dir := subtleStyle.Render(truncateTail(sess.Cwd, width))
	return rowColumns(title, state, width) + "\n" + meta + "\n" + dir
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
	head := rowColumns(title, subtleStyle.Render(countLabel), width)

	if m.renamingGroup(group) {
		label := labelStyle
		if m.rename.focus == 1 {
			label = lipgloss.NewStyle().Foreground(colorAccent)
		}
		if fieldWidth := width - 8; fieldWidth >= 10 {
			m.rename.dir.Width = fieldWidth
		}
		out := head + "\n" + label.Width(6).Render("path") + m.rename.dir.View()
		if m.rename.focus == 1 && m.pathSugg.active() {
			out += "\n" + m.viewPathSuggestions()
		}
		return out
	}

	path := m.groupPaths[group]
	source := ""
	if path == "" {
		path = m.groupDefaultDir(group)
		source = subtleStyle.Render(" · inherited")
	}
	lines := []string{head, subtleStyle.Render(truncateTail(path, width-len(source))) + source}
	if breakdown := m.groupStatusBreakdown(group); breakdown != "" {
		lines = append(lines, breakdown)
	}
	return strings.Join(lines, "\n")
}

// viewGroupAgents lists a group's sessions where a session's pane preview
// would sit, so a group reads as a roster.
func (m *Model) viewGroupAgents(group string, width, height int) string {
	lines := []string{subtleStyle.Render("agents")}
	shown, total := 0, m.groupSessionCount(group)
	if total == 0 {
		return lines[0] + "\n" + mutedStyle.Render("(none yet — press space to spawn one)")
	}
	for _, sess := range m.visibleSessions() {
		if !inGroupSubtree(sess.Group, group) {
			continue
		}
		if shown >= height-2 && total > shown+1 {
			lines = append(lines, subtleStyle.Render(fmt.Sprintf("… %d more", total-shown)))
			break
		}
		dot := lipgloss.NewStyle().Foreground(statusColor(sess.Status)).Render(statusGlyph(sess.Status))
		line := rowColumns(dot+" "+valueStyle.Render(sess.Name),
			subtleStyle.Render(sess.Tool+" · "+relSince(lastActivity(sess))), width)
		lines = append(lines, line)
		shown++
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
	target := "no selection"
	if entry, ok := m.selectedRow(); ok {
		if entry.isGroup {
			target = "new " + m.quickTool() + " agent in " + displayGroup(entry.group)
		} else {
			target = "answer " + entry.sess.Name
		}
	}
	m.quick.input.SetWidth(width)
	m.quick.input.SetHeight(m.quickBarRows(width - 2))
	// Chips are tokens inside the typed text, so they wrap and reflow with
	// the words around them; painting happens on the rendered prompt.
	return subtleStyle.Render(target) + "\n" + m.renderQuickChips(m.quick.input.View())
}

// viewHeaderRows is the full-width band over both columns: the wordmark
// on the left, and the richest reading of the fleet that fits set against
// the right edge (scope and rollup, then a compact rollup, then the scope
// alone).
func (m *Model) viewHeaderRows() []string {
	left := m.viewBanner()[0]
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

// headerScope names what the list is showing and badges a newer release.
// The count is of the same sessions the rollup beside it breaks down, so
// the two lines always add up; counting painted rows instead would drop
// everything folded inside a collapsed group.
func (m *Model) headerScope() string {
	count := len(m.visibleSessions())
	label := " sessions"
	if count == 1 {
		label = " session"
	}
	scope := subtleStyle.Render(" · active")
	if m.showArchived {
		scope = subtleStyle.Render(" · ") + scopeBadgeStyle.Render("ARCHIVED")
	}
	line := valueStyle.Render(fmt.Sprintf("%d", count)) + subtleStyle.Render(label) + scope
	if m.updateLatest != "" {
		line += subtleStyle.Render("   ") +
			lipgloss.NewStyle().Foreground(colorAccent).Render("↑ "+m.updateLatest+" available")
	}
	return line
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
	for _, sess := range m.visibleSessions() {
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
