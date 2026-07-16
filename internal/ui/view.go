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

	header := titleStyle.Render("Agent Manager")
	scope := "active"
	if m.showArchived {
		scope = "all (incl. archived)"
	}
	sessionCount := 0
	for _, r := range m.rows {
		if !r.isGroup {
			sessionCount++
		}
	}
	header += "  " + mutedStyle.Render(fmt.Sprintf("%d sessions · %s", sessionCount, scope))

	leftWidth := m.width * 35 / 100
	if leftWidth < 28 {
		leftWidth = 28
	}
	rightWidth := m.width - leftWidth - 4
	if rightWidth < 24 {
		rightWidth = 24
	}
	bodyHeight := m.height - 4
	if bodyHeight < 3 {
		bodyHeight = 3
	}

	left := panelStyle.Width(leftWidth).Height(bodyHeight).Render(m.viewList(leftWidth, bodyHeight))
	right := panelStyle.Width(rightWidth).Height(bodyHeight).Render(m.viewSidebar(rightWidth-2, bodyHeight))
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	return strings.Join([]string{header, body, m.viewFooter()}, "\n")
}

func (m *Model) viewList(width, height int) string {
	if len(m.rows) == 0 {
		empty := "No sessions. Press n to create one."
		if strings.TrimSpace(m.search) != "" {
			empty = "No matches for \"" + m.search + "\"."
		}
		return mutedStyle.Render(empty)
	}

	start, end := scrollWindow(len(m.rows), m.cursor, height)
	var b strings.Builder
	if start > 0 {
		b.WriteString(mutedStyle.Render(fmt.Sprintf("  ↑ %d more", start)) + "\n")
	}
	for i := start; i < end; i++ {
		b.WriteString(m.renderTreeRow(m.rows[i], i == m.cursor, width) + "\n")
	}
	if end < len(m.rows) {
		b.WriteString(mutedStyle.Render(fmt.Sprintf("  ↓ %d more", len(m.rows)-end)))
	}
	return strings.TrimRight(b.String(), "\n")
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
	cursor := "  "
	if selected {
		cursor = "❯ "
	}
	indent := strings.Repeat("  ", r.depth)

	maxWidth := width - 2

	if r.isGroup {
		marker := "▾"
		if m.collapsed[r.group] {
			marker = "▸"
		}
		line := cursor + indent + groupHeaderStyle.Render(marker+" "+baseName(r.group))
		line = ansi.Truncate(line, maxWidth, "…")
		if selected {
			return selectedRowStyle.Width(maxWidth).Render(line)
		}
		return line
	}

	sess := r.sess
	glyph := lipgloss.NewStyle().Foreground(statusColor(sess.Status)).Render(statusGlyph(sess.Status))
	archived := ""
	if sess.Archived {
		archived = " [archived]"
	}
	line := fmt.Sprintf("%s%s%s %s %s%s",
		cursor, indent, glyph, sess.Name,
		mutedStyle.Render(sess.Tool+" · "+sess.Status+" · "+relTime(sess.CreatedAt)), archived)
	line = ansi.Truncate(line, maxWidth, "…")
	if selected {
		return selectedRowStyle.Width(maxWidth).Render(line)
	}
	return rowStyle.Render(line)
}

// viewSidebar lays out session details and computer stats side by side
// on top, with the live preview filling the rest of the panel below.
func (m *Model) viewSidebar(width, height int) string {
	detailWidth := width * 55 / 100
	statsWidth := width - detailWidth
	detail := m.viewDetail(detailWidth)
	computer := groupHeaderStyle.Render("Computer") + "\n" + m.viewComputer()
	top := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(detailWidth).Render(detail),
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
		return mutedStyle.Render("No session selected") + "\n"
	}
	var b strings.Builder
	b.WriteString(valueStyle.Render(sess.Name) + "\n")
	b.WriteString(kv("tool", sess.Tool))
	b.WriteString(kv("group", displayGroup(sess.Group)))
	b.WriteString(kv("status", statusText(sess.Status)))
	b.WriteString(kv("dir", truncateTail(sess.Cwd, width-10)))
	b.WriteString(kv("age", relTime(sess.CreatedAt)))
	if m.procFor == sess.ID && m.proc.OK {
		b.WriteString(kv("proc", fmt.Sprintf("%.1f%% cpu · %s", m.proc.CPUPercent, humanBytes(m.proc.RSS))))
	}
	return b.String()
}

