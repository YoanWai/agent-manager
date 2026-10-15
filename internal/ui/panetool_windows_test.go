//go:build windows

package ui

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/config"
)

// The process snapshot names images in lower case without .exe, so a
// command spelling either still matches the CLI its pane runs.
func TestToolBinaryMatchesTheSnapshotNameOfAWindowsCommand(t *testing.T) {
	binaries := toolBinaries{"claude": toolBinary(config.Tool{Command: `C:\Tools\Claude.exe --verbose`}), "codex": toolBinary(config.Tool{Command: "codex.exe"})}
	if got := detectRelaunchedTool("claude", []string{"codex"}, binaries); got != "codex" {
		t.Fatalf("detectRelaunchedTool = %q, want codex", got)
	}
	if got := detectRelaunchedTool("claude", []string{"claude"}, binaries); got != "" {
		t.Fatalf("the CLI the row launched with read as %q", got)
	}
}
