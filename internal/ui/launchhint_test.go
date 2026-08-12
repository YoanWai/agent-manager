package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/mcpreg"
	"github.com/YoanWai/agent-manager/internal/status"
	tea "github.com/charmbracelet/bubbletea"
)

func TestReportLaunchErrorOpensInstallHintForHermes(t *testing.T) {
	m := buildModel(t)

	m.reportLaunchError(fmt.Errorf("launch: %w", mcpreg.ErrHermesMCPUnavailable))

	if m.mode != modeLaunchHint {
		t.Fatalf("mode = %v, want modeLaunchHint", m.mode)
	}
	if !strings.Contains(m.launchHint, "hermes setup") {
		t.Fatalf("hint %q should name the install command", m.launchHint)
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m = updated.(*Model)
	if m.mode != modeList {
		t.Fatalf("after esc, mode = %v, want modeList", m.mode)
	}
	if m.launchHint != "" {
		t.Fatalf("dismiss should clear the hint, got %q", m.launchHint)
	}
}

func TestReportLaunchErrorKeepsPlainErrorsOnStatusLine(t *testing.T) {
	m := buildModel(t)

	m.reportLaunchError(fmt.Errorf("tmux create: boom"))

	if m.mode != modeList {
		t.Fatalf("mode = %v, want modeList", m.mode)
	}
	if m.errBar.text != "tmux create: boom" {
		t.Fatalf("errBar = %q", m.errBar.text)
	}
}

// The whole spawn path: a Hermes without its MCP SDK must not produce a
// session, and the dialog naming the fix must be what the user sees.
func TestSpawnHermesWithoutMCPSupportPromptsInstall(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake Hermes executable is a shell script")
	}
	m := buildModel(t)
	m.cfg.Tools["hermes"] = config.Tool{Command: "cat", DefaultStatus: status.Idle}
	bin := t.TempDir()
	script := `#!/bin/sh
if [ "$1" = "config" ]; then
  exit 1
fi
printf "Failed to connect: MCP server 'agent-manager' requires the 'mcp' Python SDK, but it is not installed. Run 'hermes setup' to install MCP support, then retry.\n"
exit 0
`
	if err := os.WriteFile(filepath.Join(bin, "hermes"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	m.openForm()
	m.form.name.SetValue("agent")
	m.form.dir.SetValue(t.TempDir())
	hermesIndex := -1
	for i, name := range m.form.toolNames {
		if name == "hermes" {
			hermesIndex = i
		}
	}
	if hermesIndex < 0 {
		t.Fatalf("hermes not offered by the form: %v", m.form.toolNames)
	}
	m.form.toolIndex = hermesIndex
	pickGroup(t, m, "")
	m.submitForm()

	if m.mode != modeLaunchHint {
		t.Fatalf("mode = %v, err = %q, want modeLaunchHint", m.mode, m.errBar.text)
	}
	if len(m.sessionRows()) != 0 {
		t.Fatalf("no session may spawn without MCP support, got %v", sessionNames(m))
	}
}
