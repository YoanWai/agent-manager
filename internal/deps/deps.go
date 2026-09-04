// Package deps reports how to install the external tools agent-manager runs,
// so a missing binary names the command that fixes it instead of a bare
// "not found in PATH". Agent CLIs use the vendor's portable installer;
// tmux and git use the package manager on this machine.
package deps

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

var lookPath = exec.LookPath
var getUID = os.Getuid

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

// official is the vendor installer for a built-in agent CLI. It is the same
// command on macOS, Linux, and WSL, and it wins over a package manager
// that would guess `brew install claude`.
var official = map[string]string{
	"claude":   "curl -fsSL https://claude.ai/install.sh | bash",
	"cmd":      "npm install -g command-code",
	"codex":    "curl -fsSL https://chatgpt.com/codex/install.sh | sh",
	"grok":     "curl -fsSL https://x.ai/cli/install.sh | bash",
	"gemini":   "npm install -g @google/gemini-cli",
	"opencode": "curl -fsSL https://opencode.ai/install | bash",
	"hermes":   "curl -fsSL https://hermes-agent.nousresearch.com/install.sh | bash",
	"pi":       "npm install -g @mariozechner/pi-coding-agent",
}

func Hint(tool string) string {
	return hint(runtime.GOOS, tool)
}

// Command is the install command the setup dialog offers to run, empty
// when there is none to offer. Only the agent CLIs with a vendor
// installer have one: a package-manager line is built from the tool's
// own command name, so for a custom tool it would install whatever
// package happens to share that name. Those stay a suggestion to read.
func Command(tool string) string {
	return official[tool]
}

func hint(goos, tool string) string {
	if command := official[tool]; command != "" {
		return "install it with: " + command
	}
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
		if _, err := lookPath(candidate.bin); err != nil {
			continue
		}
		command := privilegedInstall(candidate.install)
		if command == "" {
			continue
		}
		return command + tool
	}
	return ""
}

func privilegedInstall(command string) string {
	if !strings.Contains(command, "sudo ") {
		return command
	}
	if getUID() == 0 {
		return strings.ReplaceAll(command, "sudo ", "")
	}
	if _, err := lookPath("sudo"); err == nil {
		return command
	}
	return ""
}
