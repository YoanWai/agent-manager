//go:build !windows

package catalog

import (
	"os/exec"
	"syscall"
)

// tree is a server's process group, named by its leader's pid.
type tree int

// prepareTree puts a server's children in the server's own process group,
// so stop reaches a forked child too.
func prepareTree(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// adoptTree names the group the started server leads.
func adoptTree(cmd *exec.Cmd) (tree, error) {
	return tree(cmd.Process.Pid), nil
}

// term asks the whole process group to stop.
func (t tree) term() {
	_ = syscall.Kill(-int(t), syscall.SIGTERM)
}

// close kills whatever of the process group is left, the server or a child
// that outlived it.
func (t tree) close() {
	_ = syscall.Kill(-int(t), syscall.SIGKILL)
}
