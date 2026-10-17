package settings

import (
	"fmt"
	"strings"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/x/ansi"
)

// view paints the panel: the list around the cursor, the editor, or the
// delete question, each fitted to the terminal.
func (p *Prompts) view(ctx ViewContext) Frame {
	st := ctx.Styles
	inner := max(1, ctx.InnerWidth)
	var body []string
	p.lines = p.lines[:0]
	add := func(text string, holds int) {
		for _, line := range strings.Split(text, "\n") {
			body = append(body, ansi.Truncate(line, inner, "…"))
			p.lines = append(p.lines, holds)
		}
	}
	hint := [][2]string{{"↑↓", "select"}, {"n", "new"}, {"↵", "edit"}, {"d", "delete"}, {"esc", "back"}}
	switch {
	case p.editing:
		p.name.Width = max(1, inner-3)
		p.text.SetWidth(inner)
		p.text.SetHeight(max(1, min(6, ctx.Height-14)))
		add("name", promptNameField)
		add(ctx.TextInput(p.name), promptNameField)
		add("text", promptTextField)
		add(ctx.TextArea(p.text), promptTextField)
		hint = [][2]string{{"tab", "field"}, {"ctrl+s", "save"}, {"esc", "discard"}}
		if p.busy {
			hint = [][2]string{{"wait", "save in progress"}}
		}
	case p.confirm:
		add("Delete "+p.rows[p.cursor].Name+"?", -1)
		hint = [][2]string{{"↵", "delete"}, {"esc", "cancel"}}
		if p.busy {
			hint = [][2]string{{"wait", "delete in progress"}}
		}
	default:
		switch {
		case p.loading:
			add("Loading saved prompts…", -1)
		case p.refreshFailed:
			add("Could not load them. Press r.", -1)
		case len(p.rows) == 0:
			add("No saved prompts.", -1)
		}
		first, last := promptsWindow(p.first, len(p.rows), p.cursor, max(1, ctx.Height-12))
		p.first = first
		if first > 0 {
			add(fmt.Sprintf("↑ %d more", first), first-1)
		}
		for i := first; i < last; i++ {
			if i == p.cursor {
				add(st.Value.Render("› "+p.rows[i].Name), i)
			} else {
				add(st.Subtle.Render("  "+p.rows[i].Name), i)
			}
		}
		if last < len(p.rows) {
			add(fmt.Sprintf("↓ %d more", len(p.rows)-last), last)
		}
		add("agents add these with agent-manager prompts add", -1)
		if p.refreshFailed && !p.loading {
			hint = [][2]string{{"n", "new"}, {"r", "refresh"}, {"esc", "back"}}
		}
	}
	return Frame{Card: Fixed, Title: "Saved prompts", Body: strings.Join(body, "\n"), Hints: hint}
}

// promptsWindow is the rows that fit room, held still while the cursor
// stays inside them.
func promptsWindow(first, count, cursor, room int) (int, int) {
	if count == 0 {
		return 0, 0
	}
	visible := min(count, max(1, room))
	first = presentation.Follow(first, cursor, count, visible)
	return first, first + visible
}
