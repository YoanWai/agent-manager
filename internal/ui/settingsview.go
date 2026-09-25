package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m *Model) viewSettings() string {
	if m.settings.cliPicker {
		return m.viewCLIPicker()
	}
	if m.settings.keyPicker {
		return m.viewKeyPicker()
	}
	layout := "unified"
	if m.settings.layoutSplit {
		layout = "split"
	}
	density := "compact"
	if m.settings.comfortableRows {
		density = "comfortable"
	}
	sessionLayout := "split"
	if m.settings.fullLayout {
		sessionLayout = "full screen"
	}
	header := "show"
	if m.settings.hideHeader {
		header = "hide"
	}
	stats := "show"
	if m.settings.hideStats {
		stats = "hide"
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
	arrowStep := "off"
	if m.settings.arrowStep {
		arrowStep = "on"
	}
	mouseMode := "on"
	if m.settings.mouseDisabled {
		mouseMode = "off"
	}
	// The beta tag borrows the messages card's yellow, so the row reads as
	// the one still under test.
	betaTag := lipgloss.NewStyle().Foreground(lipgloss.Color("#e2c044")).Render(" beta")
	themeAuto := "off"
	if m.settings.themeAuto {
		themeAuto = "on"
	}
	notifications := "off"
	if m.settings.notifications {
		notifications = "on"
	}
	notifyFinished := "off"
	if m.settings.notifyFinished {
		notifyFinished = "on"
	}
	toolValue := ""
	if len(m.settings.toolNames) > 0 {
		toolValue = m.settings.toolNames[m.settings.toolIndex]
	}
	lead := func(field int, name string) string {
		marker := "  "
		labelStyle := valueStyle
		if m.settings.field == field {
			marker = lipgloss.NewStyle().Foreground(colorAccent).Render("❯ ")
			labelStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
		}
		return marker + padRight(labelStyle.Render(name), 18)
	}
	row := func(field int, name, value string) string {
		return lead(field, name) + subtleStyle.Render("◂ ") + valueStyle.Render(value) + subtleStyle.Render(" ▸")
	}
	// An action row: enter runs it, so it carries no picker arrows.
	actionRow := func(field int, name, action string) string {
		return lead(field, name) + keyStyle.Render("↵") + mutedStyle.Render(" "+action)
	}
	// Report and suggest stay accent-colored even when unfocused so the
	// actions read as a call-to-action among the picker rows above them.
	ctaLead := func(field int, name string) string {
		marker := "  "
		labelStyle := lipgloss.NewStyle().Foreground(colorAccent2).Bold(true)
		if m.settings.field == field {
			marker = lipgloss.NewStyle().Foreground(colorAccent).Render("❯ ")
			labelStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
		}
		return marker + padRight(labelStyle.Render(name), 18)
	}
	ctaRow := func(field int, name, action string) string {
		return ctaLead(field, name) + keyStyle.Render("↵") + " " +
			lipgloss.NewStyle().Foreground(colorAccent2).Render(action)
	}
	body := row(settingsFieldTool, "default tool", toolValue) + "\n" +
		row(settingsFieldTheme, "theme", themes[m.settings.themeIndex].Name) + "  " +
		themeSwatch(themes[m.settings.themeIndex]) + "\n" +
		row(settingsFieldThemeAuto, "theme follows OS", themeAuto) + "\n" +
		row(settingsFieldDensity, "list density", density) + "\n" +
		row(settingsFieldSessionLayout, "sessions layout", sessionLayout) + "\n" +
		row(settingsFieldHeader, "header", header) + "\n" +
		row(settingsFieldStats, "computer stats", stats) + "\n" +
		row(settingsFieldLayout, "review layout", layout) + "\n" +
		row(settingsFieldQuickClose, "after quick send", quickClose) + "\n" +
		row(settingsFieldFocusKey, "session keys", focusKey) + "\n" +
		row(settingsFieldArrowStep, "←→ step in/out", arrowStep) + betaTag + "\n" +
		row(settingsFieldMouse, "mouse", mouseMode) + "\n" +
		row(settingsFieldWorktree, "spawn in worktree", worktreeDefault) + "\n" +
		row(settingsFieldNotify, "notifications", notifications) + "\n" +
		row(settingsFieldNotifyFinish, "notify on finish", notifyFinished) + "\n" +
		actionRow(settingsFieldKeybindings, "keybindings", keybindingsSummary(m.keys, m.listKeys)) + "\n" +
		actionRow(settingsFieldCLIs, "CLIs", "show or hide for new sessions") + "\n" +
		ctaRow(settingsFieldBugReport, "report a bug", "open the bug report form") + "\n" +
		ctaRow(settingsFieldFeatureRequest, "suggest a change", "open the feature request form") + "\n" +
		m.settingsVersionRow(lead, actionRow)
	hint := [][2]string{{"↑↓", "field"}, {"←→", "change"}, {"↵/esc", "save"}}
	switch m.settings.field {
	case settingsFieldBugReport, settingsFieldFeatureRequest:
		hint = [][2]string{{"↑↓", "field"}, {"↵", "open form"}, {"esc", "save"}}
	case settingsFieldCLIs:
		hint = [][2]string{{"↑↓", "field"}, {"↵", "manage CLIs"}, {"esc", "save"}}
	case settingsFieldKeybindings:
		hint = [][2]string{{"↑↓", "field"}, {"↵", "change the keys"}, {"esc", "save"}}
	case settingsFieldUpdate:
		switch {
		case m.update.applying:
			hint = [][2]string{{"↑↓", "field"}, {"esc", "save"}}
		case m.update.latest != "":
			hint = [][2]string{{"↑↓", "field"}, {"↵", "update"}, {"esc", "save"}}
		default:
			hint = [][2]string{{"↑↓", "field"}, {"↵/esc", "save"}}
		}
	}
	return m.cardFlex("⚙ Settings", body, hint)
}

// settingsVersionRow is the focusable version line: when a newer release is
// known it is an action row that starts the same in-place update as the
// messages modal's u key.
func (m *Model) settingsVersionRow(lead func(int, string) string, actionRow func(int, string, string) string) string {
	if m.update.applying {
		label := m.update.latest
		if label == "" {
			label = "update"
		}
		return lead(settingsFieldUpdate, "version") +
			lipgloss.NewStyle().Foreground(colorAccent).Render("↓ downloading "+label+"…")
	}
	if m.update.latest != "" {
		return actionRow(settingsFieldUpdate, "version "+m.update.version, "update to "+m.update.latest)
	}
	return lead(settingsFieldUpdate, "version") + valueStyle.Render(m.update.version)
}

func (m *Model) viewCLIPicker() string {
	var b strings.Builder
	for i, name := range m.settings.cliNames {
		marker := "  "
		labelStyle := valueStyle
		if m.settings.cliCursor == i {
			marker = lipgloss.NewStyle().Foreground(colorAccent).Render("❯ ")
			labelStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
		}
		box := "[x]"
		if m.settings.cliHidden[name] {
			box = "[ ]"
		}
		b.WriteString(marker)
		b.WriteString(labelStyle.Render(box + " " + name))
		b.WriteByte('\n')
	}
	// Request row matches other settings actions; the note below is not focusable.
	reqFocused := m.settings.cliCursor >= len(m.settings.cliNames)
	reqMarker := "  "
	reqLabel := mutedStyle.Render("request CLI support")
	if reqFocused {
		reqMarker = lipgloss.NewStyle().Foreground(colorAccent).Render("❯ ")
		reqLabel = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("request CLI support")
	}
	b.WriteByte('\n')
	b.WriteString(reqMarker)
	b.WriteString(keyStyle.Render("↵"))
	b.WriteString(" ")
	b.WriteString(reqLabel)
	b.WriteString(mutedStyle.Render("  open a GitHub issue"))
	b.WriteByte('\n')
	b.WriteString(subtleStyle.Render("  * more will be supported soon!"))
	hint := [][2]string{{"↑↓", "move"}, {"space/↵", "toggle"}, {"esc", "back"}}
	if reqFocused {
		hint = [][2]string{{"↑↓", "move"}, {"↵", "open request issue"}, {"esc", "back"}}
	}
	// Fixed card width: short checkbox rows must not stretch a wide empty panel.
	return m.card("⚙ CLIs", strings.TrimRight(b.String(), "\n"), hint)
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

func (m *Model) viewKeyPicker() string {
	tables := m.settings.tables
	if m.settings.keyReset {
		return m.confirmCard("↺ Reset keys", "Reset every key to its default?",
			strings.Join(keyResetChanges(tables...), "\n"), true, "reset")
	}
	rows := keyRowsOf(tables)
	first, last := pickerWindow(len(rows), m.settings.keyCursor, m.height-14)
	var b strings.Builder
	if first > 0 {
		b.WriteString(subtleStyle.Render(fmt.Sprintf("  ↑ %d more", first)) + "\n")
	}
	for i := first; i < last; i++ {
		row := rows[i]
		keys := tables[row.table]
		section := keySectionFor(keys)
		if i == first || rows[i-1].table != row.table {
			if i > first {
				b.WriteByte('\n')
			}
			b.WriteString(annotationStyle.Render("  "+section.title) + "\n")
		}
		marker := "  "
		labelStyle := valueStyle
		if m.settings.keyCursor == i {
			marker = lipgloss.NewStyle().Foreground(colorAccent).Render("❯ ")
			labelStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
		}
		value := keys.Binding(row.action.Name).Label()
		valueRender := valueStyle.Render(value)
		if value == "" {
			valueRender = subtleStyle.Render(section.off)
		}
		if m.settings.keyCapture && m.settings.keyCursor == i {
			word := "press a key"
			if m.settings.keyAppend {
				word = "press a key to add"
			}
			valueRender = lipgloss.NewStyle().Foreground(colorAccent).Render(word + "…")
		}
		b.WriteString(marker)
		b.WriteString(padRight(labelStyle.Render(row.action.Name), 14))
		b.WriteString(padRight(valueRender, 26))
		b.WriteString(mutedStyle.Render(row.action.Does))
		b.WriteByte('\n')
	}
	if last < len(rows) {
		b.WriteString(subtleStyle.Render(fmt.Sprintf("  ↓ %d more", len(rows)-last)) + "\n")
	}
	hint := [][2]string{{"↑↓", "move"}, {"↵", "set a key"}, {"a", "add one"}, {"d", "off"}, {"r", "defaults"}, {"esc", "back"}}
	if m.settings.keyCapture {
		hint = [][2]string{{"any key", "bind it"}, {"esc", "cancel"}}
	}
	return m.cardFlex("⚙ Keybindings", strings.TrimRight(b.String(), "\n"), hint)
}

func pickerWindow(count, cursor, room int) (first, last int) {
	visible := max(min(count, room), 5)
	if visible >= count {
		return 0, count
	}
	first = cursor - visible/2
	if first < 0 {
		first = 0
	}
	if first+visible > count {
		first = count - visible
	}
	return first, first + visible
}
