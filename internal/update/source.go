package update

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Manager describes who owns the installed binary. A delegated manager
// carries the upgrade command that replaces the in-place download, and
// Advice names the command to run by hand when none is runnable here.
type Manager struct {
	Name    string
	Command []string
	Advice  string
}

func (m Manager) Delegated() bool { return len(m.Command) > 0 }

func (m Manager) String() string { return strings.Join(m.Command, " ") }

// Test seams: lookPath finds upgrade helpers, pacmanOwns asks pacman
// whether it installed the binary.
var (
	lookPath   = exec.LookPath
	pacmanOwns = pacmanOwnsPath
)

// DetectManager reports which package manager owns the binary at execPath,
// from the ldflag package managers inject and the resolved install path.
// The zero Manager means the install is direct (install script, go install,
// manual download) and self-updates in place.
func DetectManager(execPath string) Manager {
	path := execPath
	if resolved, err := filepath.EvalSymlinks(execPath); err == nil {
		path = resolved
	}
	slashPath := filepath.ToSlash(path)
	switch {
	case homebrewTree(slashPath) == "Caskroom":
		return Manager{Name: "Homebrew", Command: []string{"brew", "upgrade", "--cask", "yoanwai/tap/agent-manager"}}
	case buildSource == "Homebrew" || homebrewTree(slashPath) != "":
		return Manager{Name: "Homebrew", Command: []string{"brew", "upgrade", "agent-manager"}}
	case miseManaged(slashPath):
		return Manager{Name: "mise", Command: []string{"mise", "upgrade", "--bump", "ubi:YoanWai/agent-manager"}}
	case pacmanOwns(path):
		return archManager()
	}
	return Manager{}
}

// homebrewTree reports which Homebrew install tree the path sits in:
// "Caskroom", "Cellar", "opt", or "" when the path is not a brew install.
func homebrewTree(slashPath string) string {
	parts := strings.Split(slashPath, "/")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i+1] != "agent-manager" {
			continue
		}
		switch parts[i] {
		case "Cellar", "Caskroom":
			return parts[i]
		case "opt":
			// Only brew's opt tree counts: /opt/agent-manager is a
			// conventional manual-install prefix, not Homebrew.
			if i > 0 && (parts[i-1] == "homebrew" || parts[i-1] == ".linuxbrew") {
				return parts[i]
			}
		}
	}
	return ""
}

// miseManaged matches mise's install layout regardless of MISE_DATA_DIR:
// .../installs/ubi-yoan-wai-agent-manager/<version>/agent-manager.
func miseManaged(slashPath string) bool {
	parts := strings.Split(slashPath, "/")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] != "installs" {
			continue
		}
		tool := parts[i+1]
		if strings.HasPrefix(tool, "ubi-") && strings.HasSuffix(tool, "-agent-manager") {
			return true
		}
	}
	return false
}

func pacmanOwnsPath(path string) bool {
	if runtime.GOOS != "linux" {
		return false
	}
	pacman, err := lookPath("pacman")
	if err != nil {
		return false
	}
	return exec.Command(pacman, "-Qqo", path).Run() == nil
}

// archManager picks an installed AUR helper; pacman alone cannot build
// agent-manager-bin from the AUR.
func archManager() Manager {
	for _, helper := range []string{"yay", "paru"} {
		if _, err := lookPath(helper); err == nil {
			return Manager{Name: helper, Command: []string{helper, "-S", "agent-manager-bin"}}
		}
	}
	return Manager{Name: "pacman", Advice: "installed with pacman; install an AUR helper first, then: yay -S agent-manager-bin"}
}

// RestartTarget is the path main should exec after a delegated upgrade.
// The PATH entry wins over the resolved install dir: brew and mise may
// delete the version directory the running image was loaded from, while
// the PATH entry tracks the new install.
func RestartTarget(execPath string) string {
	if found, err := lookPath(filepath.Base(execPath)); err == nil {
		return found
	}
	return execPath
}
