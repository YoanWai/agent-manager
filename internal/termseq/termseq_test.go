package termseq

import (
	"errors"
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

// fakeSession stands in for the two places an SSH connection is recorded:
// this process's environment and the hosting tmux's session environment.
func fakeSession(t *testing.T, tmux bool, processValue, tmuxAnswer string, tmuxErr error) {
	t.Helper()
	origTmux, origGetenv, origShow := inTmux, getenv, tmuxSSHConnection
	t.Cleanup(func() { inTmux, getenv, tmuxSSHConnection = origTmux, origGetenv, origShow })
	inTmux = func() bool { return tmux }
	getenv = func(name string) string {
		if name == "SSH_CONNECTION" {
			return processValue
		}
		return ""
	}
	tmuxSSHConnection = func() (string, error) { return tmuxAnswer, tmuxErr }
}

func TestRemote(t *testing.T) {
	const connection = "203.0.113.7 51022 198.51.100.2 22"
	cases := []struct {
		name       string
		tmux       bool
		process    string
		tmuxAnswer string
		tmuxErr    error
		want       bool
	}{
		{name: "shell over ssh", process: connection, want: true},
		{name: "local shell", want: false},
		{name: "tmux last attached over ssh", tmux: true, tmuxAnswer: "SSH_CONNECTION=" + connection + "\n", want: true},
		{name: "tmux started over ssh, attached locally since", tmux: true, process: connection, tmuxAnswer: "-SSH_CONNECTION\n", want: false},
		{name: "tmux that does not track the variable", tmux: true, process: connection, tmuxErr: errors.New("unknown variable: SSH_CONNECTION"), want: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeSession(t, c.tmux, c.process, c.tmuxAnswer, c.tmuxErr)
			if got := Remote(); got != c.want {
				t.Fatalf("Remote() = %v, want %v", got, c.want)
			}
		})
	}
}
