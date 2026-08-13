package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m *Model) cardWidth() int {
	width := 64
	if m.width >= 28 && width > m.width-4 {
		width = m.width - 4
	}
	return width
}

const (
	cardPaddingX = 3
	// cardChromeX is the two border columns a card spends on its frame.
	cardChromeX = 2
)

// cardBorderStyle is the card's frame: the theme's border tone pulled toward
// the accent, so a dialog reads as the app's own surface rather than as a
// box drawn around it.
func cardBorderStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(mix(current.Border, current.Accent, 0.35)))
}

// card floats a modal on the app backdrop: a framed panel with its title set
// into the top edge and its keys on a foot below a hairline.
func (m *Model) card(title, body string, hint [][2]string) string {
	return m.cardSized(m.cardWidth(), title, body, hint)
}

// cardFlex is card, but the panel grows with its content up to the terminal
// width so long settings rows are not clipped.
func (m *Model) cardFlex(title, body string, hint [][2]string) string {
	return m.cardSized(m.flexCardWidth(title, body, hint), title, body, hint)
}

func cardInnerWidth(width int) int { return width - cardChromeX - 2*cardPaddingX }

// flexCardWidth picks a width that fits every content line, never under the
// default card width and never past the terminal edge.
func (m *Model) flexCardWidth(title, body string, hint [][2]string) int {
	need := m.cardWidth()
	measure := func(s string) {
		for _, line := range strings.Split(s, "\n") {
			if w := lipgloss.Width(line) + cardChromeX + 2*cardPaddingX; w > need {
				need = w
			}
		}
	}
	measure(body)
	measure(cardTitle(title))
	measure(legendInline(hint, 1<<30))
	if m.errBar.text != "" {
		measure(errStyle.Render("⚠ " + m.errBar.text))
	}
	if m.width >= 28 && need > m.width-4 {
		need = m.width - 4
	}
	return need
}

func cardTitle(title string) string {
	return lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(title)
}

// cardSized is card at an explicit width, for the key map, whose lines are
// too long to read inside the default column.
func (m *Model) cardSized(width int, title, body string, hint [][2]string) string {
	inner := cardInnerWidth(width)
	border := cardBorderStyle()
	pad := strings.Repeat(" ", cardPaddingX)
	edge := border.Render("│")

	row := func(line string) string {
		return paint(edge+pad+padRight(line, inner)+pad+edge, width, blockHex())
	}
	rule := func(left, right string) string {
		return paint(border.Render(left+strings.Repeat("─", width-2)+right), width, blockHex())
	}

	lines := []string{cardTitleRow(width, title, border), row("")}
	for _, line := range strings.Split(body, "\n") {
		lines = append(lines, row(line))
	}
	if m.errBar.text != "" {
		lines = append(lines, row(""), row(errStyle.Render("⚠ "+m.errBar.text)))
	}
	lines = append(lines, row(""))
	if len(hint) > 0 {
		lines = append(lines, rule("├", "┤"))
		for _, line := range strings.Split(legendInline(hint, inner), "\n") {
			lines = append(lines, row(line))
		}
	}
	lines = append(lines, rule("╰", "╯"))
	return m.centerOnBackdrop(lines)
}

// cardTitleRow sets the title into the top edge, so the frame names the
// dialog instead of spending a content row on it.
func cardTitleRow(width int, title string, border lipgloss.Style) string {
	label := " " + cardTitle(title) + " "
	dashes := width - 4 - lipgloss.Width(label)
	if dashes < 0 {
		dashes = 0
	}
	head := border.Render("╭──") + label + border.Render(strings.Repeat("─", dashes)+"╮")
	return paint(head, width, blockHex())
}

// centerOnBackdrop floats a block of pre-painted lines in the middle of
// the app frame, filling the rest with the backdrop.
func (m *Model) centerOnBackdrop(box []string) string {
	width := maxLineWidth(box)
	height := max(m.height, len(box))
	left := max((m.width-width)/2, 0)
	frameWidth := max(m.width, left+width)
	top := max((height-len(box))/2, 0)
	frame := make([]string, 0, height)
	for i := 0; i < height; i++ {
		row := ""
		if i >= top && i-top < len(box) {
			row = paint("", left, backdropHex()) + box[i-top]
		}
		frame = append(frame, paint(row, frameWidth, backdropHex()))
	}
	return strings.Join(frame, "\n")
}

func maxLineWidth(lines []string) int {
	width := 0
	for _, line := range lines {
		if w := lipgloss.Width(line); w > width {
			width = w
		}
	}
	return width
}

