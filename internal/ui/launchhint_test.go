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
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// fakeLaunchHintHost stands in for the root's composers, effect lane,
// status bar and card chrome.
type fakeLaunchHintHost struct {
	mode     mode
	gens     int
	images   []imageAttachment
	err      string
	admit    bool
	installs []installStartRequest
}

func (h *fakeLaunchHintHost) quit() tea.Cmd     { return tea.Quit }
func (h *fakeLaunchHintHost) setMode(next mode) { h.mode = next }
func (h *fakeLaunchHintHost) advanceDialogGen() { h.gens++ }
func (h *fakeLaunchHintHost) takeComposerImages() []imageAttachment {
	images := h.images
	h.images = nil
	return images
}
func (h *fakeLaunchHintHost) reportErr(text string) { h.err = text }
func (h *fakeLaunchHintHost) startInstall(request installStartRequest) bool {
	h.installs = append(h.installs, request)
	return h.admit
}
func (h *fakeLaunchHintHost) cardWidth() int { return 40 }
func (h *fakeLaunchHintHost) cardSized(width int, title, body string, hint [][2]string) string {
	return title + "\n" + body
}

func TestLaunchHintDialogWithFakeHost(t *testing.T) {
	h := &fakeLaunchHintHost{images: []imageAttachment{{id: 1}}}
	var d launchHintDialog
	d.open(h, launchFix{text: "tool is not installed.\n\nRun the installer.", command: "install tool", binary: "tool"})
	if h.mode != modeLaunchHint || h.gens != 1 || len(d.fix.images) != 1 || h.images != nil {
		t.Fatalf("open: mode %v gens %d fix %+v, want the composer images taken", h.mode, h.gens, d.fix)
	}
	if got := d.view(h); !strings.Contains(got, "Session needs a setup step") || !strings.Contains(got, "Run the installer.") {
		t.Fatalf("view = %q", got)
	}

	d.handleKey(h, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	if len(h.installs) != 1 || h.installs[0].command != "install tool" || len(d.fix.images) != 1 {
		t.Fatalf("installs %+v images %d, want a refused start to keep the images", h.installs, len(d.fix.images))
	}
	h.admit = true
	d.handleKey(h, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	if len(h.installs) != 2 || len(h.installs[1].images) != 1 || d.fix.images != nil {
		t.Fatalf("installs %+v images %v, want the images handed to the admitted install", h.installs, d.fix.images)
	}
	d.install = &pendingInstall{name: "install-tool"}
	d.handleKey(h, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	if len(h.installs) != 2 || h.err != "an install is already running in install-tool" {
		t.Fatalf("installs %d err %q, want a running install refused", len(h.installs), h.err)
	}

	d.handleKey(h, tea.KeyMsg{Type: tea.KeyEsc})
	if h.mode != modeList || d.fix.command != "" {
		t.Fatalf("esc: mode %v fix %+v", h.mode, d.fix)
	}
}

func TestReportLaunchErrorOpensInstallHintForHermes(t *testing.T) {
	m := buildModel(t)
	want := "'/opt/hermes/libexec/bin/python3' -m pip install mcp"

	m.reportLaunchError(fmt.Errorf("launch: %w", mcpreg.HermesMCPUnavailableError{PipCommand: want}))

	if m.mode != modeLaunchHint {
		t.Fatalf("mode = %v, want modeLaunchHint", m.mode)
	}
	if m.launchHint.fix.command != want {
		t.Fatalf("command = %q, want %q", m.launchHint.fix.command, want)
	}
	if !strings.Contains(m.launchHint.fix.text, want) {
		t.Fatalf("hint %q should name the install command", m.launchHint.fix.text)
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m.drainEffects(t)
	m = updated.(*Model)
	if m.mode != modeList {
		t.Fatalf("after esc, mode = %v, want modeList", m.mode)
	}
	if m.launchHint.fix.text != "" {
		t.Fatalf("dismiss should clear the hint, got %q", m.launchHint.fix.text)
	}
}

func TestReportLaunchErrorLeavesHermesHintReadOnlyWithoutAnInterpreter(t *testing.T) {
	m := buildModel(t)

	m.reportLaunchError(fmt.Errorf("launch: %w", mcpreg.HermesMCPUnavailableError{}))

	if m.mode != modeLaunchHint {
		t.Fatalf("mode = %v, want modeLaunchHint", m.mode)
	}
	if m.launchHint.fix.command != "" {
		t.Fatalf("command = %q, want none when the interpreter cannot be resolved", m.launchHint.fix.command)
	}
	if !strings.Contains(m.launchHint.fix.text, "mcp package") {
		t.Fatalf("hint %q should still name what has to be installed", m.launchHint.fix.text)
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m.drainEffects(t)
	m = updated.(*Model)
	if m.launchHint.install != nil || m.mode != modeLaunchHint {
		t.Fatalf("a read-only dialog should run nothing on i: install = %v, mode = %v", m.launchHint.install, m.mode)
	}
}

func TestReportLaunchErrorOpensInstallHintForMissingCLI(t *testing.T) {
	m := buildModel(t)

	m.reportLaunchError(config.MissingToolError{Binary: "claude"})

	if m.mode != modeLaunchHint {
		t.Fatalf("mode = %v, want modeLaunchHint", m.mode)
	}
	if !strings.Contains(m.launchHint.fix.text, "claude.ai/install.sh") {
		t.Fatalf("hint %q should name the install command", m.launchHint.fix.text)
	}
	if !strings.Contains(m.launchHint.fix.text, "claude") {
		t.Fatalf("hint %q should name the missing CLI", m.launchHint.fix.text)
	}
	frame := ansi.Strip(m.launchHint.view(m))
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
	if !strings.Contains(m.launchHint.fix.text, "acme") {
		t.Fatalf("hint %q should name the missing CLI", m.launchHint.fix.text)
	}
	if !strings.Contains(m.launchHint.fix.text, "install") {
		t.Fatalf("hint %q should name how to install", m.launchHint.fix.text)
	}
}

func TestSpawnMissingCLIPromptsInstall(t *testing.T) {
	m := buildModel(t)
	m.services.cfg.Tools["claude"] = config.Tool{Command: "am-missing-cli-xyz", DefaultStatus: status.Idle}

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
	_, cmd := m.submitForm()
	m.applyCmd(t, cmd)

	if m.mode != modeLaunchHint {
		t.Fatalf("mode = %v, err = %q, want modeLaunchHint", m.mode, m.errBar.text)
	}
	if !strings.Contains(m.launchHint.fix.text, "am-missing-cli-xyz") {
		t.Fatalf("hint %q should name the missing binary", m.launchHint.fix.text)
	}
	if len(m.sessionRows()) != 0 {
		t.Fatalf("no session may spawn without the CLI, got %v", sessionNames(m))
	}
}

func TestReviveMissingCLIPromptsInstall(t *testing.T) {
	m := buildModel(t)
	sess := store.Session{ID: newID(), Name: "agent", Tool: "claude", Cwd: t.TempDir()}
	if err := m.services.store.CreateSession(sess); err != nil {
		t.Fatal(err)
	}
	m.workspace.sessions = []store.Session{sess}
	m.services.cfg.Tools["claude"] = config.Tool{
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
	if !strings.Contains(m.launchHint.fix.text, "am-missing-cli-xyz") {
		t.Fatalf("hint %q should name the revive binary", m.launchHint.fix.text)
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
	m.services.cfg.Tools["hermes"] = config.Tool{Command: "cat", DefaultStatus: status.Idle}
	installSDKlessHermes(t)
	sess := store.Session{ID: newID(), Name: "agent", Tool: "hermes", Cwd: t.TempDir()}
	m.confirm.confirmTarget = confirmTarget{action: actionRestart, sessions: []store.Session{sess}}
	m.mode = modeConfirmDelete

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.drainEffects(t)
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
	m.services.cfg.Tools["hermes"] = config.Tool{Command: "cat", DefaultStatus: status.Idle}
	installSDKlessHermes(t)
	if err := m.services.store.CreateGroup("backend", t.TempDir()); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "backend")
	m.openQuickMode()
	m.quick.toolNames = []string{"hermes"}

	updated, cmd := m.quickSpawn("backend", "fix the tests")
	m = updated.(*Model)
	m.applyCmd(t, cmd)

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
	m.services.cfg.Tools["hermes"] = config.Tool{Command: "cat", DefaultStatus: status.Idle}
	installSDKlessHermes(t)

	m.openForm()
	m.form.name.SetValue("agent")
	m.form.dir.SetValue(t.TempDir())
	m.form.toolNames = []string{"hermes"}
	m.form.toolIndex = 0
	path := tempImage(t, "mock.png")
	m.form.prompt.attachments = []imageAttachment{{id: 1, path: path}}
	m.form.prompt.input.SetValue("match " + imageToken(1))

	_, cmd := m.submitForm()
	m.applyCmd(t, cmd)

	if m.mode != modeLaunchHint {
		t.Fatalf("mode = %v, err = %q, want modeLaunchHint", m.mode, m.errBar.text)
	}
	// The dialog can still install the CLI and spawn this prompt, so it
	// takes the images over from the form and keeps the files until it
	// closes.
	if len(m.form.prompt.attachments) != 0 {
		t.Fatalf("attachments = %+v, want the form's images handed to the dialog", m.form.prompt.attachments)
	}
	if len(m.launchHint.fix.images) != 1 {
		t.Fatalf("dialog images = %+v, want the refused prompt's image", m.launchHint.fix.images)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the image file must outlive the refusal, stat err = %v", err)
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m.drainEffects(t)
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
	m.applyTestMsg(t, m.formWorktreeProbeCmd(false)())
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

	_, cmd := m.submitForm()
	m.applyCmd(t, cmd)

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
	m.services.cfg.Tools["hermes"] = config.Tool{Command: "cat", DefaultStatus: status.Idle}
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
	_, cmd := m.submitForm()
	m.applyCmd(t, cmd)

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
	m.drainEffects(t)
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
	m.reportLaunchError(config.MissingToolError{Binary: "claude"})

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
	m.reportLaunchError(config.MissingToolError{Binary: "claude"})

	m.runBatch(t, pressInLaunchHint(t, m, 'c'))

	if !strings.Contains(m.errBar.text, "no clipboard backend") || m.errBar.worked() {
		t.Fatalf("status = %q, want the copy failure", m.errBar.text)
	}
}

// A tool with no known recipe gets a read-only dialog: nothing to copy,
// nothing to run.
func TestLaunchHintWithoutARecipeOffersOnlyClose(t *testing.T) {
	m := buildModel(t)
	m.reportLaunchError(config.MissingToolError{Binary: "acme"})

	frame := ansi.Strip(m.launchHint.view(m))
	if strings.Contains(frame, "copy") {
		t.Fatalf("dialog should offer neither copy nor install:\n%s", frame)
	}
	// The first key settles the mouse hand-off; the keys under test come after.
	pressInLaunchHint(t, m, 'x')
	if cmd := pressInLaunchHint(t, m, 'i'); cmd != nil || m.mode != modeLaunchHint || m.launchHint.install != nil {
		t.Fatalf("i must do nothing without a recipe: mode = %v, install = %+v", m.mode, m.launchHint.install)
	}
	if cmd := pressInLaunchHint(t, m, 'c'); cmd != nil {
		t.Fatal("c must do nothing without a recipe")
	}
}

// The dialog hands the mouse back to the terminal while it is up, so a
// drag over the install command selects it, and takes it back on close.
func TestLaunchHintReleasesTheMouseWhileOpen(t *testing.T) {
	m := buildModel(t)
	m.reportLaunchError(config.MissingToolError{Binary: "claude"})

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
	m.reportLaunchError(config.MissingToolError{Binary: "claude"})

	pressInLaunchHint(t, m, 'x')
	if m.mode != modeLaunchHint {
		t.Fatalf("mode = %v, want the dialog kept", m.mode)
	}
	pressInLaunchHint(t, m, 'q')
	if m.mode != modeList {
		t.Fatalf("mode = %v, want q to close", m.mode)
	}
}

const installRetryTool = "install-retry"

// installFixture opens the dialog on a fake CLI whose install command is
// the given shell line. Its captured retry is a real typed spawn request,
// holding the same image as the refused prompt.
func installFixture(t *testing.T, m *Model, command string) (retryName, image string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("install command is a shell line")
	}
	retryName = "retried-agent"
	image = tempImage(t, "mock.png")
	m.services.cfg.Tools[installRetryTool] = config.Tool{Command: "am-fake-cli", DefaultStatus: status.Idle}
	w, h := m.paneTargetSize()
	retry := spawnRequest{
		id:       newID(),
		kind:     spawnForm,
		toolName: installRetryTool,
		name:     retryName,
		dir:      t.TempDir(),
		pane:     sessioncmd.PaneSize{Width: w, Height: h},
		images:   []imageAttachment{{id: 1, path: image}},
	}
	m.launchHint.open(m, launchFix{
		text:        "am-fake-cli is not installed.\n\ninstall it with: " + command,
		command:     command,
		binary:      "am-fake-cli",
		effectRetry: retry,
		images:      []imageAttachment{{id: 1, path: image}},
	})
	return retryName, image
}

func hasSessionNamed(m *Model, name string) bool {
	for _, sess := range m.sessionRows() {
		if sess.Name == name {
			return true
		}
	}
	return false
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
	for m.launchHint.install != nil {
		if time.Now().After(deadline) {
			t.Fatalf("install never settled, status = %q", m.errBar.text)
		}
		time.Sleep(100 * time.Millisecond)
		m.applyCmd(t, m.refreshCmd())
	}
}

func TestLaunchHintInstallRunsTheCommandAndRetriesTheLaunch(t *testing.T) {
	m := buildModel(t)
	retryName, image := installFixture(t, m, fakeInstallCommand(t))

	cmd := pressInLaunchHint(t, m, 'i')

	if m.mode != modeLaunchHint || m.launchHint.install != nil {
		t.Fatalf("install ran on Update: mode=%v pending=%+v", m.mode, m.launchHint.install)
	}
	m.applyCmd(t, cmd)
	if m.mode != modeList {
		t.Fatalf("mode = %v, err = %q, want the dialog closed once the install starts", m.mode, m.errBar.text)
	}
	shell := terminalSession(t, m)
	if shell.Name != "install-am-fake-cli" {
		t.Fatalf("install shell named %q", shell.Name)
	}
	if !m.services.tmux.Exists(shell.ID) {
		t.Fatal("install shell has no tmux session")
	}
	if row, ok := m.selected(); !ok || row.ID != shell.ID {
		t.Fatalf("cursor should land on the install shell, selected = %+v", row)
	}
	waitForInstallToSettle(t, m)

	if !hasSessionNamed(m, retryName) {
		t.Fatalf("captured retry did not launch %q: %v", retryName, sessionNames(m))
	}
	if !strings.Contains(m.errBar.text, "installed") || !m.errBar.worked() {
		t.Fatalf("status = %q, want the install reported done", m.errBar.text)
	}
	if !m.services.tmux.Exists(shell.ID) {
		t.Fatal("the install shell should stay open with its output")
	}
	if !imageExists(t, image) {
		t.Fatal("the launched prompt names the image, so its file must survive")
	}
}

func TestLaunchHintInstallFailureKeepsTheShellAndReportsTheStatus(t *testing.T) {
	m := buildModel(t)
	retryName, image := installFixture(t, m, "exit 3")

	m.applyCmd(t, pressInLaunchHint(t, m, 'i'))
	shell := terminalSession(t, m)
	waitForInstallToSettle(t, m)

	if hasSessionNamed(m, retryName) {
		t.Fatalf("failed install launched its retry: %v", sessionNames(m))
	}
	if !strings.Contains(m.errBar.text, "status 3") || !strings.Contains(m.errBar.text, shell.Name) || m.errBar.worked() {
		t.Fatalf("status = %q, want the exit status and the shell to read it in", m.errBar.text)
	}
	if !m.services.tmux.Exists(shell.ID) {
		t.Fatal("a failed install must leave its shell open")
	}
	if imageExists(t, image) {
		t.Fatal("a failed install gives the prompt up, so its image goes too")
	}
}

func TestLaunchHintInstallThatLeavesTheBinaryOffPathIsReported(t *testing.T) {
	m := buildModel(t)
	retryName, _ := installFixture(t, m, "true")

	m.applyCmd(t, pressInLaunchHint(t, m, 'i'))
	waitForInstallToSettle(t, m)

	if hasSessionNamed(m, retryName) {
		t.Fatalf("missing binary launched its retry: %v", sessionNames(m))
	}
	if !strings.Contains(m.errBar.text, "still not on PATH") {
		t.Fatalf("status = %q, want the PATH problem named", m.errBar.text)
	}
}

// Killing the install shell before it finishes drops the pending launch
// instead of holding it forever.
func TestLaunchHintInstallShellKilledDropsThePendingLaunch(t *testing.T) {
	m := buildModel(t)
	retryName, image := installFixture(t, m, "sleep 30")

	m.applyCmd(t, pressInLaunchHint(t, m, 'i'))
	shell := terminalSession(t, m)
	if err := m.services.tmux.Kill(shell.ID); err != nil {
		t.Fatal(err)
	}
	waitForInstallToSettle(t, m)

	if hasSessionNamed(m, retryName) {
		t.Fatalf("killed install launched its retry: %v", sessionNames(m))
	}
	if imageExists(t, image) {
		t.Fatal("a killed install gives the prompt up, so its image goes too")
	}
	_, quit := m.requestQuit()
	if quit == nil || !m.effects.quitting {
		t.Fatal("killing the tracked installer should make quit available again")
	}
}

// A CLI on the interop PATH but not in the distro is a different problem
// from one nobody installed, and the dialog has to say so.
func TestLaunchHintNamesAWindowsOnlyInstall(t *testing.T) {
	m := buildModel(t)
	windowsPath := `/mnt/c/npm-global/claude`

	m.reportLaunchError(config.MissingToolError{Binary: "claude", WindowsPath: windowsPath})

	frame := ansi.Strip(m.launchHint.view(m))
	for _, want := range []string{"installed on Windows", "WSL distro", "claude.ai/install.sh"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("dialog is missing %q:\n%s", want, frame)
		}
	}
	// The path wraps in the card, so the dialog is checked for it before
	// the frame folds the line.
	if !strings.Contains(m.launchHint.fix.text, windowsPath) {
		t.Fatalf("dialog text %q should name the Windows copy", m.launchHint.fix.text)
	}
	if m.launchHint.fix.command != deps.Command("claude") {
		t.Fatalf("command = %q, want the Linux installer", m.launchHint.fix.command)
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

	if m.launchHint.install == nil || m.launchHint.install.sessionID != running.ID {
		t.Fatalf("install = %+v, want the first one kept", m.launchHint.install)
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
	m.reportLaunchError(config.MissingToolError{Binary: "claude"})

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
	tool := m.services.cfg.Tools[installRetryTool]
	tool.Command = "am-next-missing-cli"
	m.services.cfg.Tools[installRetryTool] = tool

	m.applyCmd(t, pressInLaunchHint(t, m, 'i'))
	waitForInstallToSettle(t, m)

	if m.mode != modeLaunchHint {
		t.Fatalf("mode = %v, err = %q, want the dialog on the next missing tool", m.mode, m.errBar.text)
	}
	if len(m.launchHint.fix.images) != 1 {
		t.Fatalf("dialog images = %+v, want the prompt's image still held", m.launchHint.fix.images)
	}
	if !imageExists(t, image) {
		t.Fatal("a prompt the dialog can still spawn keeps its image")
	}
}

// An image pasted into the form while the install ran is on the dialog the
// stopped retry opens, alongside the ones that dialog was already holding.
func TestLaunchHintRetryThatStopsAgainKeepsAPastedImage(t *testing.T) {
	m := buildModel(t)
	_, image := installFixture(t, m, fakeInstallCommand(t))
	tool := m.services.cfg.Tools[installRetryTool]
	tool.Command = "am-next-missing-cli"
	m.services.cfg.Tools[installRetryTool] = tool

	pasted := tempImage(t, "pasted.png")
	m.form.prompt.attachments = []imageAttachment{{id: 2, path: pasted}}
	m.applyCmd(t, pressInLaunchHint(t, m, 'i'))
	waitForInstallToSettle(t, m)

	if len(m.launchHint.fix.images) != 2 {
		t.Fatalf("dialog images = %+v, want the refused prompt's image and the pasted one", m.launchHint.fix.images)
	}
	if !imageExists(t, image) || !imageExists(t, pasted) {
		t.Fatal("both images stay for a prompt the dialog can still spawn")
	}
}

// A retry that fails into the status bar has no dialog left to hold the
// prompt, so its images go with it.
func TestLaunchHintRetryThatFailsOutrightDropsTheImages(t *testing.T) {
	m := buildModel(t)
	_, image := installFixture(t, m, fakeInstallCommand(t))

	m.applyCmd(t, pressInLaunchHint(t, m, 'i'))
	// The accepted install already captured its lifecycle. Removing the
	// live service now makes only the later typed retry fail outright.
	m.services.lifecycle = nil
	waitForInstallToSettle(t, m)

	if m.mode != modeList {
		t.Fatalf("mode = %v, want the list", m.mode)
	}
	if m.errBar.text == "" {
		t.Fatal("failed retry should report its launch error")
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

	cmd := pressInLaunchHint(t, m, 'i')
	m.applyCmd(t, cmd)
	// Read the script before the refresh runs: a command this short can
	// have settled by then, and settling removes the file.
	script := m.launchHint.install.script
	statusFile := m.launchHint.install.statusFile
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
	for _, path := range []string{script, statusFile} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s should be gone, stat err = %v", path, err)
		}
	}
}

// A restore the manager refused for a missing CLI has to finish the
// restore when the install unblocks it: a session brought back but left
// filed as archived is invisible in the list it returned to.
func TestInstallFinishesARefusedRestore(t *testing.T) {
	m := buildModel(t)
	sess := store.Session{ID: newID(), Name: "agent", Tool: "claude", Cwd: t.TempDir(), Archived: true}
	if err := m.services.store.CreateSession(sess); err != nil {
		t.Fatal(err)
	}
	if err := m.services.store.SetArchived(sess.ID, true); err != nil {
		t.Fatal(err)
	}
	m.services.cfg.Tools["claude"] = config.Tool{Command: "am-missing-cli-xyz", DefaultStatus: status.Idle}
	m.confirm.confirmTarget = confirmTarget{action: actionRestore, sessions: []store.Session{sess}}
	m.mode = modeConfirmDelete

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.drainEffects(t)
	m = updated.(*Model)
	if m.mode != modeLaunchHint {
		t.Fatalf("mode = %v, err = %q, want the dialog", m.mode, m.errBar.text)
	}

	// What the install unblocks has to be the whole restore, not the
	// revive alone, so the retry is run here with a working CLI.
	m.services.cfg.Tools["claude"] = config.Tool{Command: "cat", DefaultStatus: status.Idle}
	if m.launchHint.fix.effectRetry == nil {
		t.Fatal("missing captured restore retry")
	}
	m.enqueueEffect(m.launchHint.fix.effectRetry, 0, false)
	m.drainEffects(t)

	restored, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Archived {
		t.Fatal("the revived session is still filed as archived")
	}
	if !m.services.tmux.Exists(sess.ID) {
		t.Fatal("the session should be running again")
	}
}
