package ui

import (
	"fmt"
	"regexp"
	"strings"
	"time"

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
	}

	leftWidth := m.width * 34 / 100
	if leftWidth < 30 {
		leftWidth = 30
	}
	rightWidth := m.width - leftWidth
	bodyHeight := m.height - 3
	if bodyHeight < 3 {
		bodyHeight = 3
	}

	listInner := leftWidth - 4
	left := titledPanel("Sessions", m.viewList(listInner, bodyHeight-2), leftWidth, bodyHeight, false)
	right := titledPanel(m.sidebarTitle(), m.viewSidebar(rightWidth-4, bodyHeight-2), rightWidth, bodyHeight, false)
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	return m.viewHeader() + "\n" + body + "\n" + m.viewFooter()
}

func (m *Model) viewHeader() string {
	scope := "active"
	if m.showArchived {
		scope = "archived"
	}
	sessionCount := 0
	for _, r := range m.rows {
		if !r.isGroup {
			sessionCount++
		}
	}
	brand := badgeStyle.Render("◆ Agent Manager")
	meta := mutedStyle.Render(fmt.Sprintf("%d sessions", sessionCount)) +
		subtleStyle.Render(" · ") +
		lipgloss.NewStyle().Foreground(colorAccent2).Render(scope)

	left := brand + "  " + meta
	gap := m.width - ansi.StringWidth(left)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap)
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

