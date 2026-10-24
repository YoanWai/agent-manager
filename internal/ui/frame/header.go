package frame

import "strings"

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
	// Left is the wordmark and its update tag.
	Left string
	// Scope names what the list is showing, and Agents is the fleet's cost.
	Scope  string
	Agents string
	// Rollup is the status strip, richest first: full, compact, and absent.
	Rollup []string
	// Sep joins the readings that fit.
	Sep    string
	Width  int
	Gutter int
}

// Row is the band as one painted line. The readings run richest first, so
// the band drops the fleet cost, then the rollup, then the scope, and keeps
// the scope alone before it gives up the room.
func (h Header) Row() string {
	readings := make([]string, 0, len(h.Rollup)+2)
	for _, rollup := range h.Rollup {
		readings = append(readings, JoinHeaderPieces(h.Sep, h.Scope, rollup, h.Agents))
	}
	if len(h.Rollup) > 0 {
		readings = append(readings, JoinHeaderPieces(h.Sep, h.Scope, h.Rollup[len(h.Rollup)-1]))
	}
	return HeaderRow(h.Left, append(readings, h.Scope, ""), h.Width, h.Gutter)
}
