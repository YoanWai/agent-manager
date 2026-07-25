package ui

import "testing"

// tmux forwards a passthrough payload after undoubling ESC bytes; a
// sequence wrapped without the doubling reaches the terminal truncated at
// its own first ESC.
func TestTmuxPassthroughDoublesEscapes(t *testing.T) {
	got := tmuxPassthrough("\x1b]11;#0f1115\x07")
	want := "\x1bPtmux;\x1b\x1b]11;#0f1115\x07\x1b\\"
	if got != want {
		t.Fatalf("wrapped = %q, want %q", got, want)
	}
}
