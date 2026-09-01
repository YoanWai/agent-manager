package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/mcpreg"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/charmbracelet/x/ansi"
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
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(*Model)
	if m.mode != modeList {
		t.Fatalf("after esc, mode = %v, want modeList", m.mode)
	}
	if m.launchHint != "" {
		t.Fatalf("dismiss should clear the hint, got %q", m.launchHint)
	}
}

func TestReportLaunchErrorOpensInstallHintForMissingCLI(t *testing.T) {
	m := buildModel(t)

	m.reportLaunchError(config.MissingToolError{Binary: "claude"})

	if m.mode != modeLaunchHint {
		t.Fatalf("mode = %v, want modeLaunchHint", m.mode)
	}
	if !strings.Contains(m.launchHint, "claude.ai/install.sh") {
		t.Fatalf("hint %q should name the install command", m.launchHint)
	}
	if !strings.Contains(m.launchHint, "claude") {
		t.Fatalf("hint %q should name the missing CLI", m.launchHint)
	}
	frame := ansi.Strip(m.viewLaunchHint())
	if !strings.Contains(frame, "claude.ai/install.sh") {
		t.Fatalf("dialog should show the install command:\n%s", frame)
	}
}

func TestReportLaunchErrorOpensHintForUnknownMissingCLI(t *testing.T) {
	m := buildModel(t)

	m.reportLaunchError(config.MissingToolError{Binary: "acme"})

	if m.mode != modeLaunchHint {
		t.Fatalf("mode = %v, want modeLaunchHint", m.mode)
	}
	if !strings.Contains(m.launchHint, "acme") {
		t.Fatalf("hint %q should name the missing CLI", m.launchHint)
	}
	if !strings.Contains(m.launchHint, "install") {
		t.Fatalf("hint %q should name how to install", m.launchHint)
	}
}

func TestSpawnMissingCLIPromptsInstall(t *testing.T) {
	m := buildModel(t)
	m.cfg.Tools["claude"] = config.Tool{Command: "am-missing-cli-xyz", DefaultStatus: status.Idle}

	m.openForm()
	m.form.name.SetValue("agent")
	m.form.dir.SetValue(t.TempDir())
	claudeIndex := -1
	for i, name := range m.form.toolNames {
		if name == "claude" {
			claudeIndex = i
		}
	}
	if claudeIndex < 0 {
		t.Fatalf("claude not offered by the form: %v", m.form.toolNames)
	}
	m.form.toolIndex = claudeIndex
	pickGroup(t, m, "")
	m.submitForm()

	if m.mode != modeLaunchHint {
		t.Fatalf("mode = %v, err = %q, want modeLaunchHint", m.mode, m.errBar.text)
	}
	if !strings.Contains(m.launchHint, "am-missing-cli-xyz") {
		t.Fatalf("hint %q should name the missing binary", m.launchHint)
	}
	if len(m.sessionRows()) != 0 {
		t.Fatalf("no session may spawn without the CLI, got %v", sessionNames(m))
	}
}

func TestReviveMissingCLIPromptsInstall(t *testing.T) {
	m := buildModel(t)
	sess := store.Session{ID: newID(), Name: "agent", Tool: "claude", Cwd: t.TempDir()}
	if err := m.store.CreateSession(sess); err != nil {
		t.Fatal(err)
	}
	m.sessions = []store.Session{sess}
	m.cfg.Tools["claude"] = config.Tool{
		Command:       "cat",
		ReviveCommand: "am-missing-cli-xyz",
		DefaultStatus: status.Idle,
	}

	err := m.reviveSession(sess)
	if err == nil {
		t.Fatal("revive of a missing CLI should fail")
	}
	m.reportLaunchError(err)
	if m.mode != modeLaunchHint {
		t.Fatalf("mode = %v, err = %q, want modeLaunchHint", m.mode, m.errBar.text)
	}
	if !strings.Contains(m.launchHint, "am-missing-cli-xyz") {
		t.Fatalf("hint %q should name the revive binary", m.launchHint)
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

// installSDKlessHermes puts a fake Hermes on PATH that answers mcp add the
// way a real one without the optional SDK does: refusing to connect, saving
// nothing, and exiting 0 after its save-anyway prompt.
func installSDKlessHermes(t *testing.T) {
	t.Helper()
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
}

// The restart confirm dialog resets to the list when it closes; the hint
// dialog a refused relaunch opened must survive that reset.
func TestRestartHermesWithoutMCPSupportPromptsInstall(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake Hermes executable is a shell script")
	}
	m := buildModel(t)
	m.cfg.Tools["hermes"] = config.Tool{Command: "cat", DefaultStatus: status.Idle}
	installSDKlessHermes(t)
	sess := store.Session{ID: newID(), Name: "agent", Tool: "hermes", Cwd: t.TempDir()}
	m.confirm = confirmTarget{action: actionRestart, sessions: []store.Session{sess}}
	m.mode = modeConfirmDelete

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	m = updated.(*Model)

	if m.mode != modeLaunchHint {
		t.Fatalf("mode = %v, err = %q, want modeLaunchHint", m.mode, m.errBar.text)
	}
}

// A quick prompt whose spawn was refused has nothing left to send: the bar
// must be gone once the hint dialog closes, not swallowing list keys.
func TestQuickSpawnHermesWithoutMCPSupportClosesTheBar(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake Hermes executable is a shell script")
	}
	m := buildModel(t)
	m.cfg.Tools["hermes"] = config.Tool{Command: "cat", DefaultStatus: status.Idle}
	installSDKlessHermes(t)
	if err := m.store.CreateGroup("backend", t.TempDir()); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "backend")
	m.quick.active = true
	m.quick.toolNames = []string{"hermes"}

	updated, _ := m.quickSpawn("backend", "fix the tests")
	m = updated.(*Model)

	if m.mode != modeLaunchHint {
		t.Fatalf("mode = %v, err = %q, want modeLaunchHint", m.mode, m.errBar.text)
	}
	if m.quick.active {
		t.Fatal("a refused quick spawn must close the bar")
	}
}

