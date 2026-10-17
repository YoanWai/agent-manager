package spawn

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// hit is a body line's field, and its list entry or -1.
type hit struct {
	field Field
	entry int
}

// LabelColumn is the columns before a field value: marker (2), label (9),
// separator space (1).
const LabelColumn = 12

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
}

// View paints the form's body and its key hint, and records the line each
// field takes for clicks.
func (d *Form) View(h FormHost, facts FormFacts) (string, [][2]string) {
	st := facts.Styles
	var b strings.Builder
	d.hits = d.hits[:0]
	add := func(text string, at hit) {
		b.WriteString(text)
		for range strings.Count(text, "\n") {
			d.hits = append(d.hits, at)
		}
	}
	field := func(label, value string, id Field) {
		add(FormField(st, label, value, d.focus == id), hit{field: id, entry: -1})
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
				add(line+"\n", hit{field: FieldModel, entry: entries[i]})
			}
		}
	}
	if value, shown, _ := ch.EffortRow(h, toolName, st); shown {
		field("effort", value, FieldEffort)
	}
	field("dir", st.Input(d.dir), FieldDir)
	paths := h.Paths()
	if d.focus == FieldDir && paths.Active() {
		add(paths.View()+"\n", hit{field: FieldDir, entry: -1})
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
	// Chips are tokens inside the typed text, so they wrap and reflow with
	// the words around them; painting happens on the rendered prompt.
	field("prompt", h.Prompt().View(), FieldPrompt)
	group := lipgloss.NewStyle().Foreground(st.Accent2).Render(facts.Group)
	if facts.Host != "" {
		group = facts.Host + st.Subtle.Render(" · ") + group
	}
	field("group", group, FieldGroup)

	if d.focus == FieldGroup {
		add("\n", hit{field: FieldGroup, entry: -1})
		for i, line := range facts.GroupPicker {
			add(line+"\n", hit{field: FieldGroup, entry: i})
		}
	}

	hint := [][2]string{{"tab/↑↓", "move"}, {"←→", "change"}, {"↵", "create"}, {"esc", "cancel"}}
	switch {
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
	return strings.TrimRight(b.String(), "\n"), hint
}

// FormField paints one labelled row of a form card.
func FormField(st Styles, label, value string, focused bool) string {
	marker := "  "
	style := st.Label
	if focused {
		marker = lipgloss.NewStyle().Foreground(st.Accent).Render("❯ ")
		style = lipgloss.NewStyle().Foreground(st.Accent).Bold(true)
	}
	lines := strings.Split(value, "\n")
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%s%s %s\n", marker, style.Width(9).Render(label), lines[0]))
	for _, line := range lines[1:] {
		b.WriteString(strings.Repeat(" ", LabelColumn) + line + "\n")
	}
	return b.String()
}
