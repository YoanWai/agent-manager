//go:build unix

package remote

import (
	"os/exec"
	"path/filepath"
	"syscall"
)

// killGroup runs ssh in a process group of its own and ends the whole group
// at the deadline, since a ProxyCommand or ProxyJump helper outlives a
// killed ssh.
func killGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

// sharing runs every call to a host over one master connection, kept a
// minute past the last, so a poll does not open a new connection each time.
func sharing(controlDir string) []string {
	return []string{
		"-o", "ControlMaster=auto",
		"-o", "ControlPersist=60",
		"-o", "ControlPath=" + filepath.Join(controlDir, "%C"),
	}
}

// prepareSharing makes the directory the master's socket lives in.
func prepareSharing(controlDir string) error {
	return ensureControlDir(controlDir)
}
