//go:build !darwin && !linux

package tmux

import "os/exec"

func configureBoundedCommand(cmd *exec.Cmd) {}
