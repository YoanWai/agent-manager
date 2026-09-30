package ui

import (
	"github.com/charmbracelet/lipgloss"
	"strings"
)

func (m *Model) viewGroupPicker() string {
	var b strings.Builder
	for i, opt := range m.form.groups {
		selected := i == m.form.groupIndex
		marker := "  "
		if selected {
			marker = lipgloss.NewStyle().Foreground(colorAccent).Render("❯ ")
		}
		label := displayGroup(opt.path)
		if opt.sessID != "" {
			label = strings.Repeat("  ", opt.depth) + opt.name
		} else if opt.path != "" {
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
	b.WriteString(formField("name", textInputView(m.groupForm.name), m.groupForm.focus == gfName))
	b.WriteString(formField("parent", groupBadge(displayGroup(m.selectedGroupPath())), m.groupForm.focus == gfParent))
	b.WriteString(formField("path", textInputView(m.groupForm.path), m.groupForm.focus == gfPath))
	if m.groupForm.focus == gfPath && m.pathSugg.active() {
		b.WriteString(m.viewPathSuggestions() + "\n")
	}
	worktreeVal := subtleStyle.Render("◂ ") + valueStyle.Render(groupWorktreeOptions[m.groupForm.worktreeIndex]) + subtleStyle.Render(" ▸")
	b.WriteString(formField("worktree", worktreeVal, m.groupForm.focus == gfWorktree))
	if m.groupForm.focus == gfParent {
		b.WriteString("\n" + m.viewGroupPicker())
	}
	hint := [][2]string{{"tab/↑↓", "move"}, {"↵", "create"}, {"esc", "cancel"}}
	if m.groupForm.focus == gfParent {
		hint = [][2]string{{"←→", "pick parent"}, {"tab/↑↓", "move"}, {"↵", "create"}, {"esc", "cancel"}}
	}
	if m.groupForm.focus == gfWorktree {
		hint = [][2]string{{"tab/↑↓", "move"}, {"←→", "change"}, {"↵", "create"}, {"esc", "cancel"}}
	}
	if m.groupForm.focus == gfPath && m.pathSugg.active() {
		hint = pathSuggestHint(m.pathSugg.chosen)
	}
	return m.card("✦ New Group", strings.TrimRight(b.String(), "\n"), hint)
}
