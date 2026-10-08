//go:build !unix

package remote

import "os/exec"

// killGroup keeps the default cancel, which ends ssh alone; WaitDelay still
// bounds a helper that holds the pipe after it.
func killGroup(*exec.Cmd) {}
