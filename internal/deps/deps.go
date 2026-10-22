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
// is the exception. macOS uses Homebrew alone, and installs it when it is absent.
var linuxManagers = []manager{
	{"apt-get", "sudo apt-get update && sudo apt-get install -y "},
	{"dnf", "sudo dnf install -y "},
	{"pacman", "sudo pacman -S --needed "},
	{"zypper", "sudo zypper install -y "},
	{"apk", "sudo apk add "},
	{"brew", "brew install "},
}

// homebrewSetup installs Homebrew, then the packages. A Mac without
// Homebrew has no tmux package to offer otherwise.
const homebrewSetup = `/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)" && eval "$(/opt/homebrew/bin/brew shellenv 2>/dev/null || /usr/local/bin/brew shellenv)" && brew install `

var windowsManagers = []manager{
	{"scoop", "scoop install "},
	{"choco", "choco install -y "},
}

// windowsPackages are winget ids. A bare name search stops to ask which
// package, and winget is present on a current Windows install when scoop
// and choco are not.
var windowsPackages = map[string]string{
	"git":  "winget install --id Git.Git -e",
	"pwsh": "winget install --id Microsoft.PowerShell -e",
}

// official is the vendor installer for a built-in agent CLI. It is the same
// command on macOS, Linux, and WSL, and it wins over a package manager
// that would guess `brew install claude`.
var official = map[string]string{
	"claude":       "curl -fsSL https://claude.ai/install.sh | bash",
	"cmd":          unixNpm("cmd", "command-code"),
	"codex":        "curl -fsSL https://chatgpt.com/codex/install.sh | sh",
	"muse":         "curl -fsSL https://dev.meta.ai/install.sh | bash",
	"omp":          "curl -fsSL https://omp.sh/install | sh",
	"grok":         "curl -fsSL https://x.ai/cli/install.sh | bash",
	"gemini":       unixNpm("gemini", "@google/gemini-cli"),
	"agy":          "curl -fsSL https://antigravity.google/cli/install.sh | bash",
	"opencode":     unixOpencode(),
	"cursor-agent": "curl https://cursor.com/install -fsS | bash",
	"hermes":       "curl -fsSL https://hermes-agent.nousresearch.com/install.sh | bash",
	"pi":           "curl -fsSL https://pi.dev/install.sh | sh",
}

// windowsOfficial replaces official on native Windows, where the curl
// installers do not run. A tool missing here has no Windows recipe.
var windowsOfficial = map[string]string{
	"claude":       "irm https://claude.ai/install.ps1 | iex",
	"cmd":          windowsNpm("command-code"),
	"codex":        "& ([scriptblock]::Create((irm https://chatgpt.com/codex/install.ps1)))",
	"gemini":       windowsNpm("@google/gemini-cli"),
	"agy":          "irm https://antigravity.google/cli/install.ps1 | iex",
	"grok":         "irm https://x.ai/cli/install.ps1 | iex",
	"hermes":       "& ([scriptblock]::Create((irm https://hermes-agent.nousresearch.com/install.ps1))) -NonInteractive -Branch main",
	"muse":         "irm https://dev.meta.ai/install.ps1 | iex",
	"omp":          "irm https://omp.sh/install.ps1 | iex",
	"opencode":     windowsNpm("@opencode/cli"),
	"pi":           windowsNpm("--ignore-scripts @earendil-works/pi-coding-agent"),
	"psmux":        "winget install --id marlocarlo.psmux -e",
	"cursor-agent": "irm 'https://cursor.com/install?win32=true' | iex",
}

// shown is the one line the setup dialog prints and copies for a recipe
// that runs as a script, which would fill the card with shell.
var shown = map[string]string{
	"cmd":      "npm install -g command-code",
	"gemini":   "npm install -g @google/gemini-cli",
	"opencode": "curl -fsSL https://opencode.ai/v2/install | bash",
}

func installers(goos string) map[string]string {
	if goos == "windows" {
		return windowsOfficial
	}
	return official
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
	return installers(runtime.GOOS)[tool]
}

// Shown is the install command as the dialog prints and copies it: the
// vendor's one line, where Command may be the script that carries it out.
func Shown(tool string) string {
	return shownCommand(runtime.GOOS, tool)
}

func shownCommand(goos, tool string) string {
	if goos != "windows" && shown[tool] != "" {
		return shown[tool]
	}
	return installers(goos)[tool]
}

func hint(goos, tool string) string {
	if command := shownCommand(goos, tool); command != "" {
		return "install it with: " + command
	}
	if command := installCommand(goos, tool); command != "" {
		return "install it with: " + command
	}
	return "install " + tool + " with your package manager"
}

func installCommand(goos, tool string) string {
	if goos == "darwin" {
		if _, err := lookPath("brew"); err != nil {
			return homebrewSetup + tool
		}
		return "brew install " + tool
	}
	if goos == "windows" {
		if command := windowsPackages[tool]; command != "" {
			if _, err := lookPath("winget"); err == nil {
				return command
			}
		}
	}
	candidates := linuxManagers
	switch goos {
	case "windows":
		candidates = windowsManagers
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
