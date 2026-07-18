package ui

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func (m *Model) View() string {
	if m.width == 0 {
		return "loading..."
	}
	switch m.mode {
	case modeForm:
		return m.viewForm()
	case modeHelp:
		return m.viewHelp()
	case modeSettings:
		return m.viewSettings()
	case modeMove:
		return m.viewMove()
	case modeGroupForm:
		return m.viewGroupForm()
	case modeDiff:
		return m.viewDiffFull()
	}

	leftWidth := m.width * 34 / 100
	if leftWidth < 30 {
		leftWidth = 30
	}
	rightWidth := m.width - leftWidth
	footer := m.viewFooter()
	bodyHeight := m.height - 4 - lipgloss.Height(footer)
	if bodyHeight < 3 {
		bodyHeight = 3
	}

	listInner := leftWidth - 4
	leftBody := m.viewList(listInner, bodyHeight-2)
	stats := m.viewComputer(listInner)
	listHeight := bodyHeight - 2 - lipgloss.Height(stats)
	if listHeight >= 3 {
		leftBody = padToHeight(m.viewList(listInner, listHeight), listHeight) + "\n" + stats
	}
	left := titledPanel("Sessions", leftBody, leftWidth, bodyHeight)
	right := titledPanel(m.sidebarTitle(), m.viewSidebar(rightWidth-4, bodyHeight-2), rightWidth, bodyHeight)
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	return strings.Join([]string{m.viewHeader(), "", body, m.viewStatus(), footer}, "\n")
}

// viewStatus is the transient message line: prompts, search, and
// self-dismissing errors. Keeps the footer free for key hints.
func (m *Model) viewStatus() string {
	switch {
	case m.mode == modeConfirmDelete:
		return padRight(errStyle.Render(" ⚠ "+m.confirm.label)+subtleStyle.Render("  y/n"), m.width)
	case m.searching:
		cursor := lipgloss.NewStyle().Foreground(colorAccent).Render("▏")
		line := keyStyle.Render(" search ") + valueStyle.Render(m.search) + cursor +
			subtleStyle.Render("  enter/esc to close")
		return padRight(line, m.width)
	case m.err != "":
		return padRight(errStyle.Render(" ✖ "+m.err), m.width)
	case m.diff.notice != "":
		return padRight(lipgloss.NewStyle().Foreground(colorFinished).Render(" ✔ "+m.diff.notice), m.width)
	default:
		return ""
	}
}

func (m *Model) viewHeader() string {
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
	brand := badgeStyle.Render("◆ Agent Manager")
	meta := mutedStyle.Render(fmt.Sprintf("%d sessions", sessionCount)) +
		subtleStyle.Render(" · ") +
		lipgloss.NewStyle().Foreground(colorAccent2).Render(scope)
	left := brand + "  " + meta

	right := m.viewStatusCounts()
	if m.agents.count > 0 {
		agents := labelStyle.Render("agents ") +
			valueStyle.Render(fmt.Sprintf("%.0f%%", m.agents.cpu)) +
			subtleStyle.Render(" · ") +
			valueStyle.Render(humanBytes(m.agents.rss))
		if right != "" {
			right += subtleStyle.Render("   ")
		}
		right += agents + " "
	}

	gap := m.width - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 1 {
		return padRight(left, m.width)
	}
	return left + strings.Repeat(" ", gap) + right
}

// viewStatusCounts is the fleet-at-a-glance strip: one colored glyph and
// count per status present among the listed sessions.
func (m *Model) viewStatusCounts() string {
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
		glyph := lipgloss.NewStyle().Foreground(statusColor(st)).Render(statusGlyph(st))
		parts = append(parts, glyph+mutedStyle.Render(fmt.Sprintf(" %d %s", counts[st], st)))
	}
	return strings.Join(parts, subtleStyle.Render("  "))
}

