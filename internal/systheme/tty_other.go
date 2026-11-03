//go:build !darwin && !linux

package systheme

func queryTerminalBg() (r, g, b int, ok bool) {
	return 0, 0, 0, false
}

// QueryPalette has no terminal to ask here, so nothing is declared.
func QueryPalette() []string {
	return nil
}
