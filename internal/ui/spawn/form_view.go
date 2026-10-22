package spawn

import (
	"fmt"
	"strings"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// fieldLine is a body line's field, and its list entry or -1.
type fieldLine struct {
	field Field
	entry int
}

// LabelColumn is the columns before a field value: marker (2), label (13),
// separator space (1).
const LabelColumn = 16

// savedListRows caps the saved prompts list under its row.
const savedListRows = 6

// FormFacts are the root facts the form paints: the width a value has,
// what the chosen directory allows, and the group picker the root holds.
type FormFacts struct {
	Width                          int
	WorktreeKnown, WorktreeCapable bool
	// NoRepo is the worktree row of a directory that cannot host one.
	NoRepo string
	// Base names the ref a worktree spawn branches from, set while the
	// worktree is on.
	Base string
	// Group is the picked group as its row names it, and GroupPicker the
	// picker's lines, painted under it while it has focus.
	Group       string
	GroupPicker []string
	// Host is the painted name of the host a spawn goes to when that is
	// not this one: the worktree is its default until picked, and there is
	// no base to show.
	Host   string
	Styles Styles
	// Height is the terminal's, and Chrome the rows the card spends around
	// a body with this hint, which together budget the saved prompt lines.
	Height int
	Chrome func(hint [][2]string) int
}

// View paints the form's body and its key hint, and records the line each
// field takes for clicks.
func (d *Form) View(h FormHost, facts FormFacts) (string, [][2]string) {
	st := facts.Styles
	var head, tail strings.Builder
	var headLines, tailLines presentation.Lines[fieldLine]
	b, lines := &head, &headLines
	add := func(text string, at fieldLine) {
		b.WriteString(text)
		lines.Show(at, strings.Count(text, "\n"))
	}
	field := func(label, value string, id Field) {
		add(FormField(st, label, value, d.focus == id), fieldLine{field: id, entry: -1})
	}
	field("name", st.Input(d.name), FieldName)

	toolVal := "(none configured)"
	if len(d.toolNames) > 0 {
		toolVal = st.Subtle.Render("◂ ") + st.Value.Render(d.toolNames[d.toolIndex]) + st.Subtle.Render(" ▸")
	}
	field("tool", toolVal, FieldTool)
	toolName, ch := d.Tool(), &d.choice
	if value, shown := ch.ProfileRow(h, toolName, st); shown {
		field("profile", value, FieldProfile)
	}
	if note, listed := ModelNote(h, toolName, st); !listed {
		field("model", ansi.Wrap(note, facts.Width, ""), FieldModel)
	} else {
		field("model", ch.FilterView(st), FieldModel)
		if d.focus == FieldModel && ch.sugg.open {
			lines, entries := ch.ViewSuggestions(h, toolName, LabelColumn, facts.Width, ModelListRows, st)
			for i, line := range lines {
				add(line+"\n", fieldLine{field: FieldModel, entry: entries[i]})
			}
		}
	}
	if value, shown, _ := ch.EffortRow(h, toolName, st); shown {
		field("effort", value, FieldEffort)
	}
	field("dir", st.Input(d.dir), FieldDir)
	paths := h.Paths()
	if d.focus == FieldDir && paths.Active() {
		for i, line := range strings.Split(paths.View(), "\n") {
			add(line+"\n", fieldLine{field: FieldDir, entry: i})
		}
	}
	worktreeField := st.Subtle.Render(facts.NoRepo)
	if facts.WorktreeKnown && facts.WorktreeCapable {
		worktreeVal := "off"
		switch {
		case facts.Host != "" && d.worktreeAuto:
			worktreeVal = "host default"
		case d.worktree:
			worktreeVal = "on"
		}
		worktreeField = st.Subtle.Render("◂ ") + st.Value.Render(worktreeVal) + st.Subtle.Render(" ▸")
	}
	field("worktree", worktreeField, FieldWorktree)
	if d.worktree && facts.WorktreeKnown && facts.WorktreeCapable && facts.Host == "" {
		field("base", facts.Base, fieldBase)
	}
	if value, shown := d.savedRow(facts); shown {
		field("saved prompts", value, FieldSaved)
	}
	b, lines = &tail, &tailLines
	// Chips are tokens inside the typed text, so they wrap and reflow with
	// the words around them; painting happens on the rendered prompt.
	field("prompt", h.Prompt().View(), FieldPrompt)
	group := lipgloss.NewStyle().Foreground(st.Accent2).Render(facts.Group)
	if facts.Host != "" {
		group = facts.Host + st.Subtle.Render(" · ") + group
	}
	field("group", group, FieldGroup)

	if d.focus == FieldGroup {
		add("\n", fieldLine{field: FieldGroup, entry: -1})
		for i, line := range facts.GroupPicker {
			add(line+"\n", fieldLine{field: FieldGroup, entry: i})
		}
	}

	hint := [][2]string{{"tab/↑↓", "move"}, {"←→", "change"}, {"↵", "create"}, {"esc", "cancel"}}
	switch {
	case d.savedList.Open():
		hint = d.savedList.Hint("use", "close")
	case d.focus == FieldSaved:
		hint = [][2]string{{"←→", "saved prompts"}, {"ctrl+o", "list"}, {"↵", "list"}, {"esc", "cancel"}}
	case d.focus == FieldPrompt:
		hint = [][2]string{{"ctrl+v", "paste an image"}, {"tab", "move"}, {"↑↓", "caret or move"}, {"↵", "create"}, {"esc", "cancel"}}
	case d.focus == FieldGroup:
		hint = [][2]string{{"←→", "pick group"}, {"tab/↑↓", "move"}, {"↵", "create"}, {"esc", "cancel"}}
	case d.focus == FieldDir && paths.Active():
		hint = paths.Hint()
	case d.focus == FieldModel && ch.sugg.open:
		hint = [][2]string{{"type", "filter"}, {"↑↓", "pick"}, {"tab", "fill in"}, {"↵", "create"}, {"esc", "close"}}
	case d.focus == FieldModel:
		hint = [][2]string{{"type", "filter"}, {"tab/↑↓", "move"}, {"↵", "create"}, {"esc", "cancel"}}
	case d.focus == FieldEffort && ch.effortTyped(h, toolName):
		hint = [][2]string{{"type", "level"}, {"tab/↑↓", "move"}, {"↵", "create"}, {"esc", "cancel"}}
	case d.focus == FieldEffort:
		hint = [][2]string{{"←→", "level"}, {"tab/↑↓", "move"}, {"↵", "create"}, {"esc", "cancel"}}
	}
	body := head.String() + tail.String()
	room := facts.Height - strings.Count(strings.TrimRight(body, "\n"), "\n") - 1 - facts.Chrome(hint)
	details, detailLines := d.savedDetails(facts, room)
	d.lines.Reset()
	d.lines.Append(headLines)
	d.lines.Append(detailLines)
	d.lines.Append(tailLines)
	return strings.TrimRight(head.String()+details+tail.String(), "\n"), hint
}

// savedRow is the saved prompts row's value, and whether it shows: the
// read, its failure, or the pick once there is something to pick.
func (d *Form) savedRow(facts FormFacts) (string, bool) {
	st := facts.Styles
	switch {
	case d.savedLoading:
		return "loading… (optional)", true
	case d.savedErr != "":
		return ansi.Wrap(d.savedErr, max(1, facts.Width), ""), true
	case len(d.saved) == 0:
		return "", false
	}
	name := "none"
	if d.savedIndex > 0 && d.savedIndex <= len(d.saved) {
		name = d.saved[d.savedIndex-1].Name
	}
	return st.Subtle.Render("◂ ") + st.Value.Render(name) + st.Subtle.Render(" ▸"), true
}

// savedDetails are the lines under the saved prompts row, within room: the
// open list, or the picked prompt's text on one line.
func (d *Form) savedDetails(facts FormFacts, room int) (string, presentation.Lines[fieldLine]) {
	var lines presentation.Lines[fieldLine]
	if room <= 0 {
		return "", lines
	}
	indent := strings.Repeat(" ", LabelColumn)
	if d.savedList.Open() {
		var b strings.Builder
		for i, line := range d.savedList.Lines(facts.Width, min(savedListRows, room), facts.Styles) {
			b.WriteString(indent + line + "\n")
			lines.Show(fieldLine{field: FieldSaved, entry: i}, 1)
		}
		return b.String(), lines
	}
	text := d.SavedText()
	if text == "" {
		return "", lines
	}
	label := "Instructions: "
	preview := SavedPreview(text, facts.Width-ansi.StringWidth(label))
	line := facts.Styles.Subtle.Render(ansi.Truncate(label+preview, max(1, facts.Width), "…"))
	lines.Show(fieldLine{field: FieldSaved, entry: -1}, 1)
	return indent + line + "\n", lines
}

// FormField paints one labelled row of a form card.
func FormField(st Styles, label, value string, focused bool) string {
	marker := "  "
	style := st.Label
	if focused {
		marker = lipgloss.NewStyle().Foreground(st.Accent).Render(presentation.GlyphsFor(st.GlyphMode).SelectionGlyph + " ")
		style = lipgloss.NewStyle().Foreground(st.Accent).Bold(true)
	}
	lines := strings.Split(value, "\n")
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%s%s %s\n", marker, style.Width(LabelColumn-3).Render(label), lines[0]))
	for _, line := range lines[1:] {
		b.WriteString(strings.Repeat(" ", LabelColumn) + line + "\n")
	}
	return b.String()
}
