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
)

// viewListFrame is the sessions rail beside the session content, both
// painted surfaces rather than drawn panels.
func (m *Model) viewListFrame() string {
	leftWidth, rightWidth := m.splitWidths()
	footer := m.viewFooter()
	bodyHeight := m.listBodyHeight()

	// One column goes to the seam between the rail and the content.
	contentWidth := rightWidth - 1

	frame := []string{}
	for _, line := range m.viewHeaderRows() {
		frame = append(frame, paint(line, m.width, backdropHex()))
	}
	frame = append(frame, paint(hrule(m.width), m.width, backdropHex()))
	frame = append(frame, joinColumns(
		paintRows(m.railLines(leftWidth, bodyHeight), leftWidth, bodyHeight, panelHex()),
		m.vruleColumn(bodyHeight),
		paintContent(m.contentLines(contentWidth-2*contentGutter, bodyHeight), contentWidth, bodyHeight, backdropHex()),
	)...)
	frame = append(frame,
		paint(hrule(m.width), m.width, backdropHex()),
		paint(m.viewStatus(), m.width, backdropHex()),
	)
	for _, line := range splitLines(footer) {
		frame = append(frame, paint(line, m.width, backdropHex()))
	}
	return strings.Join(frame, "\n")
}

// railLines is the sessions rail: the entry list on top, the machine
// meters docked at the bottom.
func (m *Model) railLines(width, height int) []string {
	meters := m.computerLines(width)
	listHeight := height - len(meters)
	if listHeight < 3 {
		listHeight, meters = height, nil
	}
	lines := append([]string{""}, m.entryLines(width, listHeight-1)...)
	for len(lines) < listHeight {
		lines = append(lines, "")
	}
	return append(lines[:listHeight], meters...)
}

