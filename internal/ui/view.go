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
	case modeRename:
		return m.viewRename()
	case modeMove:
		return m.viewMove()
	case modeGroupForm:
		return m.viewGroupForm()
	}

	leftWidth := m.width * 34 / 100
	if leftWidth < 30 {
		leftWidth = 30
	}
	rightWidth := m.width - leftWidth
	footer := m.viewFooter()
	bodyHeight := m.height - 3 - lipgloss.Height(footer)
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
	left := titledPanel("Sessions", leftBody, leftWidth, bodyHeight, false)
	right := titledPanel(m.sidebarTitle(), m.viewSidebar(rightWidth-4, bodyHeight-2), rightWidth, bodyHeight, false)
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	return strings.Join([]string{m.viewHeader(), body, m.viewStatus(), footer}, "\n")
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
	return "Session"
}

func (m *Model) viewList(width, height int) string {
	if len(m.rows) == 0 {
		hint := "Press " + keyStyle.Render("n") + mutedStyle.Render(" to create a session.")
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

func (m *Model) groupSessionCount(path string) int {
	count := 0
	for _, sess := range m.sessions {
		if sess.Group == path || strings.HasPrefix(sess.Group, path+"/") {
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

	var content string
	if entry.isGroup {
		marker := "▾"
		if m.collapsed[entry.group] {
			marker = "▸"
		}
		count := subtleStyle.Render(fmt.Sprintf(" (%d)", m.groupSessionCount(entry.group)))
		name := lipgloss.NewStyle().Foreground(colorAccent2).Bold(true).Render(baseName(entry.group))
		content = subtleStyle.Render(marker) + " " + name + count
	} else {
		sess := entry.sess
		glyph := lipgloss.NewStyle().Foreground(statusColor(sess.Status)).Render(statusGlyph(sess.Status))
		nameStyle := valueStyle
		if selected {
			nameStyle = lipgloss.NewStyle().Foreground(colorBright).Bold(true)
		}
		name := nameStyle.Render(sess.Name)
		meta := subtleStyle.Render(sess.Tool + " · " + relTime(sess.CreatedAt))
		archived := ""
		if sess.Archived {
			archived = subtleStyle.Render(" ⋅ archived")
		}
		content = glyph + " " + name + "  " + meta + archived
	}

	line := bar + " " + guides + content
	line = padRight(line, width)
	if selected {
		return selectedRowStyle.Render(line)
	}
	return line
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
// filling the rest of the panel below.
func (m *Model) viewSidebar(width, height int) string {
	detail := divider("Details", width) + "\n" + m.viewDetail(width)
	previewHeight := height - lipgloss.Height(detail) - 1
	if previewHeight < 3 {
		return detail
	}
	return detail + "\n" + m.viewPreview(width, previewHeight)
}

func (m *Model) viewDetail(width int) string {
	sess, ok := m.selected()
	if !ok {
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
		{"⇧↑↓", "reorder"}, {"space", "fold"}, {"m", "move"}, {"r", "rename"},
		{"a", "archive"}, {"u", "restore"}, {"d", "delete"}, {"/", "search"},
		{"t", "archived"}, {"ctrl+r", "refresh"}, {"?", "help"}, {"q", "quit"},
	}
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
		case lineWidth+sepWidth+partWidth <= m.width:
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

// clipLine keeps the start of the string (best for terminal output).
func clipLine(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max || max <= 1 {
		return s
	}
	return string(runes[:max-1]) + "…"
}
