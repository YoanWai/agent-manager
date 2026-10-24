package help

import "github.com/YoanWai/agent-manager/internal/keybind"

// Tables are the copied key tables the key map lists, and whether the
// beta ←→ pair is on.
type Tables struct {
	Session   keybind.Table
	List      keybind.Table
	ArrowStep bool
}

// Reads is what building the key map's context asks the root: the copied
// tables, the glyphs and styles it paints with, the fixed quick-prompt keys
// it names and Review's own rows.
type Reads interface {
	KeyTables() Tables
	Glyphs() Glyphs
	QuickKeys() QuickKeys
	ReviewRows() [][2]string
	CursorMarker() string
	Styles() Styles
}

// ContextFor is the presentation state for a body width columns wide.
func ContextFor(reads Reads, width int) Context {
	tables := reads.KeyTables()
	return Context{
		SessionKeys:  tables.Session,
		ListKeys:     tables.List,
		ArrowStep:    tables.ArrowStep,
		Width:        width,
		Glyphs:       reads.Glyphs(),
		QuickKeys:    reads.QuickKeys(),
		Styles:       reads.Styles(),
		CursorMarker: reads.CursorMarker(),
		ReviewRows:   reads.ReviewRows(),
	}
}