// A form spawn the hint dialog refused takes the form off screen with it,
// so the images its prompt was holding have nothing left naming them.
func TestFormSpawnRefusedByTheHintReleasesItsImages(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake Hermes executable is a shell script")
	}
	m := buildModel(t)
	m.cfg.Tools["hermes"] = config.Tool{Command: "cat", DefaultStatus: status.Idle}
	installSDKlessHermes(t)

	m.openForm()
	m.form.name.SetValue("agent")
	m.form.dir.SetValue(t.TempDir())
	m.form.toolNames = []string{"hermes"}
	m.form.toolIndex = 0
	path := tempImage(t, "mock.png")
	m.form.prompt.attachments = []imageAttachment{{id: 1, path: path}}
	m.form.prompt.input.SetValue("match " + imageToken(1))

	m.submitForm()

	if m.mode != modeLaunchHint {
		t.Fatalf("mode = %v, err = %q, want modeLaunchHint", m.mode, m.errBar.text)
	}
	if len(m.form.prompt.attachments) != 0 {
		t.Fatalf("attachments = %+v, want the refused form's images released", m.form.prompt.attachments)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the image file should be gone, stat err = %v", err)
	}
}

// A spawn that fails into the status bar leaves the form up, so the prompt
// still names its images and they have to survive for the retry. The
// failure is a worktree that cannot be created, which is the far side of
// spawnSession rather than a field the form could have validated.
func TestFormSpawnErrorInTheBarKeepsItsImages(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	initGitRepo(t, dir)
	m.openForm()
	m.form.name.SetValue("agent")
	m.form.dir.SetValue(dir)
	m.form.worktree = true
	m.form.worktreeAuto = false
	if !m.formWorktreeOn() {
		t.Fatal("the worktree toggle should be on for this spawn")
	}
	// AddWorktree checks out into <repo>-worktrees/<session>, and refuses a
	// path that is already there. Taking the name it would pick is what
	// fails this spawn, on the far side of every field the form validates.
	taken := filepath.Join(filepath.Dir(dir), filepath.Base(dir)+"-worktrees", "agent")
	if err := os.MkdirAll(taken, 0o755); err != nil {
		t.Fatal(err)
	}
	path := tempImage(t, "mock.png")
	m.form.prompt.attachments = []imageAttachment{{id: 1, path: path}}
	m.form.prompt.input.SetValue("match " + imageToken(1))

	m.submitForm()

	if m.mode != modeForm || m.errBar.text == "" {
		t.Fatalf("mode = %v, err = %q, want the form still up with the error", m.mode, m.errBar.text)
	}
	// Named so the test cannot pass on an earlier refusal: this is the
	// spawn failing, not a field the form checked before it got there.
	if !strings.Contains(m.errBar.text, taken) {
		t.Fatalf("err = %q, want the worktree path that blocked the spawn", m.errBar.text)
	}
	if len(m.sessionRows()) != 0 {
		t.Fatalf("a failed spawn leaves no session, got %v", sessionNames(m))
	}
	if len(m.form.prompt.attachments) != 1 {
		t.Fatalf("attachments = %+v, want the chip kept for the retry", m.form.prompt.attachments)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the image the prompt still names must survive: %v", err)
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
	installSDKlessHermes(t)

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
