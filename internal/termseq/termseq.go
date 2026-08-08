// Package termseq writes control sequences to whatever terminal is actually
// drawing this process, tunnelling them past a hosting tmux when there is one.
package termseq

import (
	"io"
	"os"
	"os/exec"
	"strings"
)

// Out is the stream the sequences go to; tests point it elsewhere.
var Out io.Writer = os.Stdout

// inTmux reports whether a tmux is hosting this pane.
var inTmux = func() bool { return os.Getenv("TMUX") != "" }

// Emit sends a control sequence to the outer terminal. Under tmux only the
// passthrough envelope goes out: tmux interprets sequences it recognises
// against the pane instead of the window, and gates several of them behind
// options a user may have turned off. EnablePassthrough must have opened the
// envelope first.
func Emit(seq string) error {
	if inTmux() {
		seq = Passthrough(seq)
	}
	_, err := io.WriteString(Out, seq)
	return err
}

// Passthrough wraps a sequence in DCS tmux;…ST, doubling every ESC as tmux's
// passthrough protocol requires.
func Passthrough(seq string) string {
	return "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
}

// EnablePassthrough asks the hosting tmux, when there is one, to let this
// pane's passthrough sequences reach the outer terminal. Off by default since
// tmux 3.3, and without it every Emit above dies at the multiplexer.
func EnablePassthrough() {
	if !inTmux() {
		return
	}
	_ = exec.Command("tmux", "set-option", "-p", "allow-passthrough", "on").Run()
}
