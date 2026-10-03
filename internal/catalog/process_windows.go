//go:build windows

package catalog

import (
	"os/exec"
)

// startProcessGroup is a no-op on Windows: there is no process group to
// detach into, and the manager's own job already covers the tree.
func startProcessGroup(*exec.Cmd) {}

// killGroup has no group to signal on Windows; stop closes the process's
// stdin, which the server and any child it launched read.
func killGroup(int) {}
