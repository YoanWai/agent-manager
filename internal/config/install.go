package config

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/YoanWai/agent-manager/internal/deps"
	"github.com/YoanWai/agent-manager/internal/wsl"
)

var (
	lookPath       = exec.LookPath
	isWSL          = wsl.Detect
	onWindowsMount = wsl.OnWindowsMount
)

type MissingToolError struct {
	Binary string
	// WindowsPath is set when the only copy on PATH is a Windows install
	// reached through WSL interop, which the distro cannot run as itself.
	WindowsPath string
}

func (e MissingToolError) Error() string {
	if e.WindowsPath != "" {
		return e.Binary + " is installed on Windows (" + e.WindowsPath + "), not in this WSL distro; " + deps.Hint(e.Binary)
	}
	return e.Binary + " is not installed; " + deps.Hint(e.Binary)
}

func CheckInstalled(command string) error {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return nil
	}
	binary := fields[0]
	path, err := lookPath(binary)
	if err != nil {
		if !errors.Is(err, exec.ErrNotFound) {
			return err
		}
		return MissingToolError{Binary: binary}
	}
	// A command naming a path asked for that exact file, wherever it lives.
	if strings.ContainsRune(binary, filepath.Separator) {
		return nil
	}
	windows, err := onWindowsInterop(path)
	if err != nil {
		return err
	}
	if !windows {
		return nil
	}
	// WSL appends the Windows PATH after the distro's own, so a Windows hit
	// this early usually means there is no Linux one; the rest of PATH is
	// walked because a user can order those entries however they like.
	distro, err := lookInDistro(binary)
	if err != nil {
		return err
	}
	if distro {
		return nil
	}
	return MissingToolError{Binary: binary, WindowsPath: path}
}

// onWindowsInterop reports whether a PATH hit is a Windows install the
// distro reaches through interop. Running it from a pane starts a Windows
// process, or dies for want of the Linux runtime the launcher expects, so
// it is not the agent CLI this distro can run.
func onWindowsInterop(path string) (bool, error) {
	if !isWSL() {
		return false, nil
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false, err
	}
	return onWindowsMount(resolved)
}

// lookInDistro walks PATH for a copy of the binary inside the distro,
// which a Windows hit earlier in PATH would otherwise hide.
func lookInDistro(binary string) (bool, error) {
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			dir = "."
		}
		hit, err := lookPath(filepath.Join(dir, binary))
		if err != nil {
			continue
		}
		windows, err := onWindowsInterop(hit)
		if err != nil {
			return false, err
		}
		if !windows {
			return true, nil
		}
	}
	return false, nil
}
