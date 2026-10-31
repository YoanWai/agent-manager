//go:build !windows

package install

import "github.com/YoanWai/agent-manager/internal/tmux"

// Script shows the command, runs it, and records how it ended. The command
// runs in a subshell so an installer that exits cannot skip the status write,
// and the whole thing is a file so the pane's shell is typed one short line:
// an installer's own quoting then reaches sh unchanged whatever shell the user
// runs.
func Script(command, statusFile string) string {
	interrupted := "printf %s 130 > " + tmux.ShellQuote(statusFile) + "; exit 130"
	return "#!/bin/sh\n" +
		"trap " + tmux.ShellQuote(interrupted) + " INT TERM\n" +
		"printf '%s\\n' " + tmux.ShellQuote("$ "+command) + "\n" +
		"(" + command + ")\n" +
		`printf %s "$?" > ` + tmux.ShellQuote(statusFile) + "\n"
}

func Line(script string) (string, error) {
	return "sh " + tmux.ShellQuote(script), nil
}
