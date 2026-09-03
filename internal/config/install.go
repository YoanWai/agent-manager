package config

import (
	"errors"
	"os/exec"
	"regexp"
	"strings"

	"github.com/YoanWai/agent-manager/internal/deps"
	"github.com/YoanWai/agent-manager/internal/wsl"
)

var lookPath = exec.LookPath
var wslDetect = wsl.Detect

// windowsMountPath matches a WSL interop mount, e.g. /mnt/c/Users/... .
// exec.LookPath happily resolves a Windows-side launcher through one of
// these because WSL2 appends the Windows PATH to the Linux PATH by
// default, but a pane spawned inside the distro cannot run it the way a
// tool installed in the distro runs.
var windowsMountPath = regexp.MustCompile(`^/mnt/[a-zA-Z](/|$)`)

type MissingToolError struct {
	Binary string
}

func (e MissingToolError) Error() string {
	return e.Binary + " is not installed; " + deps.Hint(e.Binary)
}

func CheckInstalled(command string) error {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return nil
	}
	binary := fields[0]
	path, err := lookPath(binary)
	if err == nil {
		if wslDetect() && windowsMountPath.MatchString(path) {
			return MissingToolError{Binary: binary}
		}
		return nil
	}
	if !errors.Is(err, exec.ErrNotFound) {
		return err
	}
	return MissingToolError{Binary: binary}
}
