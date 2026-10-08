package ui

import (
	"fmt"
	"github.com/charmbracelet/lipgloss"
	"strings"
)

func (m *Model) viewKeyPicker() string {
	tables := m.settings.tables
	if m.settings.keyReset {
		return m.confirmCard("↺ Reset keys", "Reset every key to its default?",
			strings.Join(keyResetChanges(tables...), "\n"), true, "reset")
	}
	rows := keyRowsOf(tables)
	first, last := pickerWindow(len(rows), m.settings.keyCursor, m.height-14)
	var b strings.Builder
	if first > 0 {
		b.WriteString(subtleStyle.Render(fmt.Sprintf("  ↑ %d more", first)) + "\n")
	}
	for i := first; i < last; i++ {
		row := rows[i]
		keys := tables[row.table]
		section := keySectionFor(keys)
		if i == first || rows[i-1].table != row.table {
			if i > first {
				b.WriteByte('\n')
			}
			b.WriteString(annotationStyle.Render("  "+section.title) + "\n")
		}
		marker := "  "
		labelStyle := valueStyle
		if m.settings.keyCursor == i {
			marker = lipgloss.NewStyle().Foreground(colorAccent).Render("❯ ")
			labelStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
		}
		value := keys.Binding(row.action.Name).Label()
		valueRender := valueStyle.Render(value)
		if value == "" {
			valueRender = subtleStyle.Render(section.offLabel(row.action.Name))
		}
		if m.settings.keyCapture && m.settings.keyCursor == i {
			word := "press a key"
			if m.settings.keyAppend {
				word = "press a key to add"
			}
			valueRender = lipgloss.NewStyle().Foreground(colorAccent).Render(word + "…")
		}
		b.WriteString(marker)
		b.WriteString(padRight(labelStyle.Render(row.action.Name), 14))
		b.WriteString(padRight(valueRender, 26))
		b.WriteString(mutedStyle.Render(row.action.Does))
		b.WriteByte('\n')
	}
	if last < len(rows) {
		b.WriteString(subtleStyle.Render(fmt.Sprintf("  ↓ %d more", len(rows)-last)) + "\n")
	}
	hint := [][2]string{{"↑↓", "move"}, {"↵", "set a key"}, {"a", "add one"}, {"d", "off"}, {"r", "defaults"}, {"esc", "back"}}
	if m.settings.keyCapture {
		hint = [][2]string{{"any key", "bind it"}, {"esc", "cancel"}}
	}
	return m.cardFlex("⚙ Keybindings", strings.TrimRight(b.String(), "\n"), hint)
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
