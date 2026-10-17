package settings

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// view paints the panel: the list around the cursor, the editor, or the
// delete question, each fitted to the terminal.
func (p *Prompts) view(ctx ViewContext) Frame {
	st := ctx.Styles
	inner := max(1, ctx.InnerWidth)
	var body []string
	add := func(text string) {
		for _, line := range strings.Split(text, "\n") {
			body = append(body, ansi.Truncate(line, inner, "…"))
		}
	}
	hint := [][2]string{{"↑↓", "select"}, {"n", "new"}, {"↵", "edit"}, {"d", "delete"}, {"esc", "back"}}
	switch {
	case p.editing:
		p.name.Width = max(1, inner-3)
		p.text.SetWidth(inner)
		p.text.SetHeight(max(1, min(6, ctx.Height-14)))
		add("name")
		add(ctx.TextInput(p.name))
		add("text")
		add(ctx.TextArea(p.text))
		hint = [][2]string{{"tab", "field"}, {"ctrl+s", "save"}, {"esc", "discard"}}
		if p.busy {
			hint = [][2]string{{"wait", "save in progress"}}
		}
	case p.confirm:
		add("Delete " + p.rows[p.cursor].Name + "?")
		hint = [][2]string{{"↵", "delete"}, {"esc", "cancel"}}
		if p.busy {
			hint = [][2]string{{"wait", "delete in progress"}}
		}
	default:
		switch {
		case p.loading:
			add("Loading saved prompts…")
		case p.refreshFailed:
			add("Could not load them. Press r.")
		case len(p.rows) == 0:
			add("No saved prompts.")
		}
		first, last := promptsWindow(len(p.rows), p.cursor, max(1, ctx.Height-12))
		if first > 0 {
			add(fmt.Sprintf("↑ %d more", first))
		}
		for i := first; i < last; i++ {
			if i == p.cursor {
				add(st.Value.Render("› " + p.rows[i].Name))
			} else {
				add(st.Subtle.Render("  " + p.rows[i].Name))
			}
		}
		if last < len(p.rows) {
			add(fmt.Sprintf("↓ %d more", len(p.rows)-last))
		}
		add("agents add these with agent-manager prompts add")
		if p.refreshFailed && !p.loading {
			hint = [][2]string{{"n", "new"}, {"r", "refresh"}, {"esc", "back"}}
		}
	}
	return Frame{Card: Fixed, Title: "Saved prompts", Body: strings.Join(body, "\n"), Hints: hint}
}

// promptsWindow is the rows that fit room, keeping the cursor centred.
func promptsWindow(count, cursor, room int) (int, int) {
	if count == 0 {
		return 0, 0
	}
	visible := min(count, max(1, room))
	first := max(0, min(cursor-visible/2, count-visible))
	return first, first + visible
}
