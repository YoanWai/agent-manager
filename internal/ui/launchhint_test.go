package ui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/clipboard"
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/deps"
	"github.com/YoanWai/agent-manager/internal/mcpreg"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestReportLaunchErrorOpensInstallHintForHermes(t *testing.T) {
	m := buildModel(t)

	m.reportLaunchError(fmt.Errorf("launch: %w", mcpreg.ErrHermesMCPUnavailable), nil)

	if m.mode != modeLaunchHint {
		t.Fatalf("mode = %v, want modeLaunchHint", m.mode)
	}
	if !strings.Contains(m.launchFix.text, "hermes setup") {
		t.Fatalf("hint %q should name the install command", m.launchFix.text)
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m = updated.(*Model)
	if m.mode != modeList {
		t.Fatalf("after esc, mode = %v, want modeList", m.mode)
	}
	if m.launchFix.text != "" {
		t.Fatalf("dismiss should clear the hint, got %q", m.launchFix.text)
	}
}

func TestReportLaunchErrorOpensInstallHintForMissingCLI(t *testing.T) {
	m := buildModel(t)

	m.reportLaunchError(config.MissingToolError{Binary: "claude"}, nil)

	if m.mode != modeLaunchHint {
		t.Fatalf("mode = %v, want modeLaunchHint", m.mode)
	}
	if !strings.Contains(m.launchFix.text, "claude.ai/install.sh") {
		t.Fatalf("hint %q should name the install command", m.launchFix.text)
	}
	if !strings.Contains(m.launchFix.text, "claude") {
		t.Fatalf("hint %q should name the missing CLI", m.launchFix.text)
	}
	frame := ansi.Strip(m.viewLaunchHint())
	if !strings.Contains(frame, "claude.ai/install.sh") {
		t.Fatalf("dialog should show the install command:\n%s", frame)
	}
}

func TestReportLaunchErrorOpensHintForUnknownMissingCLI(t *testing.T) {
	m := buildModel(t)

	m.reportLaunchError(config.MissingToolError{Binary: "acme"}, nil)

	if m.mode != modeLaunchHint {
		t.Fatalf("mode = %v, want modeLaunchHint", m.mode)
	}
	if !strings.Contains(m.launchFix.text, "acme") {
		t.Fatalf("hint %q should name the missing CLI", m.launchFix.text)
	}
	if !strings.Contains(m.launchFix.text, "install") {
		t.Fatalf("hint %q should name how to install", m.launchFix.text)
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
	if !strings.Contains(m.launchFix.text, "am-missing-cli-xyz") {
		t.Fatalf("hint %q should name the missing binary", m.launchFix.text)
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
	m.reportLaunchError(err, nil)
	if m.mode != modeLaunchHint {
		t.Fatalf("mode = %v, err = %q, want modeLaunchHint", m.mode, m.errBar.text)
	}
	if !strings.Contains(m.launchFix.text, "am-missing-cli-xyz") {
		t.Fatalf("hint %q should name the revive binary", m.launchFix.text)
	}
}

func TestReportLaunchErrorKeepsPlainErrorsOnStatusLine(t *testing.T) {
	m := buildModel(t)

	m.reportLaunchError(fmt.Errorf("tmux create: boom"), nil)

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

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
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
	// The dialog can still install the CLI and spawn this prompt, so it
	// takes the images over from the form and keeps the files until it
	// closes.
	if len(m.form.prompt.attachments) != 0 {
		t.Fatalf("attachments = %+v, want the form's images handed to the dialog", m.form.prompt.attachments)
	}
	if len(m.launchFix.images) != 1 {
		t.Fatalf("dialog images = %+v, want the refused prompt's image", m.launchFix.images)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the image file must outlive the refusal, stat err = %v", err)
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m = updated.(*Model)
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

func pressInLaunchHint(t *testing.T, m *Model, key rune) tea.Cmd {
	t.Helper()
	if m.mode != modeLaunchHint {
		t.Fatalf("mode = %v, want modeLaunchHint", m.mode)
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
	*m = *updated.(*Model)
	return cmd
}

// runBatch applies a command's message, and each message of a batch the
// runtime would have fanned out, the way the program loop does.
func (m *Model) runBatch(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, part := range batch {
			m.runBatch(t, part)
		}
		return
	}
	updated, _ := m.Update(msg)
	*m = *updated.(*Model)
}

func TestLaunchHintCopiesTheInstallCommand(t *testing.T) {
	m := buildModel(t)
	var copied string
	copyLaunchCommand = func(text string) error {
		copied = text
		return nil
	}
	t.Cleanup(func() { copyLaunchCommand = clipboard.WriteText })
	m.reportLaunchError(config.MissingToolError{Binary: "claude"}, nil)

	m.runBatch(t, pressInLaunchHint(t, m, 'c'))

	if want := deps.Command("claude"); copied != want {
		t.Fatalf("copied %q, want %q", copied, want)
	}
	if m.mode != modeLaunchHint {
		t.Fatalf("copy must leave the dialog up, mode = %v", m.mode)
	}
	if m.errBar.text != "copied to clipboard" || !m.errBar.worked() {
		t.Fatalf("status = %q, want the copy confirmed", m.errBar.text)
	}
}

func TestLaunchHintCopyFailureIsReported(t *testing.T) {
	m := buildModel(t)
	copyLaunchCommand = func(string) error { return errors.New("no clipboard backend") }
	t.Cleanup(func() { copyLaunchCommand = clipboard.WriteText })
	m.reportLaunchError(config.MissingToolError{Binary: "claude"}, nil)

	m.runBatch(t, pressInLaunchHint(t, m, 'c'))

	if !strings.Contains(m.errBar.text, "no clipboard backend") || m.errBar.worked() {
		t.Fatalf("status = %q, want the copy failure", m.errBar.text)
	}
}

// A tool with no known recipe gets a read-only dialog: nothing to copy,
// nothing to run.
func TestLaunchHintWithoutARecipeOffersOnlyClose(t *testing.T) {
	m := buildModel(t)
	stubUnknownInstaller(t)
	m.reportLaunchError(config.MissingToolError{Binary: "acme"}, nil)

	frame := ansi.Strip(m.viewLaunchHint())
	if strings.Contains(frame, "copy") {
		t.Fatalf("dialog should offer neither copy nor install:\n%s", frame)
	}
	// The first key settles the mouse hand-off; the keys under test come after.
	pressInLaunchHint(t, m, 'x')
	if cmd := pressInLaunchHint(t, m, 'i'); cmd != nil || m.mode != modeLaunchHint || m.install != nil {
		t.Fatalf("i must do nothing without a recipe: mode = %v, install = %+v", m.mode, m.install)
	}
	if cmd := pressInLaunchHint(t, m, 'c'); cmd != nil {
		t.Fatal("c must do nothing without a recipe")
	}
}

// stubUnknownInstaller empties PATH of every package manager deps knows,
// so an unknown tool has no install command on this machine.
func stubUnknownInstaller(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

// The dialog hands the mouse back to the terminal while it is up, so a
// drag over the install command selects it, and takes it back on close.
func TestLaunchHintReleasesTheMouseWhileOpen(t *testing.T) {
	m := buildModel(t)
	m.reportLaunchError(config.MissingToolError{Binary: "claude"}, nil)

	opened := pressInLaunchHint(t, m, 'x')
	if opened == nil || fmt.Sprintf("%T", opened()) != fmt.Sprintf("%T", tea.DisableMouse()) {
		t.Fatalf("opening the dialog should release the mouse, got %v", opened)
	}
	if again := pressInLaunchHint(t, m, 'x'); again != nil {
		t.Fatalf("a key inside the dialog should not toggle the mouse again, got %v", again)
	}

	updated, closed := m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m = updated.(*Model)
	if m.mode != modeList {
		t.Fatalf("mode = %v, want modeList", m.mode)
	}
	if closed == nil || fmt.Sprintf("%T", closed()) != fmt.Sprintf("%T", tea.EnableMouseCellMotion()) {
		t.Fatalf("closing the dialog should take the mouse back, got %v", closed)
	}
}

func TestLaunchHintStrayKeysKeepTheDialog(t *testing.T) {
	m := buildModel(t)
	m.reportLaunchError(config.MissingToolError{Binary: "claude"}, nil)

	pressInLaunchHint(t, m, 'x')
	if m.mode != modeLaunchHint {
		t.Fatalf("mode = %v, want the dialog kept", m.mode)
	}
	pressInLaunchHint(t, m, 'q')
	if m.mode != modeList {
		t.Fatalf("mode = %v, want q to close", m.mode)
	}
}

// installFixture opens the dialog on a fake CLI whose install command is
// the given shell line, holding one image the refused prompt named.
func installFixture(t *testing.T, m *Model, command string) (retried *int, image string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("install command is a shell line")
	}
	retried = new(int)
	image = tempImage(t, "mock.png")
	m.launchFix = launchFix{
		text:    "am-fake-cli is not installed.\n\ninstall it with: " + command,
		command: command,
		binary:  "am-fake-cli",
		retry: func() error {
			*retried++
			return nil
		},
		images: []imageAttachment{{id: 1, path: image}},
	}
	m.mode = modeLaunchHint
	return retried, image
}

func imageExists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("stat %s: %v", path, err)
	}
	return err == nil
}

func fakeInstallCommand(t *testing.T) string {
	t.Helper()
	bin := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	target := filepath.Join(bin, "am-fake-cli")
	return "printf '#!/bin/sh\\nexec cat\\n' > " + target + " && chmod +x " + target
}

func waitForInstallToSettle(t *testing.T, m *Model) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for m.install != nil {
		if time.Now().After(deadline) {
			t.Fatalf("install never settled, status = %q", m.errBar.text)
		}
		time.Sleep(100 * time.Millisecond)
		m.applyCmd(t, m.refreshCmd())
	}
}

func TestLaunchHintInstallRunsTheCommandAndRetriesTheLaunch(t *testing.T) {
	m := buildModel(t)
	retried, image := installFixture(t, m, fakeInstallCommand(t))

	cmd := pressInLaunchHint(t, m, 'i')

	if m.mode != modeList {
		t.Fatalf("mode = %v, err = %q, want the dialog closed once the install runs", m.mode, m.errBar.text)
	}
	shell := terminalSession(t, m)
	if shell.Name != "install-am-fake-cli" {
		t.Fatalf("install shell named %q", shell.Name)
	}
	if !m.tmux.Exists(shell.ID) {
		t.Fatal("install shell has no tmux session")
	}
	if row, ok := m.selected(); !ok || row.ID != shell.ID {
		t.Fatalf("cursor should land on the install shell, selected = %+v", row)
	}
	m.applyCmd(t, cmd)
	waitForInstallToSettle(t, m)

	if *retried != 1 {
		t.Fatalf("retried %d times, want the refused launch run once", *retried)
	}
	if !strings.Contains(m.errBar.text, "installed") || !m.errBar.worked() {
		t.Fatalf("status = %q, want the install reported done", m.errBar.text)
	}
	if !m.tmux.Exists(shell.ID) {
		t.Fatal("the install shell should stay open with its output")
	}
	if !imageExists(t, image) {
		t.Fatal("the launched prompt names the image, so its file must survive")
	}
}

func TestLaunchHintInstallFailureKeepsTheShellAndReportsTheStatus(t *testing.T) {
	m := buildModel(t)
	retried, image := installFixture(t, m, "exit 3")

	m.applyCmd(t, pressInLaunchHint(t, m, 'i'))
	shell := terminalSession(t, m)
	waitForInstallToSettle(t, m)

	if *retried != 0 {
		t.Fatalf("retried %d times, want none after a failed install", *retried)
	}
	if !strings.Contains(m.errBar.text, "status 3") || !strings.Contains(m.errBar.text, shell.Name) || m.errBar.worked() {
		t.Fatalf("status = %q, want the exit status and the shell to read it in", m.errBar.text)
	}
	if !m.tmux.Exists(shell.ID) {
		t.Fatal("a failed install must leave its shell open")
	}
	if imageExists(t, image) {
		t.Fatal("a failed install gives the prompt up, so its image goes too")
	}
}

func TestLaunchHintInstallThatLeavesTheBinaryOffPathIsReported(t *testing.T) {
	m := buildModel(t)
	retried, _ := installFixture(t, m, "true")

	m.applyCmd(t, pressInLaunchHint(t, m, 'i'))
	waitForInstallToSettle(t, m)

	if *retried != 0 {
		t.Fatalf("retried %d times, want none while the binary is still missing", *retried)
	}
	if !strings.Contains(m.errBar.text, "still not on PATH") {
		t.Fatalf("status = %q, want the PATH problem named", m.errBar.text)
	}
}

// Killing the install shell before it finishes drops the pending launch
// instead of holding it forever.
func TestLaunchHintInstallShellKilledDropsThePendingLaunch(t *testing.T) {
	m := buildModel(t)
	retried, image := installFixture(t, m, "sleep 30")

	m.applyCmd(t, pressInLaunchHint(t, m, 'i'))
	shell := terminalSession(t, m)
	if err := m.tmux.Kill(shell.ID); err != nil {
		t.Fatal(err)
	}
	waitForInstallToSettle(t, m)

	if *retried != 0 {
		t.Fatalf("retried %d times, want none", *retried)
	}
	if imageExists(t, image) {
		t.Fatal("a killed install gives the prompt up, so its image goes too")
	}
}

// A CLI on the interop PATH but not in the distro is a different problem
// from one nobody installed, and the dialog has to say so.
func TestLaunchHintNamesAWindowsOnlyInstall(t *testing.T) {
	m := buildModel(t)
	windowsPath := `/mnt/c/npm-global/claude`

	m.reportLaunchError(config.MissingToolError{Binary: "claude", WindowsPath: windowsPath}, nil)

	frame := ansi.Strip(m.viewLaunchHint())
	for _, want := range []string{"installed on Windows", "WSL distro", "claude.ai/install.sh"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("dialog is missing %q:\n%s", want, frame)
		}
	}
	// The path wraps in the card, so the dialog is checked for it before
	// the frame folds the line.
	if !strings.Contains(m.launchFix.text, windowsPath) {
		t.Fatalf("dialog text %q should name the Windows copy", m.launchFix.text)
	}
	if m.launchFix.command != deps.Command("claude") {
		t.Fatalf("command = %q, want the Linux installer", m.launchFix.command)
	}
}

// Two installs at once would leave the first one's launch unfinished, so
// the second press says where the running one is instead.
func TestLaunchHintRefusesASecondInstall(t *testing.T) {
	m := buildModel(t)
	installFixture(t, m, "sleep 30")
	m.applyCmd(t, pressInLaunchHint(t, m, 'i'))
	running := terminalSession(t, m)

	installFixture(t, m, "sleep 30")
	pressInLaunchHint(t, m, 'i')

	if m.install == nil || m.install.sessionID != running.ID {
		t.Fatalf("install = %+v, want the first one kept", m.install)
	}
	if !strings.Contains(m.errBar.text, running.Name) {
		t.Fatalf("status = %q, want the running install named", m.errBar.text)
	}
	if shells := shellCount(m); shells != 1 {
		t.Fatalf("%d install shells, want 1", shells)
	}
}

// The dialog holds the keyboard, so the quit key has to keep working
// there the way it does over the key map.
func TestLaunchHintQuitsOnCtrlC(t *testing.T) {
	m := buildModel(t)
	m.reportLaunchError(config.MissingToolError{Binary: "claude"}, nil)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = updated.(*Model)
	if cmd == nil {
		t.Fatal("ctrl+c in the dialog should quit")
	}
	// The quit rides in a batch with the mouse hand-off the dialog does
	// on its way up.
	if !quits(cmd()) {
		t.Fatalf("ctrl+c produced %T, want a quit", cmd())
	}
}

func quits(msg tea.Msg) bool {
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		_, isQuit := msg.(tea.QuitMsg)
		return isQuit
	}
	for _, part := range batch {
		if quits(part()) {
			return true
		}
	}
	return false
}

