package termseq

import (
	"strings"
	"testing"
)

func swap(tmux bool) (*strings.Builder, func()) {
	origOut, origTmux := Out, inTmux
	var sink strings.Builder
	Out, inTmux = &sink, func() bool { return tmux }
	return &sink, func() { Out, inTmux = origOut, origTmux }
}

// tmux forwards a passthrough payload after undoubling ESC bytes; a
// sequence wrapped without the doubling reaches the terminal truncated at
// its own first ESC.
func TestPassthroughDoublesEscapes(t *testing.T) {
	got := Passthrough("\x1b]11;#0f1115\x07")
	want := "\x1bPtmux;\x1b\x1b]11;#0f1115\x07\x1b\\"
	if got != want {
		t.Fatalf("wrapped = %q, want %q", got, want)
	}
}

func TestEmitWrapsOnlyUnderTmux(t *testing.T) {
	sink, restore := swap(false)
	defer restore()
	if err := Emit("\x1b]111\x07"); err != nil {
		t.Fatal(err)
	}
	if got := sink.String(); got != "\x1b]111\x07" {
		t.Fatalf("bare emit = %q", got)
	}

	sink, restoreTmux := swap(true)
	defer restoreTmux()
	if err := Emit("\x1b]111\x07"); err != nil {
		t.Fatal(err)
	}
	if got := sink.String(); got != "\x1bPtmux;\x1b\x1b]111\x07\x1b\\" {
		t.Fatalf("tmux emit = %q", got)
	}
}
