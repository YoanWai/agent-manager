package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m *Model) cardWidth() int {
	width := 60
	if m.width >= 28 && width > m.width-4 {
		width = m.width - 4
	}
	return width
}

const cardPaddingX = 3

// card floats a modal on the app backdrop: a lifted panel, no border, its
// title set in accent above the body and its keys quietly at the foot.
func (m *Model) card(title, body, hint string) string {
	return m.cardSized(m.cardWidth(), title, body, hint)
}

// cardSized is card at an explicit width, for the key map, whose lines are
// too long to read inside the default column.
func (m *Model) cardSized(width int, title, body, hint string) string {
	head := lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(title)
	content := head + "\n\n" + body
	if m.errBar.text != "" {
		content += "\n" + errStyle.Render("✕ "+m.errBar.text)
	}
	content += "\n\n" + subtleStyle.Render(hint)

	inner := width - 2*cardPaddingX
	pad := strings.Repeat(" ", cardPaddingX)
	var lines []string
	lines = append(lines, paint("", width, blockHex()))
	for _, line := range strings.Split(content, "\n") {
		lines = append(lines, paint(pad+padRight(line, inner), width, blockHex()))
	}
	lines = append(lines, paint("", width, blockHex()))

	height := max(m.height, len(lines))
	left := max((m.width-width)/2, 0)
	frameWidth := max(m.width, left+width)
	top := max((height-len(lines))/2, 0)
	frame := make([]string, 0, height)
	for i := 0; i < height; i++ {
		row := ""
		if i >= top && i-top < len(lines) {
			row = paint("", left, backdropHex()) + lines[i-top]
		}
		frame = append(frame, paint(row, frameWidth, backdropHex()))
	}
	return strings.Join(frame, "\n")
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
	worktreeVal := "off"
	if m.form.worktree {
		worktreeVal = "on"
	}
	b.WriteString(formField("worktree", subtleStyle.Render("◂ ")+valueStyle.Render(worktreeVal)+subtleStyle.Render(" ▸"), m.form.focus == fieldWorktree))
	b.WriteString(formField("prompt", m.form.prompt.View(), m.form.focus == fieldPrompt))
	b.WriteString(formField("group", groupBadge(displayGroup(m.form.groups[m.form.groupIndex].path)), m.form.focus == fieldGroup))

	if m.form.focus == fieldGroup {
		b.WriteString("\n" + m.viewGroupPicker())
	}

	hint := "tab/↑↓ move · ←→ change · ↵ create · esc cancel"
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
	layout := "unified"
	if m.settings.layoutSplit {
		layout = "split"
	}
	density := "compact"
	if m.settings.comfortableRows {
		density = "comfortable"
	}
	quickClose := "stay open"
	if m.settings.quickCloseSend {
		quickClose = "close"
	}
	focusKey := "↵ focus · A attach"
	if !m.settings.enterFocuses {
		focusKey = "↵ attach · A focus"
	}
	worktreeDefault := "off"
	if m.settings.worktreeDefault {
		worktreeDefault = "on"
	}
	row := func(field int, name, value string) string {
		marker := "  "
		labelStyle := valueStyle
		if m.settings.field == field {
			marker = lipgloss.NewStyle().Foreground(colorAccent).Render("❯ ")
			labelStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
		}
		picker := subtleStyle.Render("◂ ") + valueStyle.Render(value) + subtleStyle.Render(" ▸")
		return marker + padRight(labelStyle.Render(name), 18) + picker
	}
	body := row(settingsFieldTool, "quick spawn tool", m.settings.toolNames[m.settings.toolIndex]) + "\n" +
		row(settingsFieldTheme, "theme", themes[m.settings.themeIndex].Name) + "  " +
		themeSwatch(themes[m.settings.themeIndex]) + "\n" +
		row(settingsFieldDensity, "list density", density) + "\n" +
		row(settingsFieldLayout, "review layout", layout) + "\n" +
		row(settingsFieldQuickClose, "after quick send", quickClose) + "\n" +
		row(settingsFieldFocusKey, "session keys", focusKey) + "\n" +
		row(settingsFieldWorktree, "worktree sessions", worktreeDefault) + "\n\n" +
		subtleStyle.Render("  version ") + valueStyle.Render(m.update.version) + m.versionStatus()
	return m.card("⚙ Settings", body, "↑↓ field · ←→ change · ↵/esc save")
}

