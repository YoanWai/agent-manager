package ui

import (
	"os"
	"strings"
	"sync"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/text/unicode/bidi"
)

// Written as escapes because the glyphs are invisible in an editor.
const (
	leftToRightMark       = "\u200e"
	firstStrongIsolate    = "\u2066"
	popDirectionalIsolate = "\u2069"
)

// bidiPin reports whether frame rows carry the direction marks. They are what
// keeps a pure-RTL row in its column on iTerm2, and they wreck the frame on a
// host that acts on them: one running its own bidi reorders the row until it
// no longer matches what the renderer believes it painted, and one that
// charges a cell for a format character overflows the row and scrolls the
// frame. So they ship off, for the host known to need them alone.
var bidiPin = sync.OnceValue(detectBidiPin)

// detectBidiPin reads the host from the environment, AGENT_MANAGER_RTL_PIN
// overriding it either way.
func detectBidiPin() bool {
	switch strings.ToLower(os.Getenv("AGENT_MANAGER_RTL_PIN")) {
	case "1", "on", "true":
		return true
	case "0", "off", "false":
		return false
	}
	return os.Getenv("TERM_PROGRAM") == "iTerm.app" ||
		strings.HasPrefix(os.Getenv("LC_TERMINAL"), "iTerm")
}

// pinFrameLTR prepends an LRM to any frame row whose first strong character is
// RTL (a Hebrew session name in the rail, say). UAX#9 takes paragraph direction
// from that first strong character, so without the mark a pinned host
// right-justifies the whole row and pulls it out of column. The mark is zero
// width and applied after layout, so geometry is untouched.
func pinFrameLTR(frame string) string {
	if !bidiPin() {
		return frame
	}
	lines := strings.Split(frame, "\n")
	changed := false
	for i, line := range lines {
		// An escape sequence ends in a Latin letter, which is strong LTR, so
		// classification reads the text the terminal lays out rather than the
		// styling wrapped around it. Rows without RTL skip the strip.
		if !containsRTL(line) || !firstStrongIsRTL(ansi.Strip(line)) {
			continue
		}
		lines[i] = leftToRightMark + line
		changed = true
	}
	if !changed {
		return frame
	}
	return strings.Join(lines, "\n")
}

func firstStrongIsRTL(text string) bool {
	for _, r := range text {
		props, _ := bidi.LookupRune(r)
		switch props.Class() {
		case bidi.L:
			return false
		case bidi.R, bidi.AL:
			return true
		}
	}
	return false
}

// pinPaneLineLTR isolates a captured pane row that carries RTL text. On rows
// where the rail is only neutrals, a leading Hebrew run is the line's first
// strong character; a pinned host then right-justifies the row and the pane
// text lands on the rail. The LRM is strong LTR, and the isolate around the
// already padded row keeps the run inside the content column.
func pinPaneLineLTR(line string) string {
	if !bidiPin() || !containsRTL(line) {
		return line
	}
	return leftToRightMark + firstStrongIsolate + line + popDirectionalIsolate
}

func containsRTL(line string) bool {
	for _, r := range line {
		if r < 0x0590 {
			continue
		}
		props, _ := bidi.LookupRune(r)
		switch props.Class() {
		case bidi.R, bidi.AL:
			return true
		}
	}
	return false
}
