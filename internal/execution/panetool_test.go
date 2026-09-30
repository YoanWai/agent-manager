package execution

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/config"
)

func TestToolBinaryNamesWhatARowRuns(t *testing.T) {
	cases := map[string]struct {
		tool config.Tool
		want string
	}{
		"plain command":     {config.Tool{Command: "claude"}, "claude"},
		"command with args": {config.Tool{Command: "hermes --cli"}, "hermes"},
		"absolute path":     {config.Tool{Command: "/opt/homebrew/bin/codex"}, "codex"},
		"shell":             {config.Tool{Command: "bash -l", Shell: true}, ""},
		"no command":        {config.Tool{}, ""},
		"wrapper script":    {config.Tool{Command: "sh -c 'claude'"}, ""},
	}
	for name, tc := range cases {
		if got := toolBinary(tc.tool); got != tc.want {
			t.Errorf("%s: toolBinary = %q, want %q", name, got, tc.want)
		}
	}
}

func TestDetectRelaunchedTool(t *testing.T) {
	binaries := ToolBinaries{
		"claude":   "claude",
		"codex":    "codex",
		"terminal": "",
		"gemini":   "gemini",
		// Two blocks of the same CLI: nothing in a process name says which
		// of them a pane is running.
		"grok":      "grok",
		"grok-fast": "grok",
	}
	cases := map[string]struct {
		current  string
		children []string
		want     string
	}{
		"another CLI took the pane":         {"claude", []string{"/opt/homebrew/bin/codex"}, "codex"},
		"same CLI came back":                {"claude", []string{"claude"}, ""},
		"nothing agent-like running":        {"claude", []string{"vim", "-bash"}, ""},
		"empty pane":                        {"claude", nil, ""},
		"terminals stay terminals":          {"terminal", []string{"claude"}, ""},
		"unknown tool row":                  {"retired-tool", []string{"codex"}, ""},
		"two blocks claim it":               {"claude", []string{"grok"}, ""},
		"agent beside its own CLI":          {"claude", []string{"codex", "claude"}, ""},
		"one configured CLI beside another": {"claude", []string{"codex", "aider"}, "codex"},
		"two configured CLIs":               {"claude", []string{"codex", "gemini"}, ""},
		"a shell is not a CLI":              {"claude", []string{"sh", "node"}, ""},
	}
	for name, tc := range cases {
		if got := detectRelaunchedTool(tc.current, tc.children, binaries); got != tc.want {
			t.Errorf("%s: detectRelaunchedTool = %q, want %q", name, got, tc.want)
		}
	}
}
