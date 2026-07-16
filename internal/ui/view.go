package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
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

	leftWidth := m.width * 55 / 100
	if leftWidth < 30 {
		leftWidth = 30
	}
	rightWidth := m.width - leftWidth - 4
	if rightWidth < 24 {
		rightWidth = 24
	}
	bodyHeight := m.height - 4
	if bodyHeight < 3 {
		bodyHeight = 3
	}

	left := panelStyle.Width(leftWidth).Height(bodyHeight).Render(m.viewList(leftWidth))
	right := panelStyle.Width(rightWidth).Height(bodyHeight).Render(m.viewSidebar())
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	return strings.Join([]string{header, body, m.viewFooter()}, "\n")
}

func (m *Model) viewList(width int) string {
	if len(m.rows) == 0 {
		empty := "No sessions. Press n to create one."
		if strings.TrimSpace(m.search) != "" {
			empty = "No matches for \"" + m.search + "\"."
		}
		return mutedStyle.Render(empty)
	}

	var b strings.Builder
	for i, r := range m.rows {
		b.WriteString(m.renderTreeRow(r, i == m.cursor, width) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m *Model) renderTreeRow(r row, selected bool, width int) string {
	cursor := "  "
	if selected {
		cursor = "❯ "
	}
	indent := strings.Repeat("  ", r.depth)

	if r.isGroup {
		marker := "▾"
		if m.collapsed[r.group] {
			marker = "▸"
		}
		line := cursor + indent + groupHeaderStyle.Render(marker+" "+baseName(r.group))
		if selected {
			return selectedRowStyle.Width(width - 2).Render(line)
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
	if selected {
		return selectedRowStyle.Width(width - 2).Render(line)
	}
	return rowStyle.Render(line)
}

func (m *Model) viewSidebar() string {
	var b strings.Builder
	sess, ok := m.selected()
	if ok {
		b.WriteString(valueStyle.Render(sess.Name) + "\n")
		b.WriteString(kv("tool", sess.Tool))
		b.WriteString(kv("group", displayGroup(sess.Group)))
		b.WriteString(kv("status", statusText(sess.Status)))
		b.WriteString(kv("dir", truncate(sess.Cwd, 40)))
		b.WriteString(kv("age", relTime(sess.CreatedAt)))
		if m.procFor == sess.ID && m.proc.OK {
			b.WriteString(kv("proc cpu", fmt.Sprintf("%.1f%%", m.proc.CPUPercent)))
			b.WriteString(kv("proc mem", humanBytes(m.proc.RSS)))
		}
	} else {
		b.WriteString(mutedStyle.Render("No session selected") + "\n")
	}

	b.WriteString("\n")
	b.WriteString(groupHeaderStyle.Render("Computer") + "\n")
	b.WriteString(m.viewComputer())
	return b.String()
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
		if sess, ok := m.selected(); ok {
			return errStyle.Render("delete " + sess.Name + "? kills tmux session. (y/n)")
		}
	}
	if m.err != "" {
		return errStyle.Render("! " + m.err)
	}
	return footerStyle.Render("n new · enter attach · a archive · u restore · d delete · space fold · t archived · / search · ? help · q quit")
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

func (m *Model) viewHelp() string {
	rows := [][2]string{
		{"n", "new session"},
		{"enter", "attach session / fold group"},
		{"ctrl+q", "inside a session: back to manager"},
		{"a / u", "archive / restore"},
		{"d", "delete (kills tmux session)"},
		{"space", "collapse / expand group"},
		{"t", "toggle archived view"},
		{"/", "search"},
		{"r", "force refresh"},
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

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 1 {
		return s[:max]
	}
	return "…" + s[len(s)-max+1:]
}
