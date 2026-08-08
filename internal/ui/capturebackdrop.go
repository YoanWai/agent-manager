package ui

import (
	"regexp"
	"strings"
)

// Agent CLIs pick their colors for a dark terminal, so a light theme keeps
// the manager chrome light and renders captured panes on a pinned dark
// backdrop instead: the classic theme's Bg, the tone a dark-theme terminal
// itself would show behind the same capture. The helpers here keep that
// backdrop alive across the capture's own SGR stream, where any reset would
// otherwise let the light theme bleed through mid-line.

// captureOnDark, captureOpen and the component sequences are rebuilt by
// applyTheme, alongside every other package-level style.
var (
	captureOnDark bool
	captureOpen   string
	captureFgSeq  string
	captureBgSeq  string
)

func rebuildCaptureBackdrop(t Theme) {
	captureOnDark = t.lightBackdrop()
	captureFgSeq = fgSeq(themes[0].Text)
	captureBgSeq = bgSeq(themes[0].Bg)
	captureOpen = captureFgSeq + captureBgSeq
}

// lightBackdrop reports whether the theme's backdrop is a light color, by
// relative luminance of Bg.
func (t Theme) lightBackdrop() bool {
	r, g, b := hexRGB(t.Bg)
	return 0.2126*float64(r)+0.7152*float64(g)+0.0722*float64(b) > 128
}

var sgrPattern = regexp.MustCompile(`\x1b\[[0-9;:]*m`)

// reassertCaptureColors re-opens the capture backdrop after every SGR in
// the line that leaves the foreground or background at the terminal
// default. Only the component actually left at default is re-opened, so an
// explicit color set in the same sequence, or still open from an earlier
// one, passes through untouched: `0;31` keeps its red foreground and gets
// only the backdrop's background.
func reassertCaptureColors(line string) string {
	if !strings.Contains(line, "\x1b[") {
		return line
	}
	return sgrPattern.ReplaceAllStringFunc(line, func(seq string) string {
		fg, bg := sgrDropsColors(seq[2 : len(seq)-1])
		if fg && bg {
			return seq + captureOpen
		}
		if fg {
			return seq + captureFgSeq
		}
		if bg {
			return seq + captureBgSeq
		}
		return seq
	})
}

// sgrDropsColors reports, per component, whether an SGR parameter string
// leaves the foreground or background at the terminal default: a reset
// (0 or empty) or an explicit default (39/49) not followed by a color for
// that component later in the same sequence. Extended color introducers
// (38/48/58) carry their arguments inline, so a 0-valued color component
// is skipped rather than misread as a reset; colon-form arguments sit
// inside one semicolon part and need no skipping.
func sgrDropsColors(params string) (fgDefault, bgDefault bool) {
	if params == "" {
		return true, true
	}
	parts := strings.Split(params, ";")
	for i := 0; i < len(parts); i++ {
		head, _, _ := strings.Cut(parts[i], ":")
		switch head {
		case "", "0":
			fgDefault, bgDefault = true, true
		case "39":
			fgDefault = true
		case "49":
			bgDefault = true
		case "30", "31", "32", "33", "34", "35", "36", "37",
			"90", "91", "92", "93", "94", "95", "96", "97":
			fgDefault = false
		case "40", "41", "42", "43", "44", "45", "46", "47",
			"100", "101", "102", "103", "104", "105", "106", "107":
			bgDefault = false
		case "38", "48", "58":
			switch head {
			case "38":
				fgDefault = false
			case "48":
				bgDefault = false
			}
			if strings.Contains(parts[i], ":") {
				continue
			}
			if i+1 < len(parts) {
				switch parts[i+1] {
				case "5":
					i += 2
				case "2":
					i += 4
				}
			}
		}
	}
	return fgDefault, bgDefault
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
