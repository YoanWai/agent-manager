//go:build !unix

package remote

import "os/exec"

// killGroup keeps the default cancel, which ends ssh alone; WaitDelay still
// bounds a helper that holds the pipe after it.
func killGroup(*exec.Cmd) {}

// sharing is empty: Win32-OpenSSH, the ssh.exe Windows ships, has no
// ControlMaster. It reads the options but links a stub in place of the mux
// client, and a ControlPath is reported to break the call rather than be
// ignored (PowerShell/Win32-OpenSSH#405). Every call opens its own
// connection.
func sharing(string) []string { return nil }

// prepareSharing has no socket to make a directory for.
func prepareSharing(string) error { return nil }