// viewPreview renders the tail of the selected session's tmux pane with
// its original ANSI colors, clipped to the panel. Each line is sanitized
// and closed with an SGR reset so pane colors never leak into the layout.
func (m *Model) viewPreview(width, height int) string {
	var b strings.Builder
	b.WriteString(groupHeaderStyle.Render("Preview") + "\n")
	lines := paneTail(m.preview, height-1)
	if len(lines) == 0 {
		b.WriteString(mutedStyle.Render("(no output)"))
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

func (m *Model) viewComputer() string {
	var b strings.Builder
	s := m.snap
	if s.CPUOK {
		b.WriteString(kv("cpu", fmt.Sprintf("%.1f%%", s.CPUPercent)))
	} else {
		b.WriteString(kv("cpu", "n/a"))
	}
	if s.MemOK {
		b.WriteString(kv("mem", fmt.Sprintf("%s / %s (%.0f%%)",
			humanBytes(s.MemUsed), humanBytes(s.MemTotal), s.MemPercent)))
	} else {
		b.WriteString(kv("mem", "n/a"))
	}
	if s.LoadOK {
		b.WriteString(kv("load", fmt.Sprintf("%.2f %.2f %.2f", s.Load1, s.Load5, s.Load15)))
	}
	if s.DiskOK {
		b.WriteString(kv("disk", fmt.Sprintf("%s / %s (%.0f%%)",
			humanBytes(s.DiskUsed), humanBytes(s.DiskTotal), s.DiskPercent)))
	}
	return b.String()
}

func (m *Model) viewFooter() string {
	if m.searching {
		return footerStyle.Render("search: ") + m.search + footerStyle.Render("  (enter/esc to close)")
	}
	if m.mode == modeConfirmDelete {
		return errStyle.Render(m.confirm.label)
	}
	if m.err != "" {
		return errStyle.Render("! " + m.err)
	}
	return footerStyle.Render("n new · enter attach · m move · r rename · a archive · u restore · d delete · space fold · t archived · / search · ? help · q quit")
}

func (m *Model) viewForm() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("New Session") + "\n\n")
	b.WriteString(formField("name", m.form.name.View(), m.form.focus == fieldName))

	toolVal := "(none configured)"
	if len(m.form.toolNames) > 0 {
		toolVal = "◂ " + m.form.toolNames[m.form.toolIndex] + " ▸"
	}
	b.WriteString(formField("tool", toolVal, m.form.focus == fieldTool))
	b.WriteString(formField("directory", m.form.dir.View(), m.form.focus == fieldDir))

	selectedGroup := displayGroup(m.form.groups[m.form.groupIndex].path)
	b.WriteString(formField("group", selectedGroup, m.form.focus == fieldGroup))

	if m.form.focus == fieldGroup {
		b.WriteString(m.viewGroupPicker())
	}

	b.WriteString("\n")
	if m.err != "" {
		b.WriteString(errStyle.Render("! "+m.err) + "\n\n")
	}
	hint := "tab/↑↓ move · ←→ change tool · enter create · esc cancel"
	if m.form.focus == fieldGroup {
		hint = "↑↓ pick group · n new subgroup here · tab next field · enter create session · esc cancel"
	}
	if m.form.creatingGroup {
		hint = "enter create group · esc cancel"
	}
	b.WriteString(footerStyle.Render(hint))
	return b.String()
}

func (m *Model) viewGroupPicker() string {
	var b strings.Builder
	for i, opt := range m.form.groups {
		cursor := "     "
		if i == m.form.groupIndex {
			cursor = "   ❯ "
		}
		label := displayGroup(opt.path)
		if opt.path != "" {
			label = strings.Repeat("  ", opt.depth) + baseName(opt.path)
		}
		line := cursor + label
		if i == m.form.groupIndex {
			line = cursor + valueStyle.Render(label)
		} else {
			line = cursor + mutedStyle.Render(label)
		}
		b.WriteString(line + "\n")
	}
	if m.form.creatingGroup {
		parent := displayGroup(m.form.groups[m.form.groupIndex].path)
		b.WriteString("   " + labelStyle.Render("new group under "+parent+":") + " " + m.form.newGroup.View() + "\n")
	}
	return b.String()
}

func displayGroup(path string) string {
	if path == "" {
		return "(root)"
	}
	return path
}

func (m *Model) viewRename() string {
	var b strings.Builder
	what := "session"
	context := ""
	if m.rename.isGroup {
		what = "group"
		if idx := strings.LastIndex(m.rename.path, "/"); idx >= 0 {
			context = "  " + mutedStyle.Render("under "+m.rename.path[:idx])
		}
	}
	b.WriteString(titleStyle.Render("Rename "+what) + context + "\n\n")
	b.WriteString("  " + m.rename.input.View() + "\n\n")
	if m.err != "" {
		b.WriteString(errStyle.Render("! "+m.err) + "\n\n")
	}
	b.WriteString(footerStyle.Render("enter apply · esc cancel"))
	return b.String()
}

func (m *Model) viewMove() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Move to group") + "\n\n")
	b.WriteString(m.viewGroupPicker())
	b.WriteString("\n")
	if m.err != "" {
		b.WriteString(errStyle.Render("! "+m.err) + "\n\n")
	}
	hint := "↑↓ pick group · n new subgroup here · enter move · esc cancel"
	if m.form.creatingGroup {
		hint = "enter create group · esc cancel"
	}
	b.WriteString(footerStyle.Render(hint))
	return b.String()
}

func (m *Model) viewHelp() string {
	rows := [][2]string{
		{"n", "new session"},
		{"enter", "attach session / fold group"},
		{"ctrl+q", "inside a session: back to manager"},
		{"m", "move session to another group"},
		{"r", "rename session / group"},
		{"a / u", "archive / restore (u works in archived view, t)"},
		{"d", "delete session, or group + entire subtree"},
		{"space", "collapse / expand group"},
		{"t", "toggle archived view"},
		{"/", "search"},
		{"ctrl+r", "force refresh"},
		{"↑↓ / jk", "move"},
		{"q", "quit (sessions keep running)"},
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render("Agent Manager · Help") + "\n\n")
	for _, r := range rows {
		b.WriteString(fmt.Sprintf("  %s  %s\n",
			valueStyle.Width(10).Render(r[0]), mutedStyle.Render(r[1])))
	}
	b.WriteString("\n" + footerStyle.Render("any key to close"))
	return b.String()
}

func formField(label, value string, focused bool) string {
	prefix := "  "
	style := labelStyle
	if focused {
		prefix = "❯ "
		style = valueStyle
	}
	return fmt.Sprintf("%s%s %s\n", prefix, style.Width(10).Render(label), value)
}

func kv(key, value string) string {
	return fmt.Sprintf("%s %s\n", labelStyle.Width(9).Render(key), valueStyle.Render(value))
}

func statusText(s string) string {
	return lipgloss.NewStyle().Foreground(statusColor(s)).Render(s)
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
