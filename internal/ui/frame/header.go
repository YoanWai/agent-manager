package frame

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// JoinHeaderPieces joins the header's non-empty readings with a separator.
func JoinHeaderPieces(sep string, pieces ...string) string {
	kept := pieces[:0:0]
	for _, piece := range pieces {
		if piece != "" {
			kept = append(kept, piece)
		}
	}
	return strings.Join(kept, sep)
}

// Header is the full-width band over both columns: the wordmark on the left,
// and the richest reading of the fleet that fits set against the right edge
// (scope and rollup, then a compact rollup, then the scope alone).
type Header struct {
	Left string
	// Version follows Left, before Tag, and is the first piece a narrow
	// row drops.
	Version string
	Tag     string
	Scope   string
	Agents  string
	// Rollup is the status strip, richest first: full, compact, and absent.
	Rollup []string
	Sep    string
	Width  int
	Gutter int
}

// Row is the band as one painted line. The readings run richest first, so
// the band drops the version, then the fleet cost, then the rollup, then
// the scope, and keeps the scope alone before it gives up the room.
func (h Header) Row() string {
	readings := make([]string, 0, len(h.Rollup)+2)
	for _, rollup := range h.Rollup {
		readings = append(readings, JoinHeaderPieces(h.Sep, h.Scope, rollup, h.Agents))
	}
	if len(h.Rollup) > 0 {
		readings = append(readings, JoinHeaderPieces(h.Sep, h.Scope, h.Rollup[len(h.Rollup)-1]))
	}
	readings = append(readings, h.Scope, "")
	if wide := h.Left + h.Version + h.Tag; h.Width-h.Gutter-ansi.StringWidth(wide)-ansi.StringWidth(readings[0]) >= 2 {
		return HeaderRow(wide, readings[:1], h.Width, h.Gutter)
	}
	return HeaderRow(h.Left+h.Tag, readings, h.Width, h.Gutter)
}
