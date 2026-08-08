//go:build !darwin && !linux

package systheme

func queryTerminalBg() (r, g, b int, ok bool) {
	return 0, 0, 0, false
}
