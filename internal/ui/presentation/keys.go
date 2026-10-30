package presentation

import "strings"

// KeyLabel formats manager-owned key hints, separately from user content
// and the one-cell marks used by rows and loaders.
func KeyLabel(mode GlyphMode, label string) string {
	if mode.Effective() != GlyphModeBasic {
		return label
	}
	return strings.ReplaceAll(strings.ReplaceAll(label, "↵", "enter"), "⌫", "backspace")
}