func (m *Model) renderTreeRow(r row, selected bool, width int) string {
	bar := " "
	if selected {
		bar = lipgloss.NewStyle().Foreground(colorAccent).Render("▎")
	}
	guides := treeGuides(r.depth)

	var content string
	if r.isGroup {
		marker := "▾"
		if m.collapsed[r.group] {
			marker = "▸"
		}
		count := subtleStyle.Render(fmt.Sprintf(" (%d)", m.groupSessionCount(r.group)))
		name := lipgloss.NewStyle().Foreground(colorAccent2).Bold(true).Render(baseName(r.group))
		content = subtleStyle.Render(marker) + " " + name + count
	} else {
		sess := r.sess
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

// viewSidebar lays out session details and computer stats side by side
// on top, with the live preview filling the rest of the panel below.
func (m *Model) viewSidebar(width, height int) string {
	detailWidth := width * 55 / 100
	statsWidth := width - detailWidth - 3
	detail := m.viewDetail(detailWidth)
	computer := m.viewComputer(statsWidth)
	top := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(detailWidth).Render(detail),
		subtleStyle.Render(" │ "),
		lipgloss.NewStyle().Width(statsWidth).Render(computer),
	)

	previewHeight := height - lipgloss.Height(top) - 1
	if previewHeight < 3 {
		return top
	}
	return top + "\n" + m.viewPreview(width, previewHeight)
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

func (m *Model) viewComputer(width int) string {
	s := m.snap
	barWidth := width - 20
	if barWidth < 6 {
		barWidth = 6
	}
	if barWidth > 14 {
		barWidth = 14
	}
	var b strings.Builder
	meter := func(label string, percent float64, ok bool) string {
		if !ok {
			return labelStyle.Width(5).Render(label) + mutedStyle.Render("n/a") + "\n"
		}
		return labelStyle.Width(5).Render(label) + gauge(percent, barWidth) +
			mutedStyle.Render(fmt.Sprintf(" %.0f%%", percent)) + "\n"
	}
	b.WriteString(meter("cpu", s.CPUPercent, s.CPUOK))
	b.WriteString(meter("mem", s.MemPercent, s.MemOK))
	b.WriteString(meter("disk", s.DiskPercent, s.DiskOK))
	if s.MemOK {
		b.WriteString(subtleStyle.Render(fmt.Sprintf("      %s / %s", humanBytes(s.MemUsed), humanBytes(s.MemTotal))) + "\n")
	}
	if s.LoadOK {
		b.WriteString(labelStyle.Width(5).Render("load") +
			mutedStyle.Render(fmt.Sprintf("%.2f %.2f %.2f", s.Load1, s.Load5, s.Load15)) + "\n")
	}
	return b.String()
}

func (m *Model) viewFooter() string {
	if m.searching {
		cursor := lipgloss.NewStyle().Foreground(colorAccent).Render("▏")
		return keyStyle.Render(" search ") + valueStyle.Render(m.search) + cursor +
			subtleStyle.Render("  enter/esc to close")
	}
	if m.mode == modeConfirmDelete {
		return errStyle.Render(" ⚠ "+m.confirm.label) + subtleStyle.Render("  y/n")
	}
	if m.err != "" {
		return errStyle.Render(" ✖ " + m.err)
	}
	pairs := [][2]string{
		{"n", "new"}, {"↵", "attach"}, {"m", "move"}, {"r", "rename"},
		{"a", "archive"}, {"u", "restore"}, {"d", "delete"}, {"/", "search"},
		{"t", "archived"}, {"?", "help"}, {"q", "quit"},
	}
	parts := make([]string, len(pairs))
	for i, p := range pairs {
		parts[i] = keyStyle.Render(p[0]) + " " + mutedStyle.Render(p[1])
	}
	line := " " + strings.Join(parts, subtleStyle.Render("  ·  "))
	return ansi.Truncate(line, m.width, subtleStyle.Render(" …"))
}

// card centers a bordered modal with a title and footer hint.
func (m *Model) card(title, body, hint string) string {
	width := 60
	if width > m.width-4 {
		width = m.width - 4
	}
	header := badgeStyle.Render(title)
	content := header + "\n\n" + body
	if m.err != "" {
		content += "\n" + errStyle.Render("✖ "+m.err)
	}
	content += "\n\n" + subtleStyle.Render(hint)

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorAccent).
		Padding(1, 3).
		Width(width).
		Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m *Model) viewForm() string {
	var b strings.Builder
	b.WriteString(formField("name", m.form.name.View(), m.form.focus == fieldName))

	toolVal := "(none configured)"
	if len(m.form.toolNames) > 0 {
		toolVal = subtleStyle.Render("◂ ") + valueStyle.Render(m.form.toolNames[m.form.toolIndex]) + subtleStyle.Render(" ▸")
	}
	b.WriteString(formField("tool", toolVal, m.form.focus == fieldTool))
	b.WriteString(formField("dir", m.form.dir.View(), m.form.focus == fieldDir))
	b.WriteString(formField("group", groupBadge(displayGroup(m.form.groups[m.form.groupIndex].path)), m.form.focus == fieldGroup))

	if m.form.focus == fieldGroup {
		b.WriteString("\n" + m.viewGroupPicker())
	}

	hint := "tab/↑↓ move · ←→ tool · ↵ create · esc cancel"
	if m.form.focus == fieldGroup {
		hint = "↑↓ pick · n new subgroup · tab next · ↵ create · esc cancel"
	}
	if m.form.creatingGroup {
		hint = "↵ create group · esc cancel"
	}
	return m.card("◆ New Session", strings.TrimRight(b.String(), "\n"), hint)
}

func groupBadge(path string) string {
	return lipgloss.NewStyle().Foreground(colorAccent2).Render(path)
}

func (m *Model) viewGroupPicker() string {
	var b strings.Builder
	for i, opt := range m.form.groups {
		selected := i == m.form.groupIndex
		marker := "  "
		if selected {
			marker = lipgloss.NewStyle().Foreground(colorAccent).Render("❯ ")
		}
		label := displayGroup(opt.path)
		if opt.path != "" {
			label = strings.Repeat("  ", opt.depth) + baseName(opt.path)
		}
		style := mutedStyle
		if selected {
			style = lipgloss.NewStyle().Foreground(colorAccent2).Bold(true)
		}
		b.WriteString("  " + marker + style.Render(label) + "\n")
	}
	if m.form.creatingGroup {
		parent := displayGroup(m.form.groups[m.form.groupIndex].path)
		b.WriteString("\n  " + labelStyle.Render("new under "+parent+":") + " " + m.form.newGroup.View())
	}
	return strings.TrimRight(b.String(), "\n")
}

func displayGroup(path string) string {
	if path == "" {
		return "root"
	}
	return path
}

func (m *Model) viewRename() string {
	what := "Session"
	sub := ""
	if m.rename.isGroup {
		what = "Group"
		if idx := strings.LastIndex(m.rename.path, "/"); idx >= 0 {
			sub = mutedStyle.Render("under "+m.rename.path[:idx]) + "\n\n"
		}
	}
	body := sub + m.rename.input.View()
	return m.card("✎ Rename "+what, body, "↵ apply · esc cancel")
}

func (m *Model) viewMove() string {
	hint := "↑↓ pick · n new subgroup · ↵ move · esc cancel"
	if m.form.creatingGroup {
		hint = "↵ create group · esc cancel"
	}
	return m.card("⇄ Move to group", m.viewGroupPicker(), hint)
}

func (m *Model) viewHelp() string {
	rows := [][2]string{
		{"n", "new session"},
		{"↵", "attach session / fold group"},
		{"ctrl+q", "inside a session: back to manager"},
		{"m", "move session to another group"},
		{"r", "rename session / group"},
		{"a / u", "archive / restore"},
		{"d", "delete session, or group + subtree"},
		{"space", "collapse / expand group"},
		{"t", "toggle archived view"},
		{"/", "search"},
		{"ctrl+r", "force refresh"},
		{"↑↓ / jk", "move cursor"},
		{"q", "quit (sessions keep running)"},
	}
	var b strings.Builder
	for _, r := range rows {
		b.WriteString(keyStyle.Width(8).Render(r[0]) + mutedStyle.Render(r[1]) + "\n")
	}
	return m.card("? Keys", strings.TrimRight(b.String(), "\n"), "any key to close")
}

func formField(label, value string, focused bool) string {
	marker := "  "
	style := labelStyle
	if focused {
		marker = lipgloss.NewStyle().Foreground(colorAccent).Render("❯ ")
		style = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	}
	return fmt.Sprintf("%s%s %s\n", marker, style.Width(7).Render(label), value)
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
