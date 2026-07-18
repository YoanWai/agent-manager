package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

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
	if m.form.focus == fieldDir && m.pathSugg.active() {
		b.WriteString(m.viewPathSuggestions() + "\n")
	}
	b.WriteString(formField("prompt", m.form.prompt.View(), m.form.focus == fieldPrompt))
	b.WriteString(formField("group", groupBadge(displayGroup(m.form.groups[m.form.groupIndex].path)), m.form.focus == fieldGroup))

	if m.form.focus == fieldGroup {
		b.WriteString("\n" + m.viewGroupPicker())
	}

	hint := "tab/↑↓ move · ←→ tool · ↵ create · esc cancel"
	if m.form.focus == fieldGroup {
		hint = "↑↓ pick group · tab next field · ↵ create · esc cancel"
	}
	if m.form.focus == fieldDir && m.pathSugg.active() {
		hint = pathSuggestHint(m.pathSugg.chosen)
	}
	return m.card("◆ New Session", strings.TrimRight(b.String(), "\n"), hint)
}

func groupBadge(path string) string {
	return lipgloss.NewStyle().Foreground(colorAccent2).Render(path)
}

func pathSuggestHint(chosen bool) string {
	if chosen {
		return "↑↓ pick · ↵/tab complete · esc close"
	}
	return "↑↓ pick · tab complete · ↵ create · esc close"
}

// viewPathSuggestions renders the directory-completion dropdown under
// a focused path field.
func (m *Model) viewPathSuggestions() string {
	var b strings.Builder
	for i, path := range m.pathSugg.suggestions {
		marker := "  "
		style := mutedStyle
		if i == m.pathSugg.index {
			marker = lipgloss.NewStyle().Foreground(colorAccent).Render("❯ ")
			style = lipgloss.NewStyle().Foreground(colorAccent2).Bold(true)
		}
		b.WriteString("      " + marker + style.Render(truncateTail(path, 40)) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
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
	return strings.TrimRight(b.String(), "\n")
}

func (m *Model) viewGroupForm() string {
	var b strings.Builder
	b.WriteString(formField("name", m.groupForm.name.View(), m.groupForm.focus == gfName))
	b.WriteString(formField("parent", groupBadge(displayGroup(m.selectedGroupPath())), m.groupForm.focus == gfParent))
	b.WriteString(formField("path", m.groupForm.path.View(), m.groupForm.focus == gfPath))
	if m.groupForm.focus == gfPath && m.pathSugg.active() {
		b.WriteString(m.viewPathSuggestions() + "\n")
	}
	if m.groupForm.focus == gfParent {
		b.WriteString("\n" + m.viewGroupPicker())
	}
	hint := "tab/↑↓ move · ↵ create · esc cancel"
	if m.groupForm.focus == gfParent {
		hint = "↑↓ pick parent · tab next field · ↵ create · esc cancel"
	}
	if m.groupForm.focus == gfPath && m.pathSugg.active() {
		hint = pathSuggestHint(m.pathSugg.chosen)
	}
	return m.card("✦ New Group", strings.TrimRight(b.String(), "\n"), hint)
}

func (m *Model) viewSettings() string {
	marker := lipgloss.NewStyle().Foreground(colorAccent).Render("❯ ")
	label := lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("quick spawn tool")
	tool := subtleStyle.Render("◂ ") +
		valueStyle.Render(m.settings.toolNames[m.settings.toolIndex]) +
		subtleStyle.Render(" ▸")
	return m.card("⚙ Settings", marker+label+"  "+tool, "←→ change · ↵/esc save")
}

func (m *Model) viewMove() string {
	return m.card("⇄ Move to group", m.viewGroupPicker(), "↑↓ pick · ↵ move · esc cancel")
}

func (m *Model) viewHelp() string {
	rows := [][2]string{
		{"n", "new session"},
		{"↵", "attach session / fold group"},
		{"ctrl+q", "inside a session: back to manager"},
		{"m", "move session to another group"},
		{"g", "new group (name, parent, default path)"},
		{"r", "rename session / edit group (name + default path)"},
		{"v", "revive dead session (resumes the agent)"},
		{"a / u", "archive / restore"},
		{"d", "delete session, or group + subtree"},
		{"shift+↑↓", "reorder row up / down"},
		{"space", "quick prompt: answer session / spawn agent in group"},
		{"⇥", "in quick prompt: switch spawn tool"},
		{"D", "review changes: whole-file diffs, comment lines, send to agent"},
		{"s", "cycle diff scope: uncommitted / vs base / last commit / staged"},
		{"F", "fold / unfold all groups"},
		{"s", "settings (quick spawn tool)"},
		{"t", "toggle archived view"},
		{"/", "search"},
		{"↑↓ / jk", "move cursor"},
		{"q", "quit (sessions keep running)"},
	}
	var b strings.Builder
	for _, binding := range rows {
		b.WriteString(keyStyle.Width(10).Render(binding[0]) + mutedStyle.Render(binding[1]) + "\n")
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
