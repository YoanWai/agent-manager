package update

import (
	"os"
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
	miseSlug := miseManaged(slashPath)
	switch {
	case scoopManaged(slashPath):
		// Scoop refuses to update an app while its binary is running.
		return Manager{Name: "Scoop", Advice: "installed with Scoop. Close agent-manager, then run: scoop update agent-manager. For a global install, add --global in an elevated PowerShell."}
	case homebrewTree(slashPath) == "Caskroom":
		return Manager{Name: "Homebrew", Command: []string{"brew", "upgrade", "--cask", "yoanwai/tap/agent-manager"}}
	case buildSource == "Homebrew" || homebrewTree(slashPath) != "":
		return Manager{Name: "Homebrew", Command: []string{"brew", "upgrade", "agent-manager"}}
	case miseSlug != "":
		return Manager{Name: "mise", Command: []string{"mise", "upgrade", "--bump", miseSlug}}
	case strings.HasPrefix(slashPath, "/nix/store/"):
		// The store is read-only, and the user's Nix profile or config owns the install.
		return Manager{Name: "Nix", Advice: "installed with Nix; update it through Nix, for example: nix profile upgrade agent-manager"}
	case pacmanOwns(path):
		return archManager()
	}
	return Manager{}
}

func scoopManaged(path string) bool {
	path = strings.ToLower(strings.ReplaceAll(path, `\`, "/"))
	if scoopLayout(path) {
		return true
	}
	for _, key := range []string{"SCOOP", "SCOOP_GLOBAL"} {
		root := os.Getenv(key)
		if root == "" {
			continue
		}
		// The running binary is already resolved. A Scoop root can be a
		// junction or a Windows short path, so resolve it the same way
		// before comparing prefixes.
		if resolved, err := filepath.EvalSymlinks(root); err == nil {
			root = resolved
		}
		root = strings.TrimRight(strings.ToLower(strings.ReplaceAll(root, `\`, "/")), "/")
		if root != "" && strings.HasPrefix(path, root+"/apps/agent-manager/") {
			return true
		}
	}
	return false
}

// scoopLayout matches Scoop's default tree: a directory named scoop, then
// apps, then agent-manager. The name has to be the whole segment. A
// directory such as not-scoop is a normal install, and the in-place
// updater still replaces that binary.
func scoopLayout(path string) bool {
	parts := strings.Split(path, "/")
	for i := 0; i+3 < len(parts); i++ {
		if parts[i] == "scoop" && parts[i+1] == "apps" && parts[i+2] == "agent-manager" {
			return true
		}
	}
	return false
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

// miseSlugs maps mise's install dir name to the ubi slug it was installed
// as. mise keys an install by the slug it was first registered under, so
// each dir upgrades through its own slug.
var miseSlugs = map[string]string{
	"ubi-yoan-wai-agent-manager": "ubi:YoanWai/agent-manager",
	"ubi-wainmann-agent-manager": "ubi:wainmann/agent-manager",
}

// miseManaged matches mise's install layout regardless of MISE_DATA_DIR,
// .../installs/<dir>/<version>/agent-manager, and returns the ubi slug the
// dir belongs to, or "" when the path is not a mise install.
func miseManaged(slashPath string) string {
	parts := strings.Split(slashPath, "/")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] != "installs" {
			continue
		}
		if slug, ok := miseSlugs[parts[i+1]]; ok {
			return slug
		}
	}
	return ""
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
