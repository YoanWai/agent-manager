// Package badge renders the README's stat card as SVG the repo owns, so its
// type, palette and spacing match the TUI instead of a badge host's house
// style.
package badge

import "strings"

// Palette is one rendering of the card: the hairlines between its columns and
// the tone its labels are set in. Light and dark variants pair with the
// reader's GitHub theme through a <picture> element.
type Palette struct {
	Border string
	Label  string
}

var (
	Light = Palette{Border: "#d5dbe2", Label: "#59636e"}
	Dark  = Palette{Border: "#2d333d", Label: "#98a0ac"}
)

// escape makes a value safe to drop into the document.
func escape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}
