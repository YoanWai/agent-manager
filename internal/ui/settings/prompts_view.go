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
	p.lines.Reset()
	add := func(text string) int {
		lines := strings.Split(text, "\n")
		for _, line := range lines {
			body = append(body, ansi.Truncate(line, inner, "…"))
		}
		return len(lines)
	}
	show := func(text string, holds int) { p.lines.Show(holds, add(text)) }
	note := func(text string) { p.lines.Skip(add(text)) }
	hint := [][2]string{{"↑↓", "select"}, {"n", "new"}, {"↵", "edit"}, {"d", "delete"}, {"esc", "back"}}
	switch {
	case p.editing:
		p.name.Width = max(1, inner-3)
		p.text.SetWidth(inner)
		p.text.SetHeight(max(1, min(6, ctx.Height-14)))
		show("name", promptNameField)
		show(ctx.TextInput(p.name), promptNameField)
		show("text", promptTextField)
		show(ctx.TextArea(p.text), promptTextField)
		hint = [][2]string{{"tab", "field"}, {"ctrl+s", "save"}, {"esc", "discard"}}
		if p.busy {
			hint = [][2]string{{"wait", "save in progress"}}
		}
	case p.confirm:
		note("Delete " + p.rows[p.cursor].Name + "?")
		hint = [][2]string{{"↵", "delete"}, {"esc", "cancel"}}
		if p.busy {
			hint = [][2]string{{"wait", "delete in progress"}}
		}
	default:
		switch {
		case p.loading:
			note("Loading saved prompts…")
		case p.refreshFailed:
			note("Could not load them. Press r.")
		case len(p.rows) == 0:
			note("No saved prompts.")
		}
		first, last := promptsWindow(p.first, len(p.rows), p.cursor, max(1, ctx.Height-12))
		p.first = first
		if first > 0 {
			show(fmt.Sprintf("↑ %d more", first), first-1)
		}
		for i := first; i < last; i++ {
			if i == p.cursor {
				show(st.Value.Render("› "+p.rows[i].Name), i)
			} else {
				show(st.Subtle.Render("  "+p.rows[i].Name), i)
			}
		}
		if last < len(p.rows) {
			show(fmt.Sprintf("↓ %d more", len(p.rows)-last), last)
		}
		note("agents add these with agent-manager prompts add")
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
