// Package termseq writes control sequences to whatever terminal is actually
// drawing this process, tunnelling them past a hosting tmux when there is one,
// and tells whether that terminal is on this machine at all.
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

var (
	getenv = os.Getenv
	// tmuxSSHConnection reads SSH_CONNECTION from the session hosting this
	// pane, as "SSH_CONNECTION=…" or "-SSH_CONNECTION" once removed.
	tmuxSSHConnection = func() (string, error) {
		socket, _, _ := strings.Cut(os.Getenv("TMUX"), ",")
		out, err := exec.Command("tmux", "-S", socket, "show-environment", "SSH_CONNECTION").Output()
		return string(out), err
	}
)

// Remote reports whether the terminal drawing this process sits on another
// machine at the far end of an SSH session, where this host's browser,
// clipboard and desktop are out of the user's reach.
func Remote() bool {
	if inTmux() {
		// tmux refreshes the variable from every client that attaches, while
		// this process kept the value its pane was started with.
		if line, err := tmuxSSHConnection(); err == nil {
			return strings.HasPrefix(line, "SSH_CONNECTION=")
		}
	}
	return getenv("SSH_CONNECTION") != ""
}
