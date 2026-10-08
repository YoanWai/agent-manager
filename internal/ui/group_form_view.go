package ui

import (
	"cmp"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m *Model) viewGroupPicker() string { return m.form.viewGroupPicker() }

func (d *formDialog) viewGroupPicker() string {
	var b strings.Builder
	for i, opt := range d.groups {
		selected := i == d.groupIndex
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
	body, hint := m.groupForm.view(m)
	return m.card("✦ New Group", body, hint)
}

func (d *groupFormDialog) view(h groupFormHost) (string, [][2]string) {
	paths := h.pathSuggestions()
	var b strings.Builder
	b.WriteString(formField("name", textInputView(d.name), d.focus == gfName))
	b.WriteString(formField("parent", groupBadge(displayGroup(h.selectedGroupPath())), d.focus == gfParent))
	b.WriteString(formField("path", textInputView(d.path), d.focus == gfPath))
	if d.focus == gfPath && paths.active() {
		b.WriteString(paths.view() + "\n")
	}
	worktreeVal := subtleStyle.Render("◂ ") + valueStyle.Render(groupWorktreeOptions[d.worktreeIndex]) + subtleStyle.Render(" ▸")
	b.WriteString(formField("worktree", worktreeVal, d.focus == gfWorktree))
	b.WriteString(formField("base", groupBaseChoice(d.base, h.groupBase(h.selectedGroupPath())), d.focus == gfBase))
	if d.focus == gfParent {
		b.WriteString("\n" + h.viewGroupPicker())
	}
	hint := [][2]string{{"tab/↑↓", "move"}, {"↵", "create"}, {"esc", "cancel"}}
	if d.focus == gfParent {
		hint = [][2]string{{"←→", "pick parent"}, {"tab/↑↓", "move"}, {"↵", "create"}, {"esc", "cancel"}}
	}
	if d.focus == gfWorktree || d.focus == gfBase {
		hint = [][2]string{{"tab/↑↓", "move"}, {"←→", "change"}, {"↵", "create"}, {"esc", "cancel"}}
	}
	if d.focus == gfPath && paths.active() {
		hint = pathSuggestHint(paths.chosen)
	}
	return strings.TrimRight(b.String(), "\n"), hint
}

// groupBaseChoice renders a group's base picker: its own ref, or auto and
// the parent's choice that auto inherits.
func groupBaseChoice(base, inherited string) string {
	choice := subtleStyle.Render("◂ ") + valueStyle.Render(cmp.Or(base, "auto")) + subtleStyle.Render(" ▸")
	if base == "" && inherited != "" {
		choice += subtleStyle.Render("  " + inherited + " from parent")
	}
	return choice
}
