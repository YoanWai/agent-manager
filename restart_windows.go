//go:build windows

package main

import (
	"errors"
	"os"
	"os/exec"
)

// restart runs the binary at path as a child on this console and exits with
// its status. Windows has no exec: the parent holds the console until the
// child is done so the shell does not take it back in between. The psmux
// sessions are separate processes and are unaffected.
func restart(path string) error {
	cmd := exec.Command(path, os.Args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = os.Environ()
	err := cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		os.Exit(exitErr.ExitCode())
	}
	if err != nil {
		return err
	}
	os.Exit(0)
	return nil
}
