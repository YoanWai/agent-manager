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

// NavigationLabel formats fixed manager text before it is combined with user content.
func NavigationLabel(mode GlyphMode, label string) string {
	if mode.Effective() != GlyphModeBasic {
		return label
	}
	return navigationLabels.Replace(label)
}

var navigationLabels = strings.NewReplacer("↑↓", "up/down", "←→", "left/right", "↑", "up", "↓", "down", "←", "left", "→", "right", "↕", "up/down")

// NavigationSet includes variable-width directional fragments, separate from row marks.
type NavigationSet struct{ UpGlyph, DownGlyph, UpDownGlyph, ToGlyph string }

func NavigationFor(mode GlyphMode) NavigationSet {
	if mode.Effective() == GlyphModeBasic {
		return NavigationSet{UpGlyph: "^", DownGlyph: "v", UpDownGlyph: "^v", ToGlyph: "->"}
	}
	return NavigationSet{UpGlyph: "↑", DownGlyph: "↓", UpDownGlyph: "↕", ToGlyph: "→"}
}