// A retry that stops on the next missing piece hands the dialog back the
// images, so one more install can still spawn the same prompt.
func TestLaunchHintRetryThatStopsAgainKeepsTheImages(t *testing.T) {
	m := buildModel(t)
	_, image := installFixture(t, m, fakeInstallCommand(t))
	m.launchFix.retry = func() error { return config.MissingToolError{Binary: "tmux"} }

	m.applyCmd(t, pressInLaunchHint(t, m, 'i'))
	waitForInstallToSettle(t, m)

	if m.mode != modeLaunchHint {
		t.Fatalf("mode = %v, err = %q, want the dialog on the next missing tool", m.mode, m.errBar.text)
	}
	if len(m.launchFix.images) != 1 {
		t.Fatalf("dialog images = %+v, want the prompt's image still held", m.launchFix.images)
	}
	if !imageExists(t, image) {
		t.Fatal("a prompt the dialog can still spawn keeps its image")
	}
}

// A retry that fails into the status bar has no dialog left to hold the
// prompt, so its images go with it.
func TestLaunchHintRetryThatFailsOutrightDropsTheImages(t *testing.T) {
	m := buildModel(t)
	_, image := installFixture(t, m, fakeInstallCommand(t))
	m.launchFix.retry = func() error { return errors.New("tmux create: boom") }

	m.applyCmd(t, pressInLaunchHint(t, m, 'i'))
	waitForInstallToSettle(t, m)

	if m.mode != modeList {
		t.Fatalf("mode = %v, want the list", m.mode)
	}
	if m.errBar.text != "tmux create: boom" {
		t.Fatalf("status = %q", m.errBar.text)
	}
	if imageExists(t, image) {
		t.Fatal("nothing can spawn that prompt now, so its image goes too")
	}
}

// The install runs from a script the manager writes, so the pane's shell
// is typed one short line and the installer's own quoting reaches sh
// untouched. Both files are gone once the install has settled.
func TestLaunchHintInstallRunsFromAScriptAndCleansUp(t *testing.T) {
	m := buildModel(t)
	command := `printf '%s\n' "one '\'' two" > /dev/null`
	installFixture(t, m, command)

	m.applyCmd(t, pressInLaunchHint(t, m, 'i'))
	shell := terminalSession(t, m)
	script := m.install.script
	body, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), command) {
		t.Fatalf("script does not carry the command verbatim:\n%s", body)
	}
	waitForInstallToSettle(t, m)

	if !strings.Contains(m.errBar.text, "not on PATH") {
		t.Fatalf("status = %q, want the command reported as run", m.errBar.text)
	}
	for _, path := range []string{script, m.hooks.InstallStatusFile(shell.ID)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s should be gone, stat err = %v", path, err)
		}
	}
}
