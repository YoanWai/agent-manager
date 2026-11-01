//go:build !darwin && !linux

package termbrowser

import "os/exec"

func configureBoundedCommand(cmd *exec.Cmd) {}
