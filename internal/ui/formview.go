package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m *Model) viewForm() string {
	m.form.prompt.input.SetHeight(textareaRows(m.form.prompt.input, m.formValueWidth()-2, formPromptMaxRows))

	var b strings.Builder
	b.WriteString(formField("name", textInputView(m.form.name), m.form.focus == fieldName))

	toolVal := "(none configured)"
	if len(m.form.toolNames) > 0 {
		toolVal = subtleStyle.Render("◂ ") + valueStyle.Render(m.form.toolNames[m.form.toolIndex]) + subtleStyle.Render(" ▸")
	}
	b.WriteString(formField("tool", toolVal, m.form.focus == fieldTool))
	b.WriteString(formField("dir", textInputView(m.form.dir), m.form.focus == fieldDir))
	if m.form.focus == fieldDir && m.pathSugg.active() {
		b.WriteString(m.viewPathSuggestions() + "\n")
	}
	worktreeField := subtleStyle.Render(worktreeUnavailable)
	if m.worktreeCapable(m.formSpawnDir()) {
		worktreeVal := "off"
		if m.form.worktree {
			worktreeVal = "on"
		}
		worktreeField = subtleStyle.Render("◂ ") + valueStyle.Render(worktreeVal) + subtleStyle.Render(" ▸")
	}
	b.WriteString(formField("worktree", worktreeField, m.form.focus == fieldWorktree))
	// Chips are tokens inside the typed text, so they wrap and reflow with
	// the words around them; painting happens on the rendered prompt.
	b.WriteString(formField("prompt", m.form.prompt.renderChips(textAreaView(m.form.prompt.input)), m.form.focus == fieldPrompt))
	b.WriteString(formField("group", groupBadge(displayGroup(m.form.groups[m.form.groupIndex].path)), m.form.focus == fieldGroup))

	if m.form.focus == fieldGroup {
		b.WriteString("\n" + m.viewGroupPicker())
	}

	hint := [][2]string{{"tab/↑↓", "move"}, {"←→", "change"}, {"↵", "create"}, {"esc", "cancel"}}
	if m.form.focus == fieldPrompt {
		hint = [][2]string{{"ctrl+v", "paste an image"}, {"tab", "move"}, {"↑↓", "caret or move"}, {"↵", "create"}, {"esc", "cancel"}}
	}
	if m.form.focus == fieldGroup {
		hint = [][2]string{{"←→", "pick group"}, {"tab/↑↓", "move"}, {"↵", "create"}, {"esc", "cancel"}}
	}
	if m.form.focus == fieldDir && m.pathSugg.active() {
		hint = pathSuggestHint(m.pathSugg.chosen)
	}
	return m.card("◆ New Session", strings.TrimRight(b.String(), "\n"), hint)
}

func groupBadge(path string) string {
	return lipgloss.NewStyle().Foreground(colorAccent2).Render(path)
}

func pathSuggestHint(chosen bool) [][2]string {
	if chosen {
		return [][2]string{{"↑↓", "pick"}, {"↵/tab", "complete"}, {"esc", "close"}}
	}
	return [][2]string{{"↑↓", "pick"}, {"tab", "complete"}, {"↵", "create"}, {"esc", "close"}}
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

func (m *Model) viewMove() string {
	return m.card("⇄ Move", m.viewGroupPicker(),
		[][2]string{{"↑↓", "pick"}, {"↵", "move"}, {"esc", "cancel"}})
}

func formField(label, value string, focused bool) string {
	marker := "  "
	style := labelStyle
	if focused {
		marker = lipgloss.NewStyle().Foreground(colorAccent).Render("❯ ")
		style = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	}
	lines := strings.Split(value, "\n")
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%s%s %s\n", marker, style.Width(9).Render(label), lines[0]))
	for _, line := range lines[1:] {
		b.WriteString(strings.Repeat(" ", formLabelColumn) + line + "\n")
	}
	return b.String()
}
