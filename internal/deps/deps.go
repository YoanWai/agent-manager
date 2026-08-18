// Package deps reports how to install the external tools agent-manager runs,
// so a missing tmux or git names the command that fixes it instead of a bare
// "not found in PATH".
package deps

import (
	"os/exec"
	"runtime"
)

var lookPath = exec.LookPath

type manager struct {
	bin     string
	install string
}

// Native package managers come before brew on Linux, where a Homebrew install
// is the exception; on macOS brew is the only one of these that ships packages.
var linuxManagers = []manager{
	{"apt-get", "sudo apt-get update && sudo apt-get install -y "},
	{"dnf", "sudo dnf install -y "},
	{"pacman", "sudo pacman -S --needed "},
	{"zypper", "sudo zypper install -y "},
	{"apk", "sudo apk add "},
	{"brew", "brew install "},
}

var darwinManagers = []manager{
	{"brew", "brew install "},
}

// Hint is a sentence fragment naming how to install tool, for appending to an
// error or a status line.
func Hint(tool string) string {
	return hint(runtime.GOOS, tool)
}

func hint(goos, tool string) string {
	if command := installCommand(goos, tool); command != "" {
		return "install it with: " + command
	}
	return "install " + tool + " with your package manager"
}

func installCommand(goos, tool string) string {
	candidates := linuxManagers
	if goos == "darwin" {
		candidates = darwinManagers
	}
	for _, candidate := range candidates {
		if _, err := lookPath(candidate.bin); err == nil {
			return candidate.install + tool
		}
	}
	return ""
}