func (m *Model) viewForm() string {
	m.form.prompt.SetHeight(textareaRows(m.form.prompt, m.formValueWidth()-2, formPromptMaxRows))

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
	worktreeField := subtleStyle.Render(worktreeUnavailable)
	if m.worktreeCapable(m.formSpawnDir()) {
		worktreeVal := "off"
		if m.form.worktree {
			worktreeVal = "on"
		}
		worktreeField = subtleStyle.Render("◂ ") + valueStyle.Render(worktreeVal) + subtleStyle.Render(" ▸")
	}
	b.WriteString(formField("worktree", worktreeField, m.form.focus == fieldWorktree))
	b.WriteString(formField("prompt", m.form.prompt.View(), m.form.focus == fieldPrompt))
	b.WriteString(formField("group", groupBadge(displayGroup(m.form.groups[m.form.groupIndex].path)), m.form.focus == fieldGroup))

	if m.form.focus == fieldGroup {
		b.WriteString("\n" + m.viewGroupPicker())
	}

	hint := [][2]string{{"tab/↑↓", "move"}, {"←→", "change"}, {"↵", "create"}, {"esc", "cancel"}}
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

func (m *Model) viewSettings() string {
	if m.settings.cliPicker {
		return m.viewCLIPicker()
	}
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
	arrowStep := "off"
	if m.settings.arrowStep {
		arrowStep = "on"
	}
	// The beta tag borrows the messages card's yellow, so the row reads as
	// the one still under test.
	betaTag := lipgloss.NewStyle().Foreground(lipgloss.Color("#e2c044")).Render(" beta")
	themeAuto := "off"
	if m.settings.themeAuto {
		themeAuto = "on"
	}
	terminals := "inline"
	if m.settings.shellsPinned {
		terminals = "pinned"
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
	// Bug report stays accent-colored even when unfocused so the action
	// reads as a call-to-action among the picker rows above it.
	bugLead := func() string {
		marker := "  "
		labelStyle := lipgloss.NewStyle().Foreground(colorAccent2).Bold(true)
		if m.settings.field == settingsFieldBugReport {
			marker = lipgloss.NewStyle().Foreground(colorAccent).Render("❯ ")
			labelStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
		}
		return marker + padRight(labelStyle.Render("report a bug"), 18)
	}
	bugRow := bugLead() + keyStyle.Render("↵") + " " +
		lipgloss.NewStyle().Foreground(colorAccent2).Render("open a prefilled GitHub issue")
	bugNote := subtleStyle.Render("  * found a bug? report it (prefilled)")
	body := row(settingsFieldTool, "default tool", toolValue) + "\n" +
		row(settingsFieldTheme, "theme", themes[m.settings.themeIndex].Name) + "  " +
		themeSwatch(themes[m.settings.themeIndex]) + "\n" +
		row(settingsFieldThemeAuto, "theme follows OS", themeAuto) + "\n" +
		row(settingsFieldDensity, "list density", density) + "\n" +
		row(settingsFieldLayout, "review layout", layout) + "\n" +
		row(settingsFieldQuickClose, "after quick send", quickClose) + "\n" +
		row(settingsFieldFocusKey, "session keys", focusKey) + "\n" +
		row(settingsFieldArrowStep, "←→ step in/out", arrowStep) + betaTag + "\n" +
		row(settingsFieldWorktree, "spawn in worktree", worktreeDefault) + "\n" +
		row(settingsFieldTerminals, "terminal rows", terminals) + "\n" +
		row(settingsFieldNotify, "notifications", notifications) + "\n" +
		row(settingsFieldNotifyFinish, "notify on finish", notifyFinished) + "\n" +
		actionRow(settingsFieldCLIs, "CLIs", "show or hide for new sessions") + "\n" +
		bugRow + "\n" +
		bugNote + "\n" +
		m.settingsVersionRow(lead, actionRow)
	hint := [][2]string{{"↑↓", "field"}, {"←→", "change"}, {"↵/esc", "save"}}
	switch m.settings.field {
	case settingsFieldBugReport:
		hint = [][2]string{{"↑↓", "field"}, {"↵", "open issue"}, {"esc", "save"}}
	case settingsFieldCLIs:
		hint = [][2]string{{"↑↓", "field"}, {"↵", "manage CLIs"}, {"esc", "save"}}
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

func (m *Model) viewMove() string {
	return m.card("⇄ Move to group", m.viewGroupPicker(),
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
