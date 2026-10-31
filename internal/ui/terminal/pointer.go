package terminal

import "github.com/YoanWai/agent-manager/internal/termseq"

// The hand goes out under its CSS name, then its X11 one: Ghostty and kitty
// skip a name they do not know, iTerm2 resets on one, so X11 comes last.
const (
	handPointer  = "\x1b]22;pointer\x1b\\\x1b]22;hand2\x1b\\"
	resetPointer = "\x1b]22;\x1b\\"
)

// handShown is the pointer the one terminal was last told to show. Updates
// and handoffs both run on Bubble Tea's event loop.
var handShown bool

// ShowHand turns the terminal's pointer into a hand, or gives it back its
// own, when that changes. Terminals without OSC 22 ignore both.
func ShowHand(hand bool) {
	if hand == handShown {
		return
	}
	handShown = hand
	seq := resetPointer
	if hand {
		seq = handPointer
	}
	_ = termseq.Emit(seq)
}
