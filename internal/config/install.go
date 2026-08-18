package config

import (
	"os/exec"
	"strings"
)

var lookPath = exec.LookPath

// MissingToolError names the missing binary and the vendor's portable
// installer when agent-manager knows one.
type MissingToolError struct {
	Binary  string
	Install string
}

func (e MissingToolError) Error() string {
	if e.Install != "" {
		return e.Binary + " is not installed; install it with: " + e.Install
	}
	return e.Binary + " is not installed"
}

var officialInstall = map[string]string{
	"claude":   "curl -fsSL https://claude.ai/install.sh | bash",
	"codex":    "curl -fsSL https://chatgpt.com/codex/install.sh | sh",
	"grok":     "curl -fsSL https://x.ai/cli/install.sh | bash",
	"gemini":   "npm install -g @google/gemini-cli",
	"opencode": "curl -fsSL https://opencode.ai/install | bash",
	"hermes":   "curl -fsSL https://hermes-agent.nousresearch.com/install.sh | bash",
	"pi":       "npm install -g @mariozechner/pi-coding-agent",
}

func CheckInstalled(tool Tool) error {
	fields := strings.Fields(tool.Command)
	if len(fields) == 0 {
		return nil
	}
	binary := fields[0]
	if _, err := lookPath(binary); err == nil {
		return nil
	}
	return MissingToolError{Binary: binary, Install: officialInstall[binary]}
}
