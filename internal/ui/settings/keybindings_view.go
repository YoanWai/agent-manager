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
			Title:       presentation.GlyphsFor(ctx.GlyphMode).ResetGlyph + " Reset keys",
			Question:    "Reset every key to its default?",
			Consequence: strings.Join(keyResetChanges(tables...), "\n"),
			Destructive: true,
			Answer:      "reset",
		}
	}
	hint := [][2]string{{"↑↓", "move"}, {"↵", "set a key"}, {"a", "add one"}, {"d", "off"}, {"r", "defaults"}, {"esc", "back"}}
	if s.KeyCapture {
		hint = [][2]string{{"any key", "bind it"}, {"esc", "cancel"}}
	}
	rows := KeyRows(tables)
	first, last := s.keyWindow(rows, ctx.Height-ctx.Chrome(hint))
	s.keyFirst = first
	var b strings.Builder
	s.lines.Reset()
	if first > 0 {
		b.WriteString(st.Subtle.Render(fmt.Sprintf("  ↑ %d more", first)) + "\n")
		s.lines.Show(first-1, 1)
	}
	for i := first; i < last; i++ {
		row := rows[i]
		keys := tables[row.Table]
		section := keySectionFor(keys)
		if i == first || rows[i-1].Table != row.Table {
			if i > first {
				b.WriteByte('\n')
				s.lines.Skip(1)
			}
			b.WriteString(st.Annotation.Render("  "+section.title) + "\n")
			s.lines.Skip(1)
		}
		marker := "  "
		labelStyle := st.Value
		if s.KeyCursor == i {
			marker = st.Marker.Render(presentation.GlyphsFor(ctx.GlyphMode).SelectionGlyph + " ")
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
		s.lines.Show(i, 1)
	}
	if last < len(rows) {
		b.WriteString(st.Subtle.Render(fmt.Sprintf("  ↓ %d more", len(rows)-last)) + "\n")
		s.lines.Show(last, 1)
	}
	return Frame{Card: Flex, Title: presentation.GlyphsFor(ctx.GlyphMode).SettingsGlyph + " Keybindings", Body: strings.TrimRight(b.String(), "\n"), Hints: hint}
}

// keyWindow is the rows [first, last) whose lines, headings and "more"
// counters included, fit room, held still while the cursor stays inside.
// The cursor's row always shows.
func (s *State) keyWindow(rows []KeyRow, room int) (first, last int) {
	for visible := min(len(rows), max(room, 1)); ; visible-- {
		first = presentation.Follow(s.keyFirst, s.KeyCursor, len(rows), visible)
		last = first + visible
		if visible <= 1 || keyLineCount(rows, first, last) <= room {
			return first, last
		}
	}
}

// keyLineCount is the body lines rows [first, last) paint: each row, a
// heading where its table starts with a blank line above all but the
// first, and a counter for the rows above and below the window.
func keyLineCount(rows []KeyRow, first, last int) int {
	lines := last - first
	for i := first; i < last; i++ {
		if i == first || rows[i-1].Table != rows[i].Table {
			lines++
			if i > first {
				lines++
			}
		}
	}
	if first > 0 {
		lines++
	}
	if last < len(rows) {
		lines++
	}
	return lines
}