// viewComputer is the compact machine gauge block docked at the bottom
// of the Sessions panel: cpu, memory (with used/total), swap, root-disk
// free space, and network rates.
func (m *Model) viewComputer(width int) string {
	snap := m.snap
	meter := func(label string, percent float64, ok bool, extra string) string {
		if !ok {
			return labelStyle.Width(5).Render(label) + mutedStyle.Render("n/a") + "\n"
		}
		line := labelStyle.Width(5).Render(label) + gauge(percent, 8) +
			valueStyle.Render(fmt.Sprintf(" %3.0f%%", percent))
		if extra != "" {
			line += subtleStyle.Render(" " + extra)
		}
		return line + "\n"
	}
	var b strings.Builder
	b.WriteString(divider("Computer", width) + "\n")
	b.WriteString(meter("cpu", snap.CPUPercent, snap.CPUOK, ""))
	b.WriteString(meter("mem", snap.MemPercent, snap.MemOK,
		humanBytes(snap.MemUsed)+"/"+humanBytes(snap.MemTotal)))
	if snap.SwapOK && snap.SwapTotal > 0 {
		b.WriteString(meter("swap", snap.SwapPercent, true, humanBytes(snap.SwapUsed)))
	}
	b.WriteString(meter("disk", snap.DiskPercent, snap.DiskOK,
		humanBytes(snap.DiskTotal-snap.DiskUsed)+" free"))
	if m.netRates {
		b.WriteString(labelStyle.Width(5).Render("net") +
			valueStyle.Render("↓ "+humanBytes(m.netDown)+"/s") +
			subtleStyle.Render("  ↑ "+humanBytes(m.netUp)+"/s") + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m *Model) sidebarTitle() string {
	if sess, ok := m.selected(); ok {
		return "Session · " + sess.Name
	}
	if entry, ok := m.selectedRow(); ok && entry.isGroup {
		return "Group · " + displayGroup(entry.group)
	}
	return "Session"
}

func (m *Model) viewList(width, height int) string {
	if len(m.rows) == 0 {
		hint := "Press " + keyStyle.Render("n") + mutedStyle.Render(" to create a session.")
		if m.showArchived {
			hint = mutedStyle.Render("No archived sessions. ") + keyStyle.Render("t") + mutedStyle.Render(" goes back.")
		}
		if strings.TrimSpace(m.search) != "" {
			hint = mutedStyle.Render("No matches for ") + valueStyle.Render("\""+m.search+"\"")
		}
		return "\n  " + subtleStyle.Render("✦") + "  " + hint
	}

	start, end := scrollWindow(len(m.rows), m.cursor, height)
	var b strings.Builder
	if start > 0 {
		b.WriteString(subtleStyle.Render(fmt.Sprintf("  ↑ %d more", start)) + "\n")
	}
	for i := start; i < end; i++ {
		b.WriteString(m.renderTreeRow(m.rows[i], i == m.cursor, width) + "\n")
	}
	if end < len(m.rows) {
		b.WriteString(subtleStyle.Render(fmt.Sprintf("  ↓ %d more", len(m.rows)-end)))
	}
	return strings.TrimRight(b.String(), "\n")
}

// treeGuides draws the dim vertical guides for a row's ancestor levels.
func treeGuides(depth int) string {
	if depth <= 0 {
		return ""
	}
	return subtleStyle.Render(strings.Repeat("│ ", depth))
}

// inGroupSubtree reports whether a session's group sits at or below the
// given group in the tree.
func inGroupSubtree(sessGroup, group string) bool {
	return sessGroup == group || strings.HasPrefix(sessGroup, group+"/")
}

func (m *Model) groupSessionCount(path string) int {
	count := 0
	for _, sess := range m.visibleSessions() {
		if inGroupSubtree(sess.Group, path) {
			count++
		}
	}
	return count
}

// scrollWindow keeps the cursor visible inside a height-limited window,
// reserving one line for each overflow indicator when needed.
func scrollWindow(total, cursor, height int) (int, int) {
	if total <= height {
		return 0, total
	}
	visible := height - 2
	if visible < 1 {
		visible = 1
	}
	start := cursor - visible/2
	if start < 0 {
		start = 0
	}
	if start+visible > total {
		start = total - visible
	}
	return start, start + visible
}

func (m *Model) renderTreeRow(entry treeRow, selected bool, width int) string {
	bar := " "
	if selected {
		bar = lipgloss.NewStyle().Foreground(colorAccent).Render("▎")
	}
	guides := treeGuides(entry.depth)

	if m.renamingRow(entry) {
		line := bar + " " + guides + m.renameRowInput(entry, width-2-ansi.StringWidth(guides))
		return selectedRowStyle.Render(padRight(line, width))
	}

	var content string
	if entry.isGroup {
		marker := "▾"
		if m.collapsed[entry.group] {
			marker = "▸"
		}
		count := subtleStyle.Render(fmt.Sprintf(" (%d)", m.groupSessionCount(entry.group)))
		name := lipgloss.NewStyle().Foreground(colorAccent2).Bold(true).Render(baseName(entry.group))
		content = subtleStyle.Render(marker) + " " + name + count + m.groupStatusGlyphs(entry.group)
	} else {
		sess := entry.sess
		glyph := lipgloss.NewStyle().Foreground(statusColor(sess.Status)).Render(statusGlyph(sess.Status))
		nameStyle := valueStyle
		if selected {
			nameStyle = lipgloss.NewStyle().Foreground(colorBright).Bold(true)
		}
		name := nameStyle.Render(sess.Name)
		state := lipgloss.NewStyle().Foreground(statusColor(sess.Status)).Render(sess.Status)
		meta := state + subtleStyle.Render(" · "+sess.Tool+" · "+relTime(sess.CreatedAt))
		content = glyph + " " + name + "  " + meta
	}

	line := bar + " " + guides + content
	if ansi.StringWidth(line) > width {
		line = ansi.Truncate(line, width-1, "…") + "\x1b[0m"
	}
	line = padRight(line, width)
	if selected {
		return selectedRowStyle.Render(line)
	}
	return line
}

func (m *Model) renamingGroup(group string) bool {
	return m.mode == modeRename && m.rename.isGroup && m.rename.path == group
}

func (m *Model) renamingRow(entry treeRow) bool {
	if entry.isGroup {
		return m.renamingGroup(entry.group)
	}
	return m.mode == modeRename && !m.rename.isGroup && entry.sess.ID == m.rename.sessID
}

// renameRowInput renders the inline name editor in place of the row's
// label, keeping the row's glyph so the edit reads in context.
func (m *Model) renameRowInput(entry treeRow, width int) string {
	lead := subtleStyle.Render("▾")
	if !entry.isGroup {
		lead = lipgloss.NewStyle().Foreground(statusColor(entry.sess.Status)).
			Render(statusGlyph(entry.sess.Status))
	}
	if fieldWidth := width - 4; fieldWidth >= 5 {
		m.rename.input.Width = fieldWidth
	}
	return lead + " " + m.rename.input.View()
}

// divider renders a labeled section rule that fills the given width.
func divider(label string, width int) string {
	head := sectionStyle.Render(label) + " "
	dashes := width - ansi.StringWidth(label) - 1
	if dashes < 0 {
		dashes = 0
	}
	return head + subtleStyle.Render(strings.Repeat("─", dashes))
}

// viewSidebar lays out session details on top, with the live preview
// filling the rest of the panel below. The quick bar, when active, docks
// at the very bottom in the same spot for sessions and groups alike.
func (m *Model) viewSidebar(width, height int) string {
	bar := ""
	if m.quick.active {
		bar = m.viewQuickBar(width)
		if height -= lipgloss.Height(bar) + 1; height < 3 {
			height = 3
		}
	}
	detail := divider("Details", width) + "\n" + m.viewDetail(width)
	body := detail
	if rest := height - lipgloss.Height(detail) - 1; rest >= 3 {
		if group, ok := m.selectedGroup(); ok {
			body = detail + "\n" + m.viewGroupAgents(group, width, rest)
		} else {
			body = detail + "\n" + m.viewPreview(width, rest)
		}
	}
	if bar == "" {
		return body
	}
	return padToHeight(body, height) + "\n" + bar
}

// viewQuickBar is the docked prompt input: enter answers the selected
// session, or spawns a fresh agent when a group row is selected.
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
	return divider("Quick Prompt · "+target, width) + "\n" + m.quick.input.View()
}

const quickBarMaxRows = 5

// quickBarRows is the rows the typed text needs at the current width,
// capped so the bar never swallows the sidebar. Single-line values (the
// normal case) count exact soft-wrap rows; pasted multi-line values are
// estimated, with the textarea scrolling to keep the cursor visible.
func (m *Model) quickBarRows(textWidth int) int {
	rows := 0
	if m.quick.input.LineCount() == 1 {
		rows = m.quick.input.LineInfo().Height
	} else {
		if textWidth < 1 {
			textWidth = 1
		}
		for _, line := range strings.Split(m.quick.input.Value(), "\n") {
			rows += 1 + (max(lipgloss.Width(line), 1)-1)/textWidth
		}
	}
	if rows > quickBarMaxRows {
		rows = quickBarMaxRows
	}
	if rows < 1 {
		rows = 1
	}
	return rows
}

func (m *Model) selectedGroup() (string, bool) {
	if entry, ok := m.selectedRow(); ok && entry.isGroup {
		return entry.group, true
	}
	return "", false
}

func (m *Model) viewDetail(width int) string {
	sess, ok := m.selected()
	if !ok {
		if group, isGroup := m.selectedGroup(); isGroup {
			return m.viewGroupDetail(group, width)
		}
		return "\n" + mutedStyle.Render("Select a session to inspect it.")
	}
	var b strings.Builder
	b.WriteString(pill(sess.Status, statusColor(sess.Status)) + "  " +
		pill(sess.Tool, colorAccent) + "\n")
	b.WriteString(kv("group", displayGroup(sess.Group)))
	b.WriteString(kv("dir", truncateTail(sess.Cwd, width-8)))
	b.WriteString(kv("age", relTime(sess.CreatedAt)))
	if m.procFor == sess.ID && m.proc.OK {
		b.WriteString(kv("proc", fmt.Sprintf("%.1f%% · %s", m.proc.CPUPercent, humanBytes(m.proc.RSS))))
	}
	return b.String()
}

// viewGroupDetail fills the details panel for a selected group: default
// path (own or inherited), direct subgroup count, and a status breakdown
// of every agent in the subtree.
func (m *Model) viewGroupDetail(group string, width int) string {
	var b strings.Builder
	count := m.groupSessionCount(group)
	countLabel := fmt.Sprintf("%d agents", count)
	if count == 1 {
		countLabel = "1 agent"
	}
	b.WriteString(pill("group", colorAccent2) + "  " + pill(countLabel, colorAccent) + "\n")

	if m.renamingGroup(group) {
		label := labelStyle
		if m.rename.focus == 1 {
			label = lipgloss.NewStyle().Foreground(colorAccent)
		}
		if fieldWidth := width - 8; fieldWidth >= 10 {
			m.rename.dir.Width = fieldWidth
		}
		b.WriteString(label.Width(6).Render("path") + m.rename.dir.View() + "\n")
		if m.rename.focus == 1 && m.pathSugg.active() {
			b.WriteString(m.viewPathSuggestions() + "\n")
		}
	} else {
		path := m.groupPaths[group]
		source := ""
		if path == "" {
			path = m.groupDefaultDir(group)
			source = subtleStyle.Render(" · inherited")
		}
		b.WriteString(labelStyle.Width(6).Render("path") +
			valueStyle.Render(truncateTail(path, width-8)) + source + "\n")
	}

	if group != "" {
		b.WriteString(kv("group", displayGroup(parentGroup(group))))
	}
	if subgroups := m.directSubgroupCount(group); subgroups > 0 {
		b.WriteString(kv("subs", fmt.Sprintf("%d", subgroups)))
	}
	if breakdown := m.groupStatusBreakdown(group); breakdown != "" {
		b.WriteString(labelStyle.Width(6).Render("state") + breakdown + "\n")
	}
	return b.String()
}

func parentGroup(group string) string {
	if idx := strings.LastIndex(group, "/"); idx >= 0 {
		return group[:idx]
	}
	return ""
}

func (m *Model) directSubgroupCount(group string) int {
	count := 0
	for path := range groupClosure(m.groups, m.sessions) {
		if parentGroup(path) == group && path != group {
			count++
		}
	}
	return count
}

// groupStatusBreakdown renders "2 working · 1 waiting" for the subtree,
// each count tinted in its status color, skipping zero statuses.
func (m *Model) groupStatusBreakdown(group string) string {
	counts := m.groupStatusCounts(group)
	var parts []string
	for _, st := range []string{status.Working, status.Waiting, status.Finished, status.Errored, status.Idle, status.Dead} {
		if counts[st] > 0 {
			parts = append(parts, lipgloss.NewStyle().Foreground(statusColor(st)).
				Render(fmt.Sprintf("%d %s", counts[st], st)))
		}
	}
	return strings.Join(parts, subtleStyle.Render(" · "))
}

func (m *Model) groupStatusCounts(group string) map[string]int {
	counts := map[string]int{}
	for _, sess := range m.visibleSessions() {
		if inGroupSubtree(sess.Group, group) {
			counts[sess.Status]++
		}
	}
	return counts
}

// groupStatusGlyphs is the compact per-row rollup of a group subtree's
// live statuses (" ◐2 ?1"), idle omitted so quiet groups stay clean.
func (m *Model) groupStatusGlyphs(group string) string {
	counts := m.groupStatusCounts(group)
	var b strings.Builder
	for _, st := range []string{status.Working, status.Waiting, status.Finished, status.Errored, status.Dead} {
		if counts[st] > 0 {
			b.WriteString(" " + lipgloss.NewStyle().Foreground(statusColor(st)).
				Render(fmt.Sprintf("%s%d", statusGlyph(st), counts[st])))
		}
	}
	return b.String()
}

// viewGroupAgents lists the subtree's sessions where a session's pane
// preview would sit, so a group row reads as a group pane.
func (m *Model) viewGroupAgents(group string, width, height int) string {
	var b strings.Builder
	b.WriteString(divider("Agents", width) + "\n")
	shown, total := 0, m.groupSessionCount(group)
	if total == 0 {
		b.WriteString(mutedStyle.Render("(no agents yet — press space to spawn one)"))
		return padToHeight(b.String(), height)
	}
	for _, sess := range m.visibleSessions() {
		if !inGroupSubtree(sess.Group, group) {
			continue
		}
		if shown >= height-2 && total > shown+1 {
			b.WriteString(subtleStyle.Render(fmt.Sprintf("  … %d more", total-shown)))
			break
		}
		glyph := lipgloss.NewStyle().Foreground(statusColor(sess.Status)).Render(statusGlyph(sess.Status))
		line := glyph + " " + valueStyle.Render(sess.Name) +
			subtleStyle.Render("  "+sess.Tool+" · "+relTime(sess.CreatedAt))
		if ansi.StringWidth(line) > width {
			line = ansi.Truncate(line, width-1, "…")
		}
		b.WriteString(line + "\n")
		shown++
	}
	return padToHeight(strings.TrimRight(b.String(), "\n"), height)
}

// viewPreview renders the tail of the selected session's tmux pane with
// its original ANSI colors, clipped to the panel. Each line is sanitized
// and closed with an SGR reset so pane colors never leak into the layout.
func (m *Model) viewPreview(width, height int) string {
	var b strings.Builder
	b.WriteString(divider("Preview", width) + "\n")
	lines := paneTail(m.preview, height-1)
	if len(lines) == 0 {
		b.WriteString(mutedStyle.Render("(no output yet)"))
		return padToHeight(b.String(), height)
	}
	for _, line := range lines {
		b.WriteString(previewLine(line, width) + "\n")
	}
	return padToHeight(strings.TrimRight(b.String(), "\n"), height)
}

// eraseSeqs matches CSI K (erase in line) and CSI J (erase in display).
// Captured panes carry them, and passed through they make the outer
// terminal paint the current background past the clip point.
var eraseSeqs = regexp.MustCompile(`\x1b\[[0-9]*[KJ]`)

func previewLine(line string, width int) string {
	line = eraseSeqs.ReplaceAllString(line, "")
	line = strings.Map(func(r rune) rune {
		if r < 0x20 && r != 0x1b && r != '\t' {
			return -1
		}
		return r
	}, line)
	if ansi.StringWidth(line) > width {
		line = ansi.Truncate(line, width-1, "…")
	}
	if strings.ContainsRune(line, 0x1b) {
		line += "\x1b[0m"
	}
	return line
}

// paneTail returns the last n lines of pane text. Trailing blanks are
// dropped and interior runs of blank lines collapse to one, so sparse
// TUI panes (claude leaves most rows empty) show their real content
// instead of a window of whitespace. ANSI-only lines count as blank.
func paneTail(pane string, n int) []string {
	if n <= 0 || pane == "" {
		return nil
	}
	blank := func(line string) bool {
		return strings.TrimSpace(ansi.Strip(line)) == ""
	}
	var lines []string
	for _, line := range strings.Split(pane, "\n") {
		if blank(line) && len(lines) > 0 && blank(lines[len(lines)-1]) {
			continue
		}
		lines = append(lines, line)
	}
	for len(lines) > 0 && blank(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

func padToHeight(s string, height int) string {
	missing := height - lipgloss.Height(s)
	if missing > 0 {
		s += strings.Repeat("\n", missing)
	}
	return s
}

// viewFooter lists every shortcut, wrapping onto extra lines when the
// terminal is too narrow for one.
func (m *Model) viewFooter() string {
	pairs := [][2]string{
		{"↑↓", "navigate"}, {"↵", "attach"}, {"n", "new"}, {"g", "group"},
		{"⇧↑↓", "reorder"}, {"space", "quick prompt"}, {"D", "review"}, {"f", "fold"}, {"F", "fold all"}, {"m", "move"}, {"r", "rename/edit"},
		{"v", "revive"}, {"a", "archive"}, {"u", "restore"}, {"d", "delete"}, {"/", "search"},
		{"t", "archived"}, {"s", "settings"}, {"?", "help"}, {"q", "quit"},
	}
	if m.quick.active {
		pairs = [][2]string{
			{"↵", "send"}, {"↑↓", "switch target"}, {"⇥", "tool: " + m.quickTool()},
			{"esc", "close"},
		}
	}
	if m.mode == modeRename {
		pairs = [][2]string{{"↵", "save"}, {"esc", "cancel"}}
		if m.rename.isGroup {
			pairs = [][2]string{{"⇥", "name / path"}, {"↵", "save"}, {"esc", "cancel"}}
		}
	}
	return footerLine(pairs, m.width)
}

// footerLine wraps key hint pairs onto extra lines when the terminal is
// too narrow for one.
func footerLine(pairs [][2]string, width int) string {
	sep := subtleStyle.Render(" · ")
	sepWidth := ansi.StringWidth(sep)
	var lines []string
	line, lineWidth := "", 0
	for _, p := range pairs {
		part := keyStyle.Render(p[0]) + " " + mutedStyle.Render(p[1])
		partWidth := ansi.StringWidth(part)
		switch {
		case line == "":
			line, lineWidth = " "+part, 1+partWidth
		case lineWidth+sepWidth+partWidth <= width:
			line += sep + part
			lineWidth += sepWidth + partWidth
		default:
			lines = append(lines, line)
			line, lineWidth = " "+part, 1+partWidth
		}
	}
	return strings.Join(append(lines, line), "\n")
}

func displayGroup(path string) string {
	if path == "" {
		return "root"
	}
	return path
}

func kv(key, value string) string {
	return labelStyle.Width(6).Render(key) + valueStyle.Render(value) + "\n"
}

func relTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func humanBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// truncateTail keeps the end of the string (best for paths).
func truncateTail(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max || max <= 1 {
		return s
	}
	return "…" + string(runes[len(runes)-max+1:])
}
