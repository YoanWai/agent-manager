//go:build windows

package git

import (
	"os/exec"
	"strconv"
)

// killFetchTree cancels the fetch by killing git and every process under
// it, so an ssh git started is not left holding the connection.
func killFetchTree(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		return exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
	}
}
