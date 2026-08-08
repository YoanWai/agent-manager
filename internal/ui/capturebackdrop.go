package ui

import (
	"regexp"
	"strings"
)

// Agent CLIs pick their colors for a dark terminal, so a light theme keeps
// the manager chrome light and renders captured panes on a pinned dark
// backdrop instead: the classic theme's surface. The helpers here keep that
// backdrop alive across the capture's own SGR stream, where any reset would
// otherwise let the light theme bleed through mid-line.

// captureOnDark, captureOpen and captureBgSeq are rebuilt by applyTheme,
// alongside every other package-level style.
var (
	captureOnDark bool
	captureOpen   string
	captureBgSeq  string
)

func rebuildCaptureBackdrop(t Theme) {
	captureOnDark = t.lightBackdrop()
	captureBgSeq = bgSeq(themes[0].Bg)
	captureOpen = fgSeq(themes[0].Text) + captureBgSeq
}

// lightBackdrop reports whether the theme's backdrop is a light color, by
// relative luminance of Bg.
func (t Theme) lightBackdrop() bool {
	r, g, b := hexRGB(t.Bg)
	return 0.2126*float64(r)+0.7152*float64(g)+0.0722*float64(b) > 128
}

var sgrPattern = regexp.MustCompile(`\x1b\[[0-9;:]*m`)

// reassertCaptureColors re-opens the capture backdrop after every SGR in
// the line that returns the foreground or background to the terminal
// default. The capture keeps every explicit color the agent set; only the
// default cells move onto the backdrop.
func reassertCaptureColors(line string) string {
	if !strings.Contains(line, "\x1b[") {
		return line
	}
	return sgrPattern.ReplaceAllStringFunc(line, func(seq string) string {
		if sgrDropsColors(seq[2 : len(seq)-1]) {
			return seq + captureOpen
		}
		return seq
	})
}

// sgrDropsColors reports whether an SGR parameter string resets all
// attributes (0 or empty) or returns the foreground (39) or background
// (49) to the default. Extended color introducers (38/48/58) carry their
// arguments inline, so a 0-valued color component is skipped rather than
// misread as a reset; colon-form arguments sit inside one semicolon part
// and need no skipping.
func sgrDropsColors(params string) bool {
	if params == "" {
		return true
	}
	parts := strings.Split(params, ";")
	for i := 0; i < len(parts); i++ {
		head, _, _ := strings.Cut(parts[i], ":")
		switch head {
		case "", "0", "39", "49":
			return true
		case "38", "48", "58":
			if strings.Contains(parts[i], ":") {
				continue
			}
			if i+1 >= len(parts) {
				return false
			}
			switch parts[i+1] {
			case "5":
				i += 2
			case "2":
				i += 4
			}
		}
	}
	return false
}

// captureBlankRow is a full-width backdrop row for the panel rows below
// the capture, so the dark island covers the whole preview panel rather
// than ending in a ragged edge at the agent's last line.
func captureBlankRow(width int) string {
	if width <= 0 {
		return ""
	}
	return captureBgSeq + strings.Repeat(" ", width) + "\x1b[0m"
}
