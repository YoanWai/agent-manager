package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/YoanWai/agent-manager/internal/deps"
	"github.com/YoanWai/agent-manager/internal/update"
)

type OutdatedToolError struct {
	Binary            string
	Installed         string
	Minimum           string
	ManagedExternally bool
}

func (e OutdatedToolError) Error() string {
	head := e.Binary + " " + e.Installed + " is older than " + e.Minimum + ", the oldest version agent-manager supports."
	if command := deps.Command(e.Binary); command != "" && !e.ManagedExternally {
		return head + " Update it with: " + command
	}
	return head + " Update it the way you installed it."
}

var versionNumber = regexp.MustCompile(`\d+\.\d+\.\d+`)

// readVersionOutput gives the probe ten seconds, and a second more for a
// child the CLI leaves holding stdout, so one stuck CLI cannot hold the
// probe lock and every spawn with it.
var readVersionOutput = func(path string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if errors.Is(err, exec.ErrWaitDelay) {
		return out, nil
	}
	return out, err
}

// probedVersions maps a build on disk to the version it printed.
var probedVersions = map[string]string{}
var versionProbeMu sync.Mutex

// CheckVersion refuses a tool whose installed CLI is older than its
// MinimumVersion. A CLI this machine cannot run is the install error, so
// the probe never reads a Windows copy on WSL as an old Linux build.
func CheckVersion(tool Tool) error {
	fields := strings.Fields(tool.Command)
	if tool.MinimumVersion == "" || len(fields) == 0 {
		return nil
	}
	if err := CheckInstalled(tool.Command); err != nil {
		return err
	}
	binary := fields[0]
	installed, resolved, err := installedVersion(binary)
	if err != nil {
		return err
	}
	if update.VersionWithin(installed, tool.MinimumVersion, "") {
		return nil
	}
	return OutdatedToolError{Binary: binary, Installed: installed, Minimum: tool.MinimumVersion,
		ManagedExternally: strings.HasPrefix(filepath.ToSlash(resolved), "/nix/store/")}
}

// installedVersion asks each build once: an upgrade replaces the file or
// the link to it, which changes the stamp. It also names the file the
// probe ran, so the caller reads the same installation it measured.
func installedVersion(binary string) (version, resolved string, err error) {
	versionProbeMu.Lock()
	defer versionProbeMu.Unlock()
	path, err := lookPath(binary)
	if err != nil {
		return "", "", err
	}
	resolved, err = filepath.EvalSymlinks(path)
	if err != nil {
		return "", "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", "", err
	}
	stamp := fmt.Sprintf("%s@%d@%d", resolved, info.ModTime().UnixNano(), info.Size())
	if version, ok := probedVersions[stamp]; ok {
		return version, resolved, nil
	}
	out, err := readVersionOutput(path)
	if err != nil {
		return "", "", fmt.Errorf("%s --version: %w", binary, err)
	}
	version = versionNumber.FindString(string(out))
	if version == "" {
		return "", "", fmt.Errorf("%s --version printed %q, which names no version", binary, strings.TrimSpace(string(out)))
	}
	probedVersions[stamp] = version
	return version, resolved, nil
}
