package frame

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/keybind"
	tea "github.com/charmbracelet/bubbletea"
)

// LegendKey is the key a click on a pair presses: the first key its label
// names. A pair that moves a cursor or a field stays with the keyboard, since
// the rows and the wheel answer for it, and a pair naming a gesture presses
// nothing.
func LegendKey(label string) (tea.KeyMsg, bool) {
	if strings.ContainsAny(label, "↑↓←→") || strings.Contains(label, "pgup") {
		return tea.KeyMsg{}, false
	}
	if key, ok := pressKey(strings.TrimSpace(label)); ok {
		return key, true
	}
	return pressKey(firstKey(label))
}

// firstKey is the first of the keys a label joins with "/". A "/" that opens
// the label is the search key itself, not a separator.
func firstKey(label string) string {
	if len(label) > 1 {
		if i := strings.Index(label[1:], "/"); i >= 0 {
			label = label[:i+1]
		}
	}
	return strings.TrimSpace(label)
}

// pressKey is the message a key named the way a legend draws it arrives as
// when typed.
func pressKey(name string) (tea.KeyMsg, bool) {
	if key, drawn := legendGlyphKeys[name]; drawn {
		name = key
	}
	alt := false
	if rest, ok := strings.CutPrefix(name, "alt+"); ok && rest != "" {
		alt, name = true, rest
	}
	if keyType, ok := namedKeyTypes[name]; ok {
		key := tea.KeyMsg{Type: keyType, Alt: alt}
		if keyType == tea.KeySpace {
			key.Runes = []rune{' '}
		}
		return key, true
	}
	if runes := []rune(name); len(runes) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: runes, Alt: alt}, true
	}
	return tea.KeyMsg{}, false
}

// legendGlyphKeys reads back the glyphs a legend draws for named keys.
var legendGlyphKeys = map[string]string{"↵": "enter", "⌫": "backspace"}

// namedKeyTypes is every key Bubble Tea names, under the spelling a key
// table stores.
var namedKeyTypes = func() map[string]tea.KeyType {
	types := map[string]tea.KeyType{}
	for keyType := tea.KeyF20; keyType <= tea.KeyBackspace; keyType++ {
		if name := keyType.String(); name != "" && keyType != tea.KeyRunes {
			types[keybind.Normalize(name)] = keyType
		}
	}
	return types
}()
