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
