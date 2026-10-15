//go:build !windows

package ui

import "github.com/YoanWai/agent-manager/internal/tmux"

// installScript shows the command, runs it, and records how it ended. The
// command runs in a subshell so an installer that exits cannot skip the
// status write, and the whole thing is a file so the pane's shell is typed
// one short line: an installer's own quoting then reaches sh unchanged
// whatever shell the user runs.
func installScript(command, statusFile string) string {
	interrupted := "printf %s 130 > " + tmux.ShellQuote(statusFile) + "; exit 130"
	return "#!/bin/sh\n" +
		"trap " + tmux.ShellQuote(interrupted) + " INT TERM\n" +
		"printf '%s\\n' " + tmux.ShellQuote("$ "+command) + "\n" +
		"(" + command + ")\n" +
		`printf %s "$?" > ` + tmux.ShellQuote(statusFile) + "\n"
}

// installLine is what the pane's shell is typed to run the install script.
func installLine(script string) (string, error) {
	return "sh " + tmux.ShellQuote(script), nil
}
