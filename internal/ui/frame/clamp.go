package frame

import (
	"strings"
)

// Clamp pins a rendered frame to exactly height rows so the outer terminal
// cannot scroll the TUI away when a layout overshoots. pad paints the rows it
// adds, so a short frame cannot show the terminal's own background through
// its bottom rows.
func Clamp(frame string, height int, pad func() string) string {
	if height <= 0 {
		return frame
	}
	lines := strings.Split(frame, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, pad())
	}
	return strings.Join(lines, "\n")
}
