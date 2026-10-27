package settings

import (
	"slices"
	"strings"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Styles are the root's live styles the dialog paints with.
type Styles struct {
	Value, Subtle, Muted, Key, Annotation lipgloss.Style
	// Marker is the focused row's caret; Focused its label.
	Marker, Focused lipgloss.Style
	// Action is an unfocused call-to-action label; ActionText its words.
	Action, ActionText lipgloss.Style
}

// ViewContext is what painting reads from the root: the live key tables,
// the release facts, the room the key picker scrolls in, the styles, and
// the root's text-field renderer, which marks the IME cursor. Chrome is the
// rows the card spends around a body with this hint.
type ViewContext struct {
	GlyphMode     presentation.GlyphMode
	Session, List keybind.Table
	// Envs are the environment profiles the shell defined, which the
	// dialog lists and never edits.
	Envs            []Env
	Version, Latest string
	Applying        bool
	Height          int
	InnerWidth      int
	Chrome          func(hint [][2]string) int
	Styles          Styles
	TextInput       func(textinput.Model) string
	TextArea        func(textarea.Model) string
}

// Card is how the root frames the dialog.
type Card uint8

const (
	// Flex grows with its rows; Fixed keeps the default width; Confirm asks
	// the Question with Consequence under it.
	Flex Card = iota
	Fixed
	Confirm
)

// Frame is a painted dialog for the root's card chrome.
type Frame struct {
	Card        Card
	Title       string
	Body        string
	Hints       [][2]string
	Question    string
	Consequence string
	Answer      string
	Destructive bool
}

// View paints the open dialog.
func (s *State) View(ctx ViewContext) Frame {
	chrome := presentation.ChromeFor(ctx.GlyphMode)
	if s.Prompts.open {
		return s.Prompts.view(ctx)
	}
	if s.CLIPicker {
		return s.viewCLIPicker(ctx)
	}
	if s.KeyPicker {
		return s.viewKeyPicker(ctx)
	}
	st := ctx.Styles
	// The beta tag borrows the messages modal's yellow, so the row reads as
	// the one still under test.
	betaTag := lipgloss.NewStyle().Foreground(lipgloss.Color("#e2c044")).Render(" beta")
	toolValue := ""
	if len(s.ToolNames) > 0 {
		toolValue = s.ToolNames[s.ToolIndex]
	}
	lead := func(field int, name string) string {
		marker := "  "
		labelStyle := st.Value
		if s.Field == field {
			marker = st.Marker.Render(presentation.GlyphsFor(ctx.GlyphMode).SelectionGlyph + " ")
			labelStyle = st.Focused
		}
		return marker + presentation.PadRight(labelStyle.Render(name), 18)
	}
	row := func(field int, name, value string) string {
		return lead(field, name) + st.Subtle.Render(chrome.ChoiceLeftGlyph+" ") + st.Value.Render(value) + st.Subtle.Render(" "+chrome.ChoiceRightGlyph)
	}
	// An action row: enter runs it, so it carries no picker arrows.
	actionRow := func(field int, name, action string) string {
		return lead(field, name) + st.Key.Render(presentation.KeyLabel(ctx.GlyphMode, "↵")) + st.Muted.Render(" "+action)
	}
	// Report and suggest stay accent-colored even when unfocused so the
	// actions read as a call-to-action among the picker rows above them.
	ctaLead := func(field int, name string) string {
		marker := "  "
		labelStyle := st.Action
		if s.Field == field {
			marker = st.Marker.Render(presentation.GlyphsFor(ctx.GlyphMode).SelectionGlyph + " ")
			labelStyle = st.Focused
		}
		return marker + presentation.PadRight(labelStyle.Render(name), 18)
	}
	ctaRow := func(field int, name, action string) string {
		return ctaLead(field, name) + st.Key.Render(presentation.KeyLabel(ctx.GlyphMode, "↵")) + " " + st.ActionText.Render(action)
	}
	label := func(field int) string { return byField[field].Row }
	editorLine := row(FieldEditor, label(FieldEditor), s.Editor.Label())
	if s.Editor.typing {
		editorLine = lead(FieldEditor, label(FieldEditor)) + ctx.TextInput(s.Editor.input)
	}
	g := presentation.GlyphsFor(s.GlyphMode)
	glyphLabel := "Compatible"
	if s.GlyphMode.Effective() == presentation.GlyphModeAdvanced {
		glyphLabel = "Advanced"
	}
	symbolsRow := row(FieldGlyphs, label(FieldGlyphs), glyphLabel) + "  " + g.WorkingGlyph + " " + g.WaitingGlyph + " " + g.ReorderGrip + " " + g.BranchGlyph
	if ctx.InnerWidth < 36 {
		style := st.Value
		if s.Field == FieldGlyphs {
			style = st.Key
		}
		symbolsRow = style.Render(label(FieldGlyphs) + " <" + glyphLabel + ">")
	}
	palette := uitheme.Palettes[s.ThemeIndex]
	// The environments row appears once a profile exists: most people never
	// define one, and every row they never use crowds the ones they do.
	s.envsHidden = len(ctx.Envs) == 0
	if s.envsHidden && s.Field == FieldEnvs {
		s.Field = FieldDocs
	}
	fields := make([]int, 0, FieldCount)
	lines := make([]string, 0, FieldCount)
	for field := range FieldCount {
		if field == FieldEnvs && s.envsHidden {
			continue
		}
		fields = append(fields, field)
		switch field {
		case FieldTool:
			lines = append(lines, row(FieldTool, label(FieldTool), toolValue))
		case FieldTheme:
			lines = append(lines, row(FieldTheme, label(FieldTheme), palette.Name)+"  "+themeSwatch(palette))
		case FieldGlyphs:
			lines = append(lines, symbolsRow)
		case FieldEditor:
			lines = append(lines, editorLine)
		case FieldKeybindings:
			lines = append(lines, actionRow(FieldKeybindings, "keybindings", keybindingsSummary(ctx.Session, ctx.List)))
		case FieldCLIs:
			lines = append(lines, actionRow(FieldCLIs, label(FieldCLIs), "show or hide for new sessions"))
		case FieldPrompts:
			lines = append(lines, actionRow(FieldPrompts, "saved prompts", "manage named instructions"))
		case FieldEnvs:
			lines = append(lines, lead(FieldEnvs, "environments")+st.Muted.Render(envSummary(ctx.Envs)))
		case FieldDocs:
			lines = append(lines, ctaRow(FieldDocs, "docs", "open the docs"))
		case FieldBugReport:
			lines = append(lines, ctaRow(FieldBugReport, "report a bug", "open the bug report form"))
		case FieldFeatureRequest:
			lines = append(lines, ctaRow(FieldFeatureRequest, "suggest a change", "open the feature request form"))
		case FieldUpdate:
			lines = append(lines, versionRow(ctx, lead, actionRow))
		default:
			setting := byField[field]
			name := setting.Row
			if field == FieldArrowStep && ctx.GlyphMode.Effective() == presentation.GlyphModeBasic {
				name = "arrow step in/out"
			}
			line := row(field, name, presentation.KeyLabel(ctx.GlyphMode, setting.label(*setting.Flag(s))))
			if setting.Beta {
				line += betaTag
			}
			lines = append(lines, line)
		}
	}
	hint := [][2]string{{presentation.NavigationLabel(ctx.GlyphMode, "↑↓"), "field"}, {presentation.NavigationLabel(ctx.GlyphMode, "←→"), "change"}, {"↵/esc", "save and close"}}
	switch s.Field {
	case FieldGlyphs:
		hint = [][2]string{{presentation.NavigationLabel(ctx.GlyphMode, "↑↓"), "field"}, {presentation.NavigationLabel(ctx.GlyphMode, "←→/click"), "symbols"}, {"↵/esc", "save and close"}}
	case FieldDocs:
		hint = [][2]string{{presentation.NavigationLabel(ctx.GlyphMode, "↑↓"), "field"}, {"↵", "open the docs"}, {"esc", "save and close"}}
	case FieldBugReport, FieldFeatureRequest:
		hint = [][2]string{{presentation.NavigationLabel(ctx.GlyphMode, "↑↓"), "field"}, {"↵", "open form"}, {"esc", "save and close"}}
	case FieldCLIs:
		hint = [][2]string{{presentation.NavigationLabel(ctx.GlyphMode, "↑↓"), "field"}, {"↵", "manage CLIs"}, {"esc", "save and close"}}
	case FieldPrompts:
		hint = [][2]string{{presentation.NavigationLabel(ctx.GlyphMode, "↑↓"), "field"}, {"↵", "saved prompts"}, {"esc", "save and close"}}
	case FieldEnvs:
		hint = [][2]string{{presentation.NavigationLabel(ctx.GlyphMode, "↑↓"), "field"}, {"↵/esc", "save and close"}}
	case FieldKeybindings:
		hint = [][2]string{{presentation.NavigationLabel(ctx.GlyphMode, "↑↓"), "field"}, {"↵", "change the keys"}, {"esc", "save and close"}}
	case FieldEditor:
		switch {
		case s.Editor.typing:
			hint = [][2]string{{"↵", "keep"}, {"esc", "cancel"}}
		case s.Editor.custom:
			hint = [][2]string{{presentation.NavigationLabel(ctx.GlyphMode, "↑↓"), "field"}, {presentation.NavigationLabel(ctx.GlyphMode, "←→"), "change"}, {"↵", "type the command"}, {"esc", "save and close"}}
		}
	case FieldUpdate:
		switch {
		case ctx.Applying:
			hint = [][2]string{{presentation.NavigationLabel(ctx.GlyphMode, "↑↓"), "field"}, {"esc", "save and close"}}
		case ctx.Latest != "":
			hint = [][2]string{{presentation.NavigationLabel(ctx.GlyphMode, "↑↓"), "field"}, {"↵", "update"}, {"esc", "save and close"}}
		default:
			hint = [][2]string{{presentation.NavigationLabel(ctx.GlyphMode, "↑↓"), "field"}, {"↵/esc", "save and close"}}
		}
	}
	return Frame{Card: Flex, Title: presentation.GlyphsFor(ctx.GlyphMode).SettingsGlyph + " Settings", Body: s.fitRows(ctx, fields, lines, hint), Hints: hint}
}

// fitRows windows the rows that fit, held still while the focused row stays
// on screen, and records what each painted line shows. fields names the
// field each row paints.
func (s *State) fitRows(ctx ViewContext, fields []int, rows []string, hint [][2]string) string {
	room := max(ctx.Height-ctx.Chrome(hint), 1)
	s.first = presentation.Follow(s.first, slices.Index(fields, s.Field), len(rows), min(room, len(rows)))
	rows = rows[s.first:min(s.first+room, len(rows))]
	s.glyphWidth = 0
	s.lines.Reset()
	for i, row := range rows {
		field := fields[s.first+i]
		s.lines.Show(field, 1)
		if field == FieldGlyphs {
			s.glyphWidth = ansi.StringWidth(row)
		}
	}
	return strings.Join(rows, "\n")
}

// versionRow is the focusable version line: when a newer release is known
// it is an action row that starts the same in-place update as the messages
// modal's u key.
func versionRow(ctx ViewContext, lead func(int, string) string, actionRow func(int, string, string) string) string {
	if ctx.Applying {
		label := ctx.Latest
		if label == "" {
			label = "update"
		}
		return lead(FieldUpdate, "version") + ctx.Styles.Marker.Render(presentation.GlyphsFor(ctx.GlyphMode).DownloadGlyph+" downloading "+label+"…")
	}
	if ctx.Latest != "" {
		return actionRow(FieldUpdate, "version "+ctx.Version, "update to "+ctx.Latest)
	}
	return lead(FieldUpdate, "version") + ctx.Styles.Value.Render(ctx.Version)
}

func (s *State) viewCLIPicker(ctx ViewContext) Frame {
	st := ctx.Styles
	var b strings.Builder
	s.lines.Reset()
	for i, name := range s.CLINames {
		marker := "  "
		labelStyle := st.Value
		if s.CLICursor == i {
			marker = st.Marker.Render(presentation.GlyphsFor(ctx.GlyphMode).SelectionGlyph + " ")
			labelStyle = st.Focused
		}
		box := "[x]"
		if s.CLIHidden[name] {
			box = "[ ]"
		}
		b.WriteString(marker)
		b.WriteString(labelStyle.Render(box + " " + name))
		b.WriteByte('\n')
		s.lines.Show(i, 1)
	}
	// Request row matches other settings actions; the note below is not focusable.
	reqFocused := s.CLICursor >= len(s.CLINames)
	reqMarker := "  "
	reqLabel := st.Muted.Render("request CLI support")
	if reqFocused {
		reqMarker = st.Marker.Render(presentation.GlyphsFor(ctx.GlyphMode).SelectionGlyph + " ")
		reqLabel = st.Focused.Render("request CLI support")
	}
	b.WriteByte('\n')
	b.WriteString(reqMarker)
	b.WriteString(st.Key.Render(presentation.KeyLabel(ctx.GlyphMode, "↵")))
	b.WriteString(" ")
	b.WriteString(reqLabel)
	b.WriteString(st.Muted.Render("  open a GitHub issue"))
	b.WriteByte('\n')
	b.WriteString(st.Subtle.Render("  * more will be supported soon!"))
	s.lines.Skip(1)
	s.lines.Show(len(s.CLINames), 1)
	hint := [][2]string{{presentation.NavigationLabel(ctx.GlyphMode, "↑↓"), "move"}, {"space/↵", "toggle"}, {"esc", "back"}}
	if reqFocused {
		hint = [][2]string{{presentation.NavigationLabel(ctx.GlyphMode, "↑↓"), "move"}, {"↵", "open request issue"}, {"esc", "back"}}
	}
	// Fixed card width: short checkbox rows must not stretch a wide empty panel.
	return Frame{Card: Fixed, Title: presentation.GlyphsFor(ctx.GlyphMode).SettingsGlyph + " CLIs", Body: strings.TrimRight(b.String(), "\n"), Hints: hint}
}

// themeSwatch previews a palette as a run of blocks, so a theme can be
// picked by eye rather than by name.
func themeSwatch(t uitheme.Theme) string {
	var b strings.Builder
	for _, hex := range []string{t.Accent, t.Accent2, t.Working, t.Waiting, t.Finished, t.Errored} {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(hex)).Render("█"))
	}
	return b.String()
}