// themeSwatch previews a palette as a run of blocks, so a theme can be
// picked by eye rather than by name.
func themeSwatch(t Theme) string {
	var b strings.Builder
	for _, hex := range []string{t.Accent, t.Accent2, t.Working, t.Waiting, t.Finished, t.Errored} {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(hex)).Render("█"))
	}
	return b.String()
}

// versionStatus reports whether a newer release is known, as a suffix for
// the settings version line. Empty when the daily check has not found one.
func (m *Model) versionStatus() string {
	if m.update.latest == "" {
		return ""
	}
	return subtleStyle.Render(" · ") +
		lipgloss.NewStyle().Foreground(colorAccent).Bold(true).
			Render("↑ "+m.update.latest+" available")
}

func (m *Model) viewMove() string {
	return m.card("⇄ Move to group", m.viewGroupPicker(), "↑↓ pick · ↵ move · esc cancel")
}

func (m *Model) viewHelp() string {
	rows := [][2]string{
		{"n", "new session"},
		{"↵", "attach session / fold group"},
		{"ctrl+q", "inside a session: back to manager"},
		{"ctrl+r", "inside a session: review its diff, esc returns"},
		{"m", "move session to another group"},
		{"g", "new group (name, parent, default path)"},
		{"r", "rename session / edit group (name + default path)"},
		{"x", "kill session, or every live session in a group (frees their RAM)"},
		{"X", "kill every live session"},
		{"v", "revive killed session, or every dead session in a group (resumes the agent)"},
		{"V", "revive every dead session"},
		{"a / u", "archive / restore"},
		{"d", "delete session, or group + subtree"},
		{"K / J", "reorder row up / down (shift+↑↓ also works)"},
		{"space", "quick prompt: answer session / spawn agent in group"},
		{"⇥", "in quick prompt: switch spawn tool"},
		{"⌥w", "in quick prompt: toggle worktree for the spawned agent"},
		{"^v", "in quick prompt: paste image as a chip at the cursor"},
		{"⌫", "in quick prompt: next to a chip, delete the whole chip"},
		{"ctrl+r", "review changes: whole-file diffs, comment lines, send to agent"},
		{"s", "in review: cycle scope (uncommitted / vs target / last commit / staged)"},
		{"r", "in review: pick the repo when the session dir holds several"},
		{"b", "in review: pick the branch from the repo's worktrees"},
		{"B", "in review: pick the target (merge-into branch) the branch diff compares against"},
		{"F", "fold / unfold all groups"},
		{"s", "settings (quick spawn tool, review layout, after quick send)"},
		{"|", "resize split (←→ / drag, enter commits, esc cancels)"},
		{"t", "toggle archived view"},
		{"e", "hide / show empty groups"},
		{"/", "search"},
		{"↑↓ / jk", "move cursor"},
		{"q", "quit (sessions keep running)"},
	}
	var b strings.Builder
	for _, binding := range rows {
		b.WriteString(keyStyle.Width(10).Render(binding[0]) + mutedStyle.Render(binding[1]) + "\n")
	}
	width := 92
	if m.width >= 28 && width > m.width-4 {
		width = m.width - 4
	}
	return m.cardSized(width, "? Keys", strings.TrimRight(b.String(), "\n"), "any key to close")
}

func formField(label, value string, focused bool) string {
	marker := "  "
	style := labelStyle
	if focused {
		marker = lipgloss.NewStyle().Foreground(colorAccent).Render("❯ ")
		style = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	}
	return fmt.Sprintf("%s%s %s\n", marker, style.Width(9).Render(label), value)
}
