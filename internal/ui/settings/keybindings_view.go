package settings

import (
	"fmt"
	"strings"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
)

func (s *State) viewKeyPicker(ctx ViewContext) Frame {
	st := ctx.Styles
	tables := s.Tables
	if s.KeyReset {
		return Frame{
			Card:        Confirm,
			Title:       "↺ Reset keys",
			Question:    "Reset every key to its default?",
			Consequence: strings.Join(keyResetChanges(tables...), "\n"),
			Destructive: true,
			Answer:      "reset",
		}
	}
	rows := KeyRows(tables)
	first, last := pickerWindow(len(rows), s.KeyCursor, ctx.Height-14)
	var b strings.Builder
	if first > 0 {
		b.WriteString(st.Subtle.Render(fmt.Sprintf("  ↑ %d more", first)) + "\n")
	}
	for i := first; i < last; i++ {
		row := rows[i]
		keys := tables[row.Table]
		section := keySectionFor(keys)
		if i == first || rows[i-1].Table != row.Table {
			if i > first {
				b.WriteByte('\n')
			}
			b.WriteString(st.Annotation.Render("  "+section.title) + "\n")
		}
		marker := "  "
		labelStyle := st.Value
		if s.KeyCursor == i {
			marker = st.Marker.Render("❯ ")
			labelStyle = st.Focused
		}
		value := keys.Binding(row.Action.Name).Label()
		valueRender := st.Value.Render(value)
		if value == "" {
			valueRender = st.Subtle.Render(section.offLabel(row.Action.Name))
		}
		if s.KeyCapture && s.KeyCursor == i {
			word := "press a key"
			if s.KeyAppend {
				word = "press a key to add"
			}
			valueRender = st.Marker.Render(word + "…")
		}
		b.WriteString(marker)
		b.WriteString(presentation.PadRight(labelStyle.Render(row.Action.Name), 14))
		b.WriteString(presentation.PadRight(valueRender, 26))
		b.WriteString(st.Muted.Render(row.Action.Does))
		b.WriteByte('\n')
	}
	if last < len(rows) {
		b.WriteString(st.Subtle.Render(fmt.Sprintf("  ↓ %d more", len(rows)-last)) + "\n")
	}
	hint := [][2]string{{"↑↓", "move"}, {"↵", "set a key"}, {"a", "add one"}, {"d", "off"}, {"r", "defaults"}, {"esc", "back"}}
	if s.KeyCapture {
		hint = [][2]string{{"any key", "bind it"}, {"esc", "cancel"}}
	}
	return Frame{Card: Flex, Title: "⚙ Keybindings", Body: strings.TrimRight(b.String(), "\n"), Hints: hint}
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
