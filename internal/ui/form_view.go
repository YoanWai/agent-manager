package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// formHit is a body line's field, and its list entry or -1.
type formHit struct {
	field int
	entry int
}

func (m *Model) viewForm() string {
	var b strings.Builder
	m.form.hits = m.form.hits[:0]
	add := func(text string, hit formHit) {
		b.WriteString(text)
		for range strings.Count(text, "\n") {
			m.form.hits = append(m.form.hits, hit)
		}
	}
	field := func(label, value string, id int) {
		add(formField(label, value, m.form.focus == id), formHit{field: id, entry: -1})
	}
	field("name", textInputView(m.form.name), fieldName)

	toolVal := "(none configured)"
	if len(m.form.toolNames) > 0 {
		toolVal = subtleStyle.Render("◂ ") + valueStyle.Render(m.form.toolNames[m.form.toolIndex]) + subtleStyle.Render(" ▸")
	}
	field("tool", toolVal, fieldTool)
	toolName, ch := m.formTool(), &m.form.choice
	if value, shown := m.profileRow(toolName, ch); shown {
		field("profile", value, fieldProfile)
	}
	if note, listed := m.modelRowNote(toolName); !listed {
		field("model", ansi.Wrap(note, m.formValueWidth(), ""), fieldModel)
	} else {
		field("model", textInputView(ch.filter), fieldModel)
		if m.form.focus == fieldModel && ch.sugg.open {
			lines, entries := m.viewModelSuggestions(toolName, ch, ch.query(), formLabelColumn, m.formValueWidth(), modelListRows)
			for i, line := range lines {
				add(line+"\n", formHit{field: fieldModel, entry: entries[i]})
			}
		}
	}
	if value, shown, _ := m.effortRow(toolName, ch); shown {
		field("effort", value, fieldEffort)
	}
	field("dir", textInputView(m.form.dir), fieldDir)
	if m.form.focus == fieldDir && m.pathSugg.active() {
		add(m.viewPathSuggestions()+"\n", formHit{field: fieldDir, entry: -1})
	}
	worktreeField := subtleStyle.Render(worktreeUnavailable)
	if capable, known := m.cachedWorktreeCapability(m.formSpawnDir()); known && capable {
		worktreeVal := "off"
		if m.form.worktree {
			worktreeVal = "on"
		}
		worktreeField = subtleStyle.Render("◂ ") + valueStyle.Render(worktreeVal) + subtleStyle.Render(" ▸")
	}
	field("worktree", worktreeField, fieldWorktree)
	if m.formWorktreeOn() {
		field("base", m.spawnBaseLabel(m.formSpawnDir(), m.selectedGroupPath()), fieldBase)
	}
	// Chips are tokens inside the typed text, so they wrap and reflow with
	// the words around them; painting happens on the rendered prompt.
	field("prompt", m.form.prompt.view(), fieldPrompt)
	field("group", groupBadge(displayGroup(m.form.groups[m.form.groupIndex].path)), fieldGroup)

	if m.form.focus == fieldGroup {
		add("\n", formHit{field: fieldGroup, entry: -1})
		for i, line := range strings.Split(m.viewGroupPicker(), "\n") {
			add(line+"\n", formHit{field: fieldGroup, entry: i})
		}
	}

	hint := [][2]string{{"tab/↑↓", "move"}, {"←→", "change"}, {"↵", "create"}, {"esc", "cancel"}}
	switch {
	case m.form.focus == fieldPrompt:
		hint = [][2]string{{"ctrl+v", "paste an image"}, {"tab", "move"}, {"↑↓", "caret or move"}, {"↵", "create"}, {"esc", "cancel"}}
	case m.form.focus == fieldGroup:
		hint = [][2]string{{"←→", "pick group"}, {"tab/↑↓", "move"}, {"↵", "create"}, {"esc", "cancel"}}
	case m.form.focus == fieldDir && m.pathSugg.active():
		hint = pathSuggestHint(m.pathSugg.chosen)
	case m.form.focus == fieldModel && ch.sugg.open:
		hint = [][2]string{{"type", "filter"}, {"↑↓", "pick"}, {"tab", "fill in"}, {"↵", "create"}, {"esc", "close"}}
	case m.form.focus == fieldModel:
		hint = [][2]string{{"type", "filter"}, {"tab/↑↓", "move"}, {"↵", "create"}, {"esc", "cancel"}}
	case m.form.focus == fieldEffort && m.effortTyped(toolName, ch):
		hint = [][2]string{{"type", "level"}, {"tab/↑↓", "move"}, {"↵", "create"}, {"esc", "cancel"}}
	case m.form.focus == fieldEffort:
		hint = [][2]string{{"←→", "level"}, {"tab/↑↓", "move"}, {"↵", "create"}, {"esc", "cancel"}}
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
