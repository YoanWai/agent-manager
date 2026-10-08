package ui

import (
	"github.com/charmbracelet/lipgloss"
	"strings"
)

// viewSettings is the root's frame for the dialog.
func (m *Model) viewSettings() string {
	return m.settings.view(m)
}

func (s *settingsFeature) view(h settingsViewHost) string {
	if s.dialog.cliPicker {
		return s.viewCLIPicker(h)
	}
	if s.dialog.keyPicker {
		return s.viewKeyPicker(h)
	}
	layout := "unified"
	if s.dialog.layoutSplit {
		layout = "split"
	}
	density := "compact"
	if s.dialog.comfortableRows {
		density = "comfortable"
	}
	sessionLayout := "split"
	if s.dialog.fullLayout {
		sessionLayout = "full screen"
	}
	header := "show"
	if s.dialog.hideHeader {
		header = "hide"
	}
	stats := "show"
	if s.dialog.hideStats {
		stats = "hide"
	}
	quickClose := "stay open"
	if s.dialog.quickCloseSend {
		quickClose = "close"
	}
	focusKey := "↵ focus · A attach"
	if !s.dialog.enterFocuses {
		focusKey = "↵ attach · A focus"
	}
	worktreeDefault := "off"
	if s.dialog.worktreeDefault {
		worktreeDefault = "on"
	}
	baseFetch := "off"
	if s.dialog.baseFetch {
		baseFetch = "on"
	}
	coordination := "on request"
	if s.dialog.proactive {
		coordination = "proactive"
	}
	arrowStep := "off"
	if s.dialog.arrowStep {
		arrowStep = "on"
	}
	mouseMode := "on"
	if s.dialog.mouseDisabled {
		mouseMode = "off"
	}
	// The beta tag borrows the messages modal's yellow, so the row reads as
	// the one still under test.
	betaTag := lipgloss.NewStyle().Foreground(lipgloss.Color("#e2c044")).Render(" beta")
	themeAuto := "off"
	if s.dialog.themeAuto {
		themeAuto = "on"
	}
	background := "theme"
	if s.dialog.terminalBackground {
		background = "terminal"
	}
	notifications := "off"
	if s.dialog.notifications {
		notifications = "on"
	}
	notifyFinished := "off"
	if s.dialog.notifyFinished {
		notifyFinished = "on"
	}
	toolValue := ""
	if len(s.dialog.toolNames) > 0 {
		toolValue = s.dialog.toolNames[s.dialog.toolIndex]
	}
	lead := func(field int, name string) string {
		marker := "  "
		labelStyle := valueStyle
		if s.dialog.field == field {
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
		if s.dialog.field == field {
			marker = lipgloss.NewStyle().Foreground(colorAccent).Render("❯ ")
			labelStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
		}
		return marker + padRight(labelStyle.Render(name), 18)
	}
	ctaRow := func(field int, name, action string) string {
		return ctaLead(field, name) + keyStyle.Render("↵") + " " +
			lipgloss.NewStyle().Foreground(colorAccent2).Render(action)
	}
	editorLine := row(settingsFieldEditor, "editor", s.dialog.editor.label())
	if s.dialog.editor.typing {
		editorLine = lead(settingsFieldEditor, "editor") + textInputView(s.dialog.editor.input)
	}
	body := row(settingsFieldTool, "default tool", toolValue) + "\n" +
		row(settingsFieldTheme, "theme", themes[s.dialog.themeIndex].Name) + "  " +
		themeSwatch(themes[s.dialog.themeIndex]) + "\n" +
		row(settingsFieldThemeAuto, "theme follows OS", themeAuto) + "\n" +
		row(settingsFieldBackground, "background", background) + "\n" +
		row(settingsFieldDensity, "list density", density) + "\n" +
		row(settingsFieldSessionLayout, "sessions layout", sessionLayout) + "\n" +
		row(settingsFieldHeader, "header", header) + "\n" +
		row(settingsFieldStats, "computer stats", stats) + "\n" +
		row(settingsFieldLayout, "review layout", layout) + "\n" +
		row(settingsFieldQuickClose, "after quick prompt", quickClose) + "\n" +
		row(settingsFieldFocusKey, "session keys", focusKey) + "\n" +
		row(settingsFieldArrowStep, "←→ step in/out", arrowStep) + betaTag + "\n" +
		row(settingsFieldMouse, "mouse", mouseMode) + "\n" +
		row(settingsFieldWorktree, "spawn in worktree", worktreeDefault) + "\n" +
		row(settingsFieldBaseFetch, "fetch on spawn", baseFetch) + "\n" +
		row(settingsFieldCoordination, "coordination", coordination) + "\n" +
		row(settingsFieldNotify, "notifications", notifications) + "\n" +
		row(settingsFieldNotifyFinish, "notify on finish", notifyFinished) + "\n" +
		editorLine + "\n" +
		actionRow(settingsFieldKeybindings, "keybindings", keybindingsSummary(h.keyTables())) + "\n" +
		actionRow(settingsFieldCLIs, "CLIs", "show or hide for new sessions") + "\n" +
		ctaRow(settingsFieldBugReport, "report a bug", "open the bug report form") + "\n" +
		ctaRow(settingsFieldFeatureRequest, "suggest a change", "open the feature request form") + "\n" +
		s.settingsVersionRow(h, lead, actionRow)
	hint := [][2]string{{"↑↓", "field"}, {"←→", "change"}, {"↵/esc", "save"}}
	switch s.dialog.field {
	case settingsFieldBugReport, settingsFieldFeatureRequest:
		hint = [][2]string{{"↑↓", "field"}, {"↵", "open form"}, {"esc", "save"}}
	case settingsFieldCLIs:
		hint = [][2]string{{"↑↓", "field"}, {"↵", "manage CLIs"}, {"esc", "save"}}
	case settingsFieldKeybindings:
		hint = [][2]string{{"↑↓", "field"}, {"↵", "change the keys"}, {"esc", "save"}}
	case settingsFieldEditor:
		switch {
		case s.dialog.editor.typing:
			hint = [][2]string{{"↵", "keep"}, {"esc", "cancel"}}
		case s.dialog.editor.custom:
			hint = [][2]string{{"↑↓", "field"}, {"←→", "change"}, {"↵", "type the command"}, {"esc", "save"}}
		}
	case settingsFieldUpdate:
		_, latest, applying := h.release()
		switch {
		case applying:
			hint = [][2]string{{"↑↓", "field"}, {"esc", "save"}}
		case latest != "":
			hint = [][2]string{{"↑↓", "field"}, {"↵", "update"}, {"esc", "save"}}
		default:
			hint = [][2]string{{"↑↓", "field"}, {"↵/esc", "save"}}
		}
	}
	return h.cardFlex("⚙ Settings", body, hint)
}

// settingsVersionRow is the focusable version line: when a newer release is
// known it is an action row that starts the same in-place update as the
// messages modal's u key.
func (s *settingsFeature) settingsVersionRow(h settingsViewHost, lead func(int, string) string, actionRow func(int, string, string) string) string {
	version, latest, applying := h.release()
	if applying {
		label := latest
		if label == "" {
			label = "update"
		}
		return lead(settingsFieldUpdate, "version") +
			lipgloss.NewStyle().Foreground(colorAccent).Render("↓ downloading "+label+"…")
	}
	if latest != "" {
		return actionRow(settingsFieldUpdate, "version "+version, "update to "+latest)
	}
	return lead(settingsFieldUpdate, "version") + valueStyle.Render(version)
}

func (s *settingsFeature) viewCLIPicker(h settingsViewHost) string {
	var b strings.Builder
	for i, name := range s.dialog.cliNames {
		marker := "  "
		labelStyle := valueStyle
		if s.dialog.cliCursor == i {
			marker = lipgloss.NewStyle().Foreground(colorAccent).Render("❯ ")
			labelStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
		}
		box := "[x]"
		if s.dialog.cliHidden[name] {
			box = "[ ]"
		}
		b.WriteString(marker)
		b.WriteString(labelStyle.Render(box + " " + name))
		b.WriteByte('\n')
	}
	// Request row matches other settings actions; the note below is not focusable.
	reqFocused := s.dialog.cliCursor >= len(s.dialog.cliNames)
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
	return h.card("⚙ CLIs", strings.TrimRight(b.String(), "\n"), hint)
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