// entryLines renders the visible slice of the tree. Entries are two lines
// tall, so the window is measured in lines rather than rows.
func (m *Model) entryLines(width, height int) []string {
	if len(m.rows) == 0 {
		return m.emptyRailLines(width)
	}
	heights := make([]int, len(m.rows))
	for i := range heights {
		heights[i] = m.entryHeight()
	}
	start, end := lineWindow(heights, m.cursor, height)

	var lines []string
	if start > 0 {
		lines = append(lines, subtleStyle.Render(strings.Repeat(" ", railGutter)+fmt.Sprintf("↑ %d more", start)))
	}
	for i := start; i < end; i++ {
		lines = append(lines, splitLines(m.renderTreeRow(m.rows[i], i == m.cursor, width))...)
	}
	if end < len(m.rows) {
		lines = append(lines, subtleStyle.Render(strings.Repeat(" ", railGutter)+fmt.Sprintf("↓ %d more", len(m.rows)-end)))
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return lines
}

// entryHeight is how many lines an entry paints. Every entry is the same
// height, groups included: a ragged list of one- and two-line rows reads
// as gaps rather than as rhythm.
func (m *Model) entryHeight() int { return 2 }

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

func (m *Model) emptyRailLines(width int) []string {
	title := "no sessions yet"
	hint := keyCap("n", "starts one")
	if m.showArchived {
		title = "nothing archived"
		hint = keyCap("t", "back to active")
	}
	if strings.TrimSpace(m.search) != "" {
		title = "no matches"
		hint = subtleStyle.Render("for \"" + m.search + "\"")
	}
	pad := strings.Repeat(" ", railGutter)
	return []string{"", pad + mutedStyle.Render(title), "", pad + hint}
}

// renderTreeRow paints one entry: a status dot, the name, and a quiet
// second line carrying the state and tool. The selected entry lifts onto
// its own band instead of wearing a marker.
func (m *Model) renderTreeRow(entry treeRow, selected bool, width int) string {
	pad := strings.Repeat(" ", railGutter)
	indent := strings.Repeat("  ", entry.depth)

	if m.renamingRow(entry) {
		line := pad + indent + m.renameRowInput(entry, width-railGutter-ansi.StringWidth(indent))
		return paint(line, width, selectedHex())
	}

	if entry.isGroup {
		return m.renderGroupEntry(entry, selected, width, pad, indent)
	}
	return m.renderSessionEntry(entry, selected, width, pad, indent)
}

func (m *Model) renderSessionEntry(entry treeRow, selected bool, width int, pad, indent string) string {
	sess := entry.sess
	dot := lipgloss.NewStyle().Foreground(statusColor(sess.Status)).Render(statusGlyph(sess.Status))
	nameStyle := valueStyle
	if selected {
		nameStyle = lipgloss.NewStyle().Foreground(colorBright).Bold(true)
	}
	head := pad + indent + dot + " " + nameStyle.Render(sess.Name)

	metaStyle := subtleStyle
	if selected {
		metaStyle = mutedStyle
	}
	age := metaStyle.Render(relSince(lastActivity(sess)))
	meta := pad + indent + "  " +
		lipgloss.NewStyle().Foreground(statusColor(sess.Status)).Render(statusLabel(sess.Status)) +
		metaStyle.Render(" · "+sess.Tool)

	bg := panelHex()
	if selected {
		bg = selectedHex()
	}
	return paint(rowColumns(head, age, width-railGutter), width, bg) + "\n" + paint(meta, width, bg)
}

func (m *Model) renderGroupEntry(entry treeRow, selected bool, width int, pad, indent string) string {
	marker := "▾"
	if m.collapsed[entry.group] {
		marker = "▸"
	}
	nameStyle := lipgloss.NewStyle().Foreground(colorAccent2).Bold(true)
	if selected {
		nameStyle = nameStyle.Foreground(colorBright)
	}
	count := m.groupSessionCount(entry.group)
	countLabel := fmt.Sprintf("%d agents", count)
	if count == 1 {
		countLabel = "1 agent"
	}
	head := pad + indent + subtleStyle.Render(marker) + " " + nameStyle.Render(baseName(entry.group))

	// The second line carries what the group is doing, so a folded group
	// still reports its subtree without being opened.
	meta := m.groupStatusBreakdown(entry.group)
	if meta == "" {
		meta = subtleStyle.Render("no agents yet")
	}

	bg := panelHex()
	if selected {
		bg = selectedHex()
	}
	return paint(rowColumns(head, subtleStyle.Render(countLabel), width-railGutter), width, bg) + "\n" +
		paint(pad+indent+"  "+meta, width, bg)
}

// computerLines is the machine block docked at the rail's foot: a label
// and one thin meter per resource.
func (m *Model) computerLines(width int) []string {
	snap := m.snap
	pad := strings.Repeat(" ", railGutter)
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

	lines := []string{hrule(width), pad + subtleStyle.Render("computer")}
	lines = append(lines,
		meter("cpu", snap.CPUPercent, snap.CPUOK, ""),
		meter("mem", snap.MemPercent, snap.MemOK, humanBytes(snap.MemUsed)+"/"+humanBytes(snap.MemTotal)),
	)
	if snap.SwapOK && snap.SwapTotal > 0 {
		lines = append(lines, meter("swap", snap.SwapPercent, true, humanBytes(snap.SwapUsed)))
	}
	lines = append(lines, meter("disk", snap.DiskPercent, snap.DiskOK,
		humanBytes(snap.DiskTotal-snap.DiskUsed)+" free"))
	if m.netRates {
		lines = append(lines, pad+labelStyle.Width(5).Render("net")+
			valueStyle.Render("↓ "+humanBytes(m.netDown)+"/s")+
			subtleStyle.Render("  ↑ "+humanBytes(m.netUp)+"/s"))
	}
	return append(lines, "")
}

// contentLines is the right column: what the cursor is on, then its live
// pane, with the quick prompt docked at the foot when it is open.
func (m *Model) contentLines(width, height int) []contentLine {
	gutter := strings.Repeat(" ", contentGutter)
	ours := func(lines []string) []contentLine {
		out := make([]contentLine, len(lines))
		for i, line := range lines {
			out[i] = contentLine{text: gutter + line}
		}
		return out
	}

	var bar []contentLine
	if m.quick.active {
		bar = append([]contentLine{{}}, ours(splitLines(m.viewQuickBar(width)))...)
	}
	body := ours(splitLines(m.viewDetail(width)))
	rest := height - len(body) - len(bar) - 1
	if rest >= 3 {
		body = append(body, contentLine{text: hrule(width)})
		if group, ok := m.selectedGroup(); ok {
			body = append(body, ours(splitLines(m.viewGroupAgents(group, width, rest)))...)
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
// marked raw: painting our backdrop behind an agent's own CLI colors would
// replace the background it drew itself, so those rows keep the terminal's.
func (m *Model) previewLines(width, height int, gutter string) []contentLine {
	lines := []contentLine{{text: gutter + subtleStyle.Render("preview")}}
	rows := height - 1
	if rows < 1 {
		return lines
	}
	pane := paneExact(m.preview, rows)
	if len(pane) == 0 {
		return append(lines, contentLine{text: gutter + mutedStyle.Render("(no output yet)")})
	}
	for _, line := range pane {
		lines = append(lines, contentLine{text: gutter + previewLine(line, width), raw: true})
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
		facts = append(facts, fmt.Sprintf("cpu %.1f%% · ram %s", m.proc.CPUPercent, humanBytes(m.proc.RSS)))
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
	// Chips live in the textarea prompt so they sit on the same line as the
	// typed text (Claude-style inline chips), not on a separate strip below.
	m.syncQuickInlineChips()
	m.quick.input.SetWidth(width)
	m.quick.input.SetHeight(m.quickBarRows(width - 2))
	return subtleStyle.Render(target) + "\n" + m.quick.input.View()
}

// viewHeaderRows is the header block: the wordmark when there is room for
// it, with the scope line and the fleet rollup set against the right edge
// on the wordmark's own rows.
func (m *Model) viewHeaderRows() []string {
	if !m.showBanner() {
		return []string{m.viewHeader()}
	}
	// Each header row offers its readings richest-first and takes the
	// widest one that still clears the wordmark.
	right := [][]string{
		{m.headerScope()},
		{
			joinHeaderRight(m.viewStatusCounts(false), m.headerAgents()),
			joinHeaderRight(m.viewStatusCounts(true), m.headerAgents()),
			m.viewStatusCounts(true),
		},
	}
	rows := m.viewBanner()
	for i := range rows {
		if i >= len(right) {
			continue
		}
		for _, text := range right[i] {
			if text == "" {
				continue
			}
			gap := m.width - railGutter - ansi.StringWidth(rows[i]) - ansi.StringWidth(text)
			if gap >= 2 {
				rows[i] += strings.Repeat(" ", gap) + text
				break
			}
		}
	}
	return rows
}

// headerScope names what the list is showing and badges a newer release.
func (m *Model) headerScope() string {
	scope := "active"
	if m.showArchived {
		scope = "archived"
	}
	sessionCount := 0
	for _, entry := range m.rows {
		if !entry.isGroup {
			sessionCount++
		}
	}
	line := valueStyle.Render(fmt.Sprintf("%d", sessionCount)) + subtleStyle.Render(" "+scope)
	if m.updateLatest != "" {
		line += subtleStyle.Render("   ") +
			lipgloss.NewStyle().Foreground(colorAccent).Render("↑ "+m.updateLatest+" available")
	}
	return line
}

// headerAgents is the fleet's process cost, empty when nothing is running.
func (m *Model) headerAgents() string {
	if m.agents.count == 0 {
		return ""
	}
	return labelStyle.Render("cpu ") + valueStyle.Render(fmt.Sprintf("%.0f%%", m.agents.cpu)) +
		subtleStyle.Render(" · ") + labelStyle.Render("ram ") + valueStyle.Render(humanBytes(m.agents.rss))
}

// viewHeader is the single-line header used when the terminal is too narrow
// for the wordmark: the product mark, what is on screen, and the fleet
// rollup pushed to the right edge.
func (m *Model) viewHeader() string {
	left := strings.Repeat(" ", railGutter) +
		lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(bannerWord) +
		subtleStyle.Render("   ") + m.headerScope()

	agents := m.headerAgents()
	for _, right := range []string{
		joinHeaderRight(m.viewStatusCounts(false), agents),
		joinHeaderRight(m.viewStatusCounts(true), agents),
		joinHeaderRight(m.viewStatusCounts(true), ""),
		"",
	} {
		if right == "" {
			break
		}
		gap := m.width - 2*railGutter - ansi.StringWidth(left) - ansi.StringWidth(right)
		if gap >= 2 {
			return left + strings.Repeat(" ", gap) + right
		}
	}
	return left
}

func joinHeaderRight(counts, agents string) string {
	switch {
	case counts == "":
		return agents
	case agents == "":
		return counts
	default:
		return counts + subtleStyle.Render("   ") + agents
	}
}

// viewStatusCounts is the fleet-at-a-glance strip: a tinted dot and count
// per state present among the listed sessions.
func (m *Model) viewStatusCounts(compact bool) string {
	counts := map[string]int{}
	for _, sess := range m.sessions {
		if !sess.Archived {
			counts[sess.Status]++
		}
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
