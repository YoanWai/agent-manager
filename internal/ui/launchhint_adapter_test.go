package ui

import (
	"errors"
	"fmt"
	uilifecycle "github.com/YoanWai/agent-manager/internal/ui/session/lifecycle"
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
	"github.com/YoanWai/agent-manager/internal/testenv"
	"github.com/YoanWai/agent-manager/internal/testfixture"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/YoanWai/agent-manager/internal/ui/composer"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uiinstall "github.com/YoanWai/agent-manager/internal/ui/launchhint/install"
	uilaunch "github.com/YoanWai/agent-manager/internal/ui/spawn/launch"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// hintHost is the root with the one reach a test wants to see: the install
// a key asks for. The rest is the real model, so the card is driven the way
// the stack drives it.
type hintHost struct {
	*Model
	admit    bool
	installs []uiinstall.StartRequest
}

func (h *hintHost) StartInstall() bool {
	if install := h.ledger.install.Pending; install != nil {
		h.reportErr("an install is already running in " + install.Name)
		return false
	}
	h.installs = append(h.installs, h.launchInstall())
	if !h.admit {
		return false
	}
	h.ledger.install.Launch.Images = nil
	return true
}

func (h *hintHost) CopyLaunchCommand() tea.Cmd { return nil }

func (h *hintHost) CardWidth() int { return h.cardWidth() }

func (h *hintHost) CardChromeRows(hint [][2]string) int { return h.cardChromeRows(hint) }

func (h *hintHost) ReleaseLaunchHint() { h.releaseLaunchHint() }

func TestLaunchHintSurfaceWithFakeInstall(t *testing.T) {
	m := buildModel(t)
	m.layout.width, m.layout.height = 110, 34
	pushQuick(m).Attachments = []composer.Attachment{{ID: 1}}
	h := &hintHost{Model: m}
	m.openLaunchHint(uiinstall.Fix{Text: "tool is not installed.\n\nRun the installer.", Command: "install tool", Script: "install tool", Binary: "tool"})
	_, ok := surfaceOf[*uidialogs.LaunchHintSurface](m)
	if !ok || m.gens.dialog != 1 || len(m.ledger.install.Launch.Images) != 1 || m.quick().Attachments != nil {
		t.Fatalf("open: surface %t gens %d fix %+v, want the composer images taken", ok, m.gens.dialog, hintFix(m))
	}
	// The card calls the root it was built with, so the fake is that root.
	s := uidialogs.NewLaunchHintSurface(h, m.launchHintCard(m.ledger.install.Launch), m.glyphs(), uitheme.MutedStyle(), uitheme.MutedStyle())
	m.surfaces[len(m.surfaces)-1] = s
	if got := ansi.Strip(s.View(h, "")); !strings.Contains(got, "Session needs a setup step") || !strings.Contains(got, "Run the installer.") {
		t.Fatalf("view = %q", got)
	}

	s.Key(h, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	if len(h.installs) != 1 || h.installs[0].Command != "install tool" || len(m.ledger.install.Launch.Images) != 1 {
		t.Fatalf("installs %+v images %d, want a refused start to keep the images", h.installs, len(m.ledger.install.Launch.Images))
	}
	h.admit = true
	s.Key(h, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	if len(h.installs) != 2 || len(h.installs[1].Images) != 1 || m.ledger.install.Launch.Images != nil {
		t.Fatalf("installs %+v images %v, want the images handed to the admitted install", h.installs, m.ledger.install.Launch.Images)
	}
	m.ledger.install.Pending = &uiinstall.Pending{Name: "install-tool"}
	s.Key(h, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	if len(h.installs) != 2 || m.errBar.text != "an install is already running in install-tool" {
		t.Fatalf("installs %d err %q, want a running install refused", len(h.installs), m.errBar.text)
	}

	s.Key(h, tea.KeyMsg{Type: tea.KeyEsc})
	if hintOpen(m) || !m.onList() || m.ledger.install.Launch.Command != "" {
		t.Fatalf("esc: open %t mode %v fix %+v", hintOpen(m), m.screenID(), hintFix(m))
	}
}

func TestReportLaunchErrorOpensInstallHintForHermes(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	want := "'/opt/hermes/libexec/bin/python3' -m pip install mcp"

	m.reportLaunchError(fmt.Errorf("launch: %w", mcpreg.HermesMCPUnavailableError{PipCommand: want}))

	if !hintOpen(m) {
		t.Fatalf("screen = %v, want the launch hint", m.screenID())
	}
	if hintFix(m).Command != want {
		t.Fatalf("command = %q, want %q", hintFix(m).Command, want)
	}
	if !strings.Contains(hintFix(m).Text, want) {
		t.Fatalf("hint %q should name the install command", hintFix(m).Text)
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m.drainEffects(t)
	m = updated.(*Model)
	if !m.onList() {
		t.Fatalf("after esc, screen = %v, want the list", m.screenID())
	}
	if hintFix(m).Text != "" {
		t.Fatalf("dismiss should clear the hint, got %q", hintFix(m).Text)
	}
}

func TestReportLaunchErrorLeavesHermesHintReadOnlyWithoutAnInterpreter(t *testing.T) {
	t.Parallel()
	m := buildModel(t)

	m.reportLaunchError(fmt.Errorf("launch: %w", mcpreg.HermesMCPUnavailableError{}))

	if !hintOpen(m) {
		t.Fatalf("screen = %v, want the launch hint", m.screenID())
	}
	if hintFix(m).Command != "" {
		t.Fatalf("command = %q, want none when the interpreter cannot be resolved", hintFix(m).Command)
	}
	if !strings.Contains(hintFix(m).Text, "mcp package") {
		t.Fatalf("hint %q should still name what has to be installed", hintFix(m).Text)
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m.drainEffects(t)
	m = updated.(*Model)
	if m.ledger.install.Pending != nil || !hintOpen(m) {
		t.Fatalf("a read-only dialog should run nothing on i: install = %v, screen = %v", m.ledger.install.Pending, m.screenID())
	}
}

func TestReportLaunchErrorOpensInstallHintForMissingCLI(t *testing.T) {
	t.Parallel()
	m := buildModel(t)

	m.reportLaunchError(config.MissingToolError{Binary: "claude"})

	if !hintOpen(m) {
		t.Fatalf("screen = %v, want the launch hint", m.screenID())
	}
	installer := "claude.ai/install.sh"
	if runtime.GOOS == "windows" {
		installer = "claude.ai/install.ps1"
	}
	if !strings.Contains(hintFix(m).Text, installer) {
		t.Fatalf("hint %q should name the install command", hintFix(m).Text)
	}
	if !strings.Contains(hintFix(m).Text, "claude") {
		t.Fatalf("hint %q should name the missing CLI", hintFix(m).Text)
	}
	frame := ansi.Strip(preparedView(m))
	if !strings.Contains(frame, installer) {
		t.Fatalf("dialog should show the install command:\n%s", frame)
	}
}

func TestReportLaunchErrorOpensHintForOutdatedCLI(t *testing.T) {
	t.Parallel()
	m := buildModel(t)

	m.reportLaunchError(fmt.Errorf("spawn: %w", config.OutdatedToolError{Binary: "pi", Installed: "0.76.0", Minimum: "0.84.3"}))

	if !hintOpen(m) {
		t.Fatalf("screen = %v, want the launch hint", m.screenID())
	}
	if hintFix(m).Command != deps.Command("pi") || hintFix(m).Binary != "pi" {
		t.Fatalf("fix = %+v, want pi's installer to rerun", hintFix(m))
	}
	installer := "pi.dev/install.sh"
	if runtime.GOOS == "windows" {
		installer = "@earendil-works/pi-coding-agent"
	}
	if !strings.Contains(hintFix(m).Text, installer) {
		t.Fatalf("dialog text %q should name the installer %q", hintFix(m).Text, installer)
	}
	frame := ansi.Strip(preparedView(m))
	for _, want := range []string{"pi 0.76.0 is older than 0.84.3", "i update"} {
		if !strings.Contains(strings.Join(strings.Fields(frame), " "), want) {
			t.Fatalf("dialog should show %q:\n%s", want, frame)
		}
	}
	got := map[string]bool{}
	for _, hit := range m.layout.keyHits {
		if hit.Key.Type == tea.KeyEsc {
			got["esc"] = true
		}
		if len(hit.Key.Runes) == 1 {
			got[string(hit.Key.Runes)] = true
		}
	}
	for _, key := range []string{"i", "c", "esc"} {
		if !got[key] {
			t.Fatalf("dialog foot is missing %q", key)
		}
	}
}

func TestLaunchHintKeepsTheInstallLineAndTheErrorOnAShortTerminal(t *testing.T) {
	m := buildModel(t)
	m.layout.width = 110
	m.layout.height = 30
	m.reportLaunchError(config.MissingToolError{Binary: "cmd"})
	m.reportErr("mkdir /hooks: not a directory")

	frame := ansi.Strip(preparedView(m))
	if rows := strings.Count(frame, "\n") + 1; rows > m.layout.height {
		t.Fatalf("dialog is %d rows on a %d-row terminal\n%s", rows, m.layout.height, frame)
	}
	if !strings.Contains(frame, "npm install -g command-code") {
		t.Fatalf("install line is off screen:\n%s", frame)
	}
	if !strings.Contains(frame, "mkdir /hooks: not a directory") {
		t.Fatalf("error is off screen:\n%s", frame)
	}
	if !strings.Contains(frame, "close") {
		t.Fatalf("keys are off screen:\n%s", frame)
	}
	if runtime.GOOS != "windows" && !strings.Contains(hintFix(m).Script, "install_cli") {
		t.Fatal("install lost the script")
	}
}

func TestReportLaunchErrorOpensHintForUnknownMissingCLI(t *testing.T) {
	t.Parallel()
	m := buildModel(t)

	m.reportLaunchError(config.MissingToolError{Binary: "acme"})

	if !hintOpen(m) {
		t.Fatalf("screen = %v, want the launch hint", m.screenID())
	}
	if !strings.Contains(hintFix(m).Text, "acme") {
		t.Fatalf("hint %q should name the missing CLI", hintFix(m).Text)
	}
	if !strings.Contains(hintFix(m).Text, "install") {
		t.Fatalf("hint %q should name how to install", hintFix(m).Text)
	}
}

func TestSpawnMissingCLIPromptsInstall(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.services.cfg.Tools["claude"] = config.Tool{Command: "am-missing-cli-xyz", DefaultStatus: status.Idle}

	m.openForm()
	m.form().SetName("agent")
	m.form().SetDir(t.TempDir())
	claudeIndex := -1
	for i, name := range formTools(m) {
		if name == "claude" {
			claudeIndex = i
		}
	}
	if claudeIndex < 0 {
		t.Fatalf("claude not offered by the form: %v", formTools(m))
	}
	setFormTool(m, claudeIndex)
	pickGroup(t, m, "")
	_, cmd := m.submitForm()
	m.applyCmd(t, cmd)

	if !hintOpen(m) {
		t.Fatalf("screen = %v, err = %q, want the launch hint", m.screenID(), m.errBar.text)
	}
	if !strings.Contains(hintFix(m).Text, "am-missing-cli-xyz") {
		t.Fatalf("hint %q should name the missing binary", hintFix(m).Text)
	}
	if len(m.sessionRows()) != 0 {
		t.Fatalf("no session may spawn without the CLI, got %v", sessionNames(m))
	}
}

func TestReviveMissingCLIPromptsInstall(t *testing.T) {
	t.Parallel()
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
	if !hintOpen(m) {
		t.Fatalf("screen = %v, err = %q, want the launch hint", m.screenID(), m.errBar.text)
	}
	if !strings.Contains(hintFix(m).Text, "am-missing-cli-xyz") {
		t.Fatalf("hint %q should name the revive binary", hintFix(m).Text)
	}
}

func TestReportLaunchErrorKeepsPlainErrorsOnStatusLine(t *testing.T) {
	t.Parallel()
	m := buildModel(t)

	m.reportLaunchError(fmt.Errorf("tmux create: boom"))

	if !m.onList() {
		t.Fatalf("screen = %v, want the list", m.screenID())
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
	m.openConfirm(confirmTarget{prompt: uidialogs.Confirm{Action: uidialogs.ConfirmRestart}, lifecycle: uilifecycle.Target{Action: uilifecycle.Action(uidialogs.ConfirmRestart), Sessions: []store.Session{sess}}})

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.drainEffects(t)
	m = updated.(*Model)

	if !hintOpen(m) {
		t.Fatalf("screen = %v, err = %q, want the launch hint", m.screenID(), m.errBar.text)
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
	m.quick().SetTools([]string{"hermes"}, 0)

	updated, cmd := m.quickSpawn("backend", "fix the tests")
	m = updated.(*Model)
	m.applyCmd(t, cmd)

	if !hintOpen(m) {
		t.Fatalf("screen = %v, err = %q, want the launch hint", m.screenID(), m.errBar.text)
	}
	if m.quick() != nil {
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
	m.form().SetName("agent")
	m.form().SetDir(t.TempDir())
	m.form().SetTools([]string{"hermes"}, 0)
	path := tempImage(t, "mock.png")
	prompt := m.formPromptFor()
	prompt.Attachments = []composer.Attachment{{ID: 1, Path: path}}
	prompt.Input.SetValue("match " + composer.Token(1))

	_, cmd := m.submitForm()
	m.applyCmd(t, cmd)

	if !hintOpen(m) {
		t.Fatalf("screen = %v, err = %q, want the launch hint", m.screenID(), m.errBar.text)
	}
	// The dialog can still install the CLI and spawn this prompt, so it
	// takes the images over from the form and keeps the files until it
	// closes.
	if len(prompt.Attachments) != 0 {
		t.Fatalf("attachments = %+v, want the form's images handed to the dialog", prompt.Attachments)
	}
	if len(hintFix(m).Images) != 1 {
		t.Fatalf("dialog images = %+v, want the refused prompt's image", hintFix(m).Images)
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
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	initGitRepo(t, dir)
	m.openForm()
	m.form().SetName("agent")
	m.form().SetDir(dir)
	m.form().SetWorktree(true)
	m.applyTestMsg(t, m.formWorktreeProbeCmd(false)())
	if !m.formWorktreeOn() {
		t.Fatal("the worktree toggle should be on for this spawn")
	}
	// AddWorktree checks out into <repo>-worktrees/<session>, and refuses a
	// path that is already there. Taking the name it would pick is what
	// fails this spawn, on the far side of every field the form validates.
	// The path is built from git's repo root, which has symlinks followed
	// and Windows short names expanded.
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	taken := filepath.Join(filepath.Dir(root), filepath.Base(root)+"-worktrees", "agent")
	if err := os.MkdirAll(taken, 0o755); err != nil {
		t.Fatal(err)
	}
	path := tempImage(t, "mock.png")
	m.formPromptFor().Attachments = []composer.Attachment{{ID: 1, Path: path}}
	m.formPromptFor().Input.SetValue("match " + composer.Token(1))

	_, cmd := m.submitForm()
	m.applyCmd(t, cmd)

	if !formOpen(m) || m.errBar.text == "" {
		t.Fatalf("screen = %v, err = %q, want the form still up with the error", m.screenID(), m.errBar.text)
	}
	// Named so the test cannot pass on an earlier refusal: this is the
	// spawn failing, not a field the form checked before it got there.
	if !strings.Contains(m.errBar.text, taken) {
		t.Fatalf("err = %q, want the worktree path that blocked the spawn", m.errBar.text)
	}
	if len(m.sessionRows()) != 0 {
		t.Fatalf("a failed spawn leaves no session, got %v", sessionNames(m))
	}
	if len(m.formPromptFor().Attachments) != 1 {
		t.Fatalf("attachments = %+v, want the chip kept for the retry", m.formPromptFor().Attachments)
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
	m.form().SetName("agent")
	m.form().SetDir(t.TempDir())
	hermesIndex := -1
	for i, name := range formTools(m) {
		if name == "hermes" {
			hermesIndex = i
		}
	}
	if hermesIndex < 0 {
		t.Fatalf("hermes not offered by the form: %v", formTools(m))
	}
	setFormTool(m, hermesIndex)
	pickGroup(t, m, "")
	_, cmd := m.submitForm()
	m.applyCmd(t, cmd)

	if !hintOpen(m) {
		t.Fatalf("screen = %v, err = %q, want the launch hint", m.screenID(), m.errBar.text)
	}
	if len(m.sessionRows()) != 0 {
		t.Fatalf("no session may spawn without MCP support, got %v", sessionNames(m))
	}
}

func pressInLaunchHint(t *testing.T, m *Model, key rune) tea.Cmd {
	t.Helper()
	if !hintOpen(m) {
		t.Fatalf("screen = %v, want the launch hint", m.screenID())
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
	uiinstall.CopyCommand = func(text string) error {
		copied = text
		return nil
	}
	t.Cleanup(func() { uiinstall.CopyCommand = clipboard.WriteText })
	m.reportLaunchError(config.MissingToolError{Binary: "claude"})

	m.runBatch(t, pressInLaunchHint(t, m, 'c'))

	if want := deps.Command("claude"); copied != want {
		t.Fatalf("copied %q, want %q", copied, want)
	}
	if !hintOpen(m) {
		t.Fatalf("copy must leave the dialog up, screen = %v", m.screenID())
	}
	if m.errBar.text != "copied to clipboard" || !m.errBar.worked() {
		t.Fatalf("status = %q, want the copy confirmed", m.errBar.text)
	}
}

func TestLaunchHintCopyFailureIsReported(t *testing.T) {
	m := buildModel(t)
	uiinstall.CopyCommand = func(string) error { return errors.New("no clipboard backend") }
	t.Cleanup(func() { uiinstall.CopyCommand = clipboard.WriteText })
	m.reportLaunchError(config.MissingToolError{Binary: "claude"})

	m.runBatch(t, pressInLaunchHint(t, m, 'c'))

	if !strings.Contains(m.errBar.text, "no clipboard backend") || m.errBar.worked() {
		t.Fatalf("status = %q, want the copy failure", m.errBar.text)
	}
}

// A tool with no known recipe gets a read-only dialog: nothing to copy,
// nothing to run.
func TestLaunchHintWithoutARecipeOffersOnlyClose(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.reportLaunchError(config.MissingToolError{Binary: "acme"})

	frame := ansi.Strip(preparedView(m))
	if strings.Contains(frame, "copy") {
		t.Fatalf("dialog should offer neither copy nor install:\n%s", frame)
	}
	if cmd := pressInLaunchHint(t, m, 'i'); cmd != nil || !hintOpen(m) || m.ledger.install.Pending != nil {
		t.Fatalf("i must do nothing without a recipe: screen = %v, install = %+v", m.screenID(), m.ledger.install.Pending)
	}
	if cmd := pressInLaunchHint(t, m, 'c'); cmd != nil {
		t.Fatal("c must do nothing without a recipe")
	}
}

// The dialog keeps the mouse like every other card: a click on the
// command copies it, the same as c, and its foot keys answer a click.
func TestLaunchHintKeepsTheMouseAndCopiesOnAClick(t *testing.T) {
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	var copied string
	uiinstall.CopyCommand = func(text string) error {
		copied = text
		return nil
	}
	t.Cleanup(func() { uiinstall.CopyCommand = clipboard.WriteText })
	m.reportLaunchError(config.MissingToolError{Binary: "claude"})

	if opened := pressInLaunchHint(t, m, 'x'); opened != nil && fmt.Sprintf("%T", opened()) == fmt.Sprintf("%T", tea.DisableMouse()) {
		t.Fatal("opening the dialog released the mouse")
	}
	x, y := paintedCell(t, m, "install it with")
	m, cmd := leftClickCmd(m, x, y)
	m.runBatch(t, cmd)
	if want := deps.Command("claude"); copied != want || !hintOpen(m) {
		t.Fatalf("a click on the command copied %q, want %q; mode %v", copied, want, m.screenID())
	}
	if m = clickLabel(t, m, "esc close"); !m.onList() {
		t.Fatalf("esc in the foot should close the dialog, screen = %v", m.screenID())
	}
}

func TestLaunchHintStrayKeysKeepTheDialog(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.reportLaunchError(config.MissingToolError{Binary: "claude"})

	pressInLaunchHint(t, m, 'x')
	if !hintOpen(m) {
		t.Fatalf("screen = %v, want the dialog kept", m.screenID())
	}
	pressInLaunchHint(t, m, 'q')
	if !m.onList() {
		t.Fatalf("screen = %v, want q to close", m.screenID())
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
	retry := uilaunch.Request{
		ID:       newID(),
		Kind:     uilaunch.Form,
		ToolName: installRetryTool,
		Name:     retryName,
		Dir:      t.TempDir(),
		Pane:     sessioncmd.PaneSize{Width: w, Height: h},
		Images:   []composer.Attachment{{ID: 1, Path: image}},
	}
	m.openLaunchHint(uiinstall.Fix{
		Text:    "am-fake-cli is not installed.\n\ninstall it with: " + command,
		Command: command,
		Script:  command,
		Binary:  "am-fake-cli",
		Retry:   retry,
		Images:  []composer.Attachment{{ID: 1, Path: image}},
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
	testenv.SkipPOSIXShell(t)
	bin := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	target := filepath.Join(bin, "am-fake-cli")
	return "printf '#!/bin/sh\\nexec cat\\n' > " + target + " && chmod +x " + target
}

func waitForInstallToSettle(t *testing.T, m *Model) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for m.ledger.install.Pending != nil {
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

	if !hintOpen(m) || m.ledger.install.Pending != nil {
		t.Fatalf("install ran on Update: screen = %v pending=%+v", m.screenID(), m.ledger.install.Pending)
	}
	m.applyCmd(t, cmd)
	if !m.onList() {
		t.Fatalf("screen = %v, err = %q, want the dialog closed once the install starts", m.screenID(), m.errBar.text)
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	if quit == nil || !m.effects.Quitting {
		t.Fatal("killing the tracked installer should make quit available again")
	}
}

// A CLI on the interop PATH but not in the distro is a different problem
// from one nobody installed, and the dialog has to say so.
func TestLaunchHintNamesAWindowsOnlyInstall(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("a Windows copy reached through interop only exists under WSL")
	}
	m := buildModel(t)
	windowsPath := `/mnt/c/npm-global/claude`

	m.reportLaunchError(config.MissingToolError{Binary: "claude", WindowsPath: windowsPath})

	frame := ansi.Strip(preparedView(m))
	for _, want := range []string{"installed on Windows", "WSL distro", "claude.ai/install.sh"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("dialog is missing %q:\n%s", want, frame)
		}
	}
	// The path wraps in the card, so the dialog is checked for it before
	// the frame folds the line.
	if !strings.Contains(hintFix(m).Text, windowsPath) {
		t.Fatalf("dialog text %q should name the Windows copy", hintFix(m).Text)
	}
	if hintFix(m).Command != deps.Command("claude") {
		t.Fatalf("command = %q, want the Linux installer", hintFix(m).Command)
	}
}

// Two installs at once would leave the first one's launch unfinished, so
// the second press says where the running one is instead.
func TestLaunchHintRefusesASecondInstall(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	installFixture(t, m, "sleep 30")
	m.applyCmd(t, pressInLaunchHint(t, m, 'i'))
	running := terminalSession(t, m)

	installFixture(t, m, "sleep 30")
	pressInLaunchHint(t, m, 'i')

	if m.ledger.install.Pending == nil || m.ledger.install.Pending.SessionID != running.ID {
		t.Fatalf("install = %+v, want the first one kept", m.ledger.install.Pending)
	}
	if !strings.Contains(m.errBar.text, running.Name) {
		t.Fatalf("status = %q, want the running install named", m.errBar.text)
	}
	if shells := shellCount(m); shells != 1 {
		t.Fatalf("%d install shells, want 1", shells)
	}
}

// ctrl+c over the hint quits the way it does over the list: the card
// changes nothing about who owns the keyboard.
func TestLaunchHintQuitsOnCtrlC(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.reportLaunchError(config.MissingToolError{Binary: "claude"})

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = updated.(*Model)
	if cmd == nil || !quits(cmd()) {
		t.Fatal("ctrl+c over the hint should quit")
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
// images, so one more install can still spawn the same prompt. The form the
// prompt came from is gone once the hint is up, so the retry's own images
// are the only ones there are to keep.
func TestLaunchHintRetryThatStopsAgainKeepsTheImages(t *testing.T) {
	m := buildModel(t)
	_, image := installFixture(t, m, fakeInstallCommand(t))
	tool := m.services.cfg.Tools[installRetryTool]
	tool.Command = "am-next-missing-cli"
	m.services.cfg.Tools[installRetryTool] = tool

	m.applyCmd(t, pressInLaunchHint(t, m, 'i'))
	waitForInstallToSettle(t, m)

	if !hintOpen(m) {
		t.Fatalf("screen = %v, err = %q, want the dialog on the next missing tool", m.screenID(), m.errBar.text)
	}
	if len(hintFix(m).Images) != 1 {
		t.Fatalf("dialog images = %+v, want the prompt's image still held", hintFix(m).Images)
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

	m.applyCmd(t, pressInLaunchHint(t, m, 'i'))
	// The accepted install already captured its lifecycle. Removing the
	// live service now makes only the later typed retry fail outright.
	m.services.lifecycle = nil
	waitForInstallToSettle(t, m)

	if !m.onList() {
		t.Fatalf("screen = %v, want the list", m.screenID())
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
	t.Parallel()
	m := buildModel(t)
	command := `printf '%s\n' "one '\'' two" > /dev/null`
	installFixture(t, m, command)

	cmd := pressInLaunchHint(t, m, 'i')
	m.applyCmd(t, cmd)
	// Read the script before the refresh runs: a command this short can
	// have settled by then, and settling removes the file.
	script := m.ledger.install.Pending.Script
	statusFile := m.ledger.install.Pending.StatusFile
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
	t.Parallel()
	m := buildModel(t)
	sess := store.Session{ID: newID(), Name: "agent", Tool: "claude", Cwd: t.TempDir(), Archived: true}
	if err := m.services.store.CreateSession(sess); err != nil {
		t.Fatal(err)
	}
	if err := m.services.store.SetArchived(sess.ID, true); err != nil {
		t.Fatal(err)
	}
	m.services.cfg.Tools["claude"] = config.Tool{Command: "am-missing-cli-xyz", DefaultStatus: status.Idle}
	m.openConfirm(confirmTarget{prompt: uidialogs.Confirm{Action: uidialogs.ConfirmRestore}, lifecycle: uilifecycle.Target{Action: uilifecycle.Action(uidialogs.ConfirmRestore), Sessions: []store.Session{sess}}})

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.drainEffects(t)
	m = updated.(*Model)
	if !hintOpen(m) {
		t.Fatalf("screen = %v, err = %q, want the dialog", m.screenID(), m.errBar.text)
	}

	// What the install unblocks has to be the whole restore, not the
	// revive alone, so the retry is run here with a working CLI.
	m.services.cfg.Tools["claude"] = config.Tool{Command: testfixture.Cat(), DefaultStatus: status.Idle}
	if hintFix(m).Retry == nil {
		t.Fatal("missing captured restore retry")
	}
	m.enqueueEffect(hintFix(m).Retry, 0, false)
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

func TestExternallyManagedOldCLIDoesNotOfferTheVendorInstaller(t *testing.T) {
	m := buildModel(t)
	m.reportLaunchError(config.OutdatedToolError{Binary: "pi", Installed: "0.84.2", Minimum: "0.84.3", ManagedExternally: true})
	if hintFix(m).Command != "" || !strings.Contains(hintFix(m).Text, "the way you installed it") {
		t.Fatalf("external install hint = %+v", hintFix(m))
	}
}

func TestLaunchHintKeepsActionsAtSevenRows(t *testing.T) {
	m := buildModel(t)
	m.layout.width, m.layout.height = 110, 7
	m.reportLaunchError(config.MissingToolError{Binary: "cmd"})
	m.reportErr("install failed")
	frame := ansi.Strip(preparedView(m))
	if strings.Count(frame, "\n")+1 > 7 || !strings.Contains(frame, "close") {
		t.Fatalf("setup controls clipped.\n%s", frame)
	}
}

func TestMissingNpmCLIShowsOneLineAndInstallsTheRecipe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the Windows npm recipe is one line already")
	}
	m := buildModel(t)
	m.reportLaunchError(config.MissingToolError{Binary: "gemini"})
	fix := hintFix(m)
	if fix.Command != "npm install -g @google/gemini-cli" || strings.Contains(fix.Text, "install_cli") {
		t.Fatalf("dialog shows %q with text %q, want the npm line", fix.Command, fix.Text)
	}
	if fix.Script != deps.Command("gemini") || !strings.Contains(fix.Script, "ensure_npm") {
		t.Fatalf("i would run %q, want the npm recipe", fix.Script)
	}
}

func TestLaunchHintKeepsTheInstallLineOnATenRowTerminal(t *testing.T) {
	m := buildModel(t)
	m.layout.width, m.layout.height = 110, 10
	m.reportLaunchError(config.MissingToolError{Binary: "claude", WindowsPath: `C:\\Users\\someone\\AppData\\Local\\Programs\\claude\\claude.exe`})
	frame := ansi.Strip(preparedView(m))
	if strings.Count(frame, "\n")+1 > 10 || !strings.Contains(frame, "install it with") {
		t.Fatalf("install line clipped away.\n%s", frame)
	}
}

func TestLaunchHintClickCopiesTheCommandAfterClipping(t *testing.T) {
	m := buildModel(t)
	m.layout.width, m.layout.height = 110, 10
	var copied string
	uiinstall.CopyCommand = func(text string) error { copied = text; return nil }
	t.Cleanup(func() { uiinstall.CopyCommand = clipboard.WriteText })
	m.reportLaunchError(config.MissingToolError{Binary: "claude", WindowsPath: `C:\Users\someone\AppData\Local\Programs\claude\claude.exe`})
	x, y := paintedCell(t, m, "install it with")
	m, cmd := leftClickCmd(m, x, y)
	m.runBatch(t, cmd)
	if want := deps.Shown("claude"); copied != want {
		t.Fatalf("a click on the clipped card copied %q, want %q", copied, want)
	}
}

func hintOpen(m *Model) bool {
	_, ok := surfaceOf[*uidialogs.LaunchHintSurface](m)
	return ok
}

// hintFix is the fix the open card holds, zero when none is open.
func hintFix(m *Model) uiinstall.Fix {
	if fix, ok := m.launchHintFix(); ok {
		return *fix
	}
	return uiinstall.Fix{}
}

// hintFixPtr is the open card's fix, for a test that sets up what a
// completion would have attached.
func hintFixPtr(m *Model) *uiinstall.Fix {
	fix, ok := m.launchHintFix()
	if !ok {
		panic("no launch hint is open")
	}
	return fix
}

func hintKey(m *Model, msg tea.KeyMsg) tea.Cmd {
	_, cmd := m.surfaces[len(m.surfaces)-1].Key(m, msg)
	return cmd
}

// hintStartInstall asks for the install the way the card's key does, with
// or without the card still open.
func hintStartInstall(m *Model) { launchHintRoot{m}.StartInstall() }

func openHintFor(t *testing.T) (*Model, string) {
	t.Helper()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	image := tempImage(t, "refused.png")
	m.openLaunchHint(uiinstall.Fix{Text: "tool is missing", Command: "true", Script: "true", Binary: "tool", Images: []composer.Attachment{{ID: 1, Path: image}}})
	if !hintOpen(m) {
		t.Fatal("test setup did not open the hint")
	}
	return m, image
}

// esc and a click off the card both give the launch up, and Leave drops
// the refused prompt's images exactly once.
func TestLaunchHintClosesOnEscAndBackdrop(t *testing.T) {
	for name, closeIt := range map[string]func(*Model) *Model{
		"esc":      func(m *Model) *Model { m.Update(tea.KeyMsg{Type: tea.KeyEsc}); return m },
		"backdrop": func(m *Model) *Model { return clickBackdrop(t, m) },
	} {
		m, image := openHintFor(t)
		if !mustTop(t, m).Dismissible() {
			t.Fatalf("%s: the hint holds no draft and must be dismissible", name)
		}
		m = closeIt(m)
		if hintOpen(m) || m.effects.Quitting || !m.onList() {
			t.Errorf("%s: open=%t quitting=%t screen = %v", name, hintOpen(m), m.effects.Quitting, m.screenID())
		}
		if _, err := os.Stat(image); !os.IsNotExist(err) {
			t.Errorf("%s: Leave left the refused prompt's image behind", name)
		}
	}
}

// The update dialog opens over the form it came from and gives the screen
// back on close, with no return mode to keep.
func TestLaunchHintOverTheFormLeavesItsModeAlone(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.openForm())
	m.presentLaunchHint(uiinstall.Fix{Text: "pi is older", Command: "true", Binary: "pi", Update: true})
	if !formOpen(m) || !hintOpen(m) {
		t.Fatalf("mode %v open %t, want the hint over the form", m.screenID(), hintOpen(m))
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !formOpen(m) || hintOpen(m) {
		t.Fatalf("mode %v open %t, want the form back", m.screenID(), hintOpen(m))
	}
}

// A completion for an install the user has since replaced lands on nothing:
// with no hint open, the install result keeps its tracker and the images
// are cleaned up by the returned command.
func TestInstallStartAfterTheHintClosedKeepsTheTracker(t *testing.T) {
	m, _ := openHintFor(t)
	gen := m.gens.dialog
	m.Pop()
	cmd := m.ledger.install.Started(installRoot{m}, uiinstall.StartRequest{Gen: gen, Binary: "tool"}, uiinstall.StartResult{Pending: &uiinstall.Pending{Name: "install-tool", SessionID: "s"}}, nil)
	if m.ledger.install.Pending == nil || hintOpen(m) {
		t.Fatalf("install %+v hint open %t, want the tracker kept and no card revived", m.ledger.install.Pending, hintOpen(m))
	}
	_ = cmd
	if _, quit := m.requestQuit(); m.effects.Quitting {
		t.Fatalf("quit accepted with an install pending: %v", quit)
	}
}

func TestInstallStartDefersWorkOffUpdate(t *testing.T) {
	m := buildModel(t)
	installFixture(t, m, "sleep 30")

	started := make(chan struct{})
	release := make(chan struct{})
	home := t.TempDir()
	original := uiinstall.HomeDir
	uiinstall.HomeDir = func() (string, error) {
		close(started)
		<-release
		return home, nil
	}
	t.Cleanup(func() { uiinstall.HomeDir = original })

	pressInLaunchHint(t, m, 'i')
	if m.effects.Main.Active == nil {
		t.Fatal("install press queued no effect")
	}
	if _, ok := m.effects.Main.Active.Request.(uiinstall.StartRequest); !ok {
		t.Fatalf("active effect = %T, want uiinstall.StartRequest", m.effects.Main.Active.Request)
	}
	if m.ledger.install.Pending != nil || storeRows(t, m) != 0 {
		t.Fatalf("install ran on Update: pending=%+v rows=%d", m.ledger.install.Pending, storeRows(t, m))
	}

	completed := make(chan tea.Msg, 1)
	go func() { completed <- m.effects.Main.Active.Command() }()
	<-started
	updated := make(chan struct{})
	go func() {
		m.Update(tea.WindowSizeMsg{Width: 150, Height: 45})
		close(updated)
	}()
	select {
	case <-updated:
	case <-time.After(time.Second):
		close(release)
		<-completed
		t.Fatal("window update waited on the install worker")
	}
	close(release)
	m.applyTestMsg(t, <-completed)
	m.drainEffects(t)
	if m.ledger.install.Pending == nil || storeRows(t, m) != 1 {
		t.Fatalf("completion = pending %+v, rows %d; want one running install", m.ledger.install.Pending, storeRows(t, m))
	}
}

func TestInstallStartRefusesDuplicateWhileQueued(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	installFixture(t, m, "sleep 30")

	pressInLaunchHint(t, m, 'i')
	first := m.effects.Main.Active
	pressInLaunchHint(t, m, 'i')

	if m.effects.Main.Active != first || len(m.effects.Main.Pending) != 0 {
		t.Fatalf("duplicate install changed the queue: active=%p first=%p pending=%d", m.effects.Main.Active, first, len(m.effects.Main.Pending))
	}
	if !strings.Contains(m.errBar.text, "already") {
		t.Fatalf("duplicate status = %q, want the in-progress install named", m.errBar.text)
	}
}

func TestInstallStartKeepsDurableShellRowWhenSendIsRefused(t *testing.T) {
	m := buildModel(t)
	_, image := installFixture(t, m, "true")
	original := uiinstall.SendText
	calls := 0
	uiinstall.SendText = func(*tmux.Driver, string, string) (tmux.SendResult, error) {
		calls++
		if calls == 1 {
			return tmux.SendResult{Phase: tmux.SendPhaseLoadStarted}, errors.New("fixture send failed")
		}
		return tmux.SendResult{Phase: tmux.SendPhaseSubmitted}, nil
	}
	t.Cleanup(func() { uiinstall.SendText = original })

	m.applyCmd(t, pressInLaunchHint(t, m, 'i'))

	if m.ledger.install.Pending != nil {
		t.Fatalf("failed send left a pending install: %+v", m.ledger.install.Pending)
	}
	if !hintOpen(m) {
		t.Fatalf("screen = %v, want the same setup dialog kept", m.screenID())
	}
	if storeRows(t, m) != 1 || len(m.workspace.sessions) != 1 {
		t.Fatalf("durable shell row was not reconciled: store=%d workspace=%d", storeRows(t, m), len(m.workspace.sessions))
	}
	if !m.services.tmux.Exists(m.workspace.sessions[0].ID) {
		t.Fatal("durable installer shell pane is missing")
	}
	if !strings.Contains(m.errBar.text, "fixture send failed") {
		t.Fatalf("status = %q, want send failure", m.errBar.text)
	}
	if len(hintFix(m).Images) != 1 || !imageExists(t, image) {
		t.Fatal("the retained dialog lost the refused prompt image")
	}
	sess := m.workspace.sessions[0]
	for _, path := range []string{
		m.services.hooks.InstallStatusFile(sess.ID),
		filepath.Join(filepath.Dir(m.services.hooks.InstallStatusFile(sess.ID)), sess.ID+".install.sh"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("refused transport kept %s: %v", path, err)
		}
	}

	m.applyCmd(t, pressInLaunchHint(t, m, 'i'))
	if calls != 2 {
		t.Fatalf("retry sent %d commands, want two total attempts", calls)
	}
}

func TestInstallStartKeepsUncertainSendWithoutBlindRetry(t *testing.T) {
	m := buildModel(t)
	_, image := installFixture(t, m, "true")
	original := uiinstall.SendText
	calls := 0
	uiinstall.SendText = func(*tmux.Driver, string, string) (tmux.SendResult, error) {
		calls++
		return tmux.SendResult{Phase: tmux.SendPhasePasteStarted}, errors.New("fixture post-paste failure")
	}
	t.Cleanup(func() { uiinstall.SendText = original })

	m.applyCmd(t, pressInLaunchHint(t, m, 'i'))

	if m.ledger.install.Pending == nil {
		t.Fatal("uncertain send lost the pending install tracker")
	}
	if !m.onList() {
		t.Fatalf("screen = %v, want the retry dialog closed", m.screenID())
	}
	if _, err := os.Stat(m.ledger.install.Pending.Script); err != nil {
		t.Fatalf("uncertain send removed its script: %v", err)
	}
	if !imageExists(t, image) || len(m.ledger.install.Pending.Images) != 1 {
		t.Fatal("uncertain send lost its prompt image ownership")
	}
	if !strings.Contains(m.errBar.text, "may be running") || !strings.Contains(m.errBar.text, "inspect") {
		t.Fatalf("status = %q, want explicit uncertain-send guidance", m.errBar.text)
	}
	hintStartInstall(m)
	if calls != 1 {
		t.Fatalf("uncertain install was sent %d times, want no blind retry", calls)
	}
}

func TestInstallStartCompletionDoesNotReplaceNewerDialog(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	_, firstImage := installFixture(t, m, "sleep 30")
	pressInLaunchHint(t, m, 'i')
	first := m.effects.Main.Active.Command

	secondImage := tempImage(t, "second.png")
	m.openLaunchHint(uiinstall.Fix{
		Text:    "second setup step",
		Command: "true",
		Script:  "true",
		Binary:  "second-cli",
		Images:  []composer.Attachment{{ID: 2, Path: secondImage}},
	})
	newGen := m.gens.dialog

	m.applyTestMsg(t, first())
	m.drainEffects(t)

	if !hintOpen(m) || m.gens.dialog != newGen || hintFix(m).Binary != "second-cli" {
		t.Fatalf("completion replaced newer dialog: screen = %v gen=%d fix=%+v", m.screenID(), m.gens.dialog, hintFix(m))
	}
	if m.ledger.install.Pending == nil || m.ledger.install.Pending.Binary != "am-fake-cli" {
		t.Fatalf("first install was not reconciled behind the dialog: %+v", m.ledger.install.Pending)
	}
	if len(hintFix(m).Images) != 1 || !imageExists(t, secondImage) {
		t.Fatal("newer dialog lost its image")
	}
	if len(m.ledger.install.Pending.Images) != 1 || !imageExists(t, firstImage) {
		t.Fatal("pending install lost the first prompt image")
	}
}

func TestInstallStartStaleFailureDefersImageCleanupToCommand(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	image := tempImage(t, "stale.png")
	m.gens.dialog = 2
	m.presentLaunchHint(uiinstall.Fix{Binary: "newer-cli"})

	cmd := m.ledger.install.Started(installRoot{m},
		uiinstall.StartRequest{Gen: 1, Images: []composer.Attachment{{ID: 1, Path: image}}},
		uiinstall.StartResult{},
		errors.New("fixture start failed"),
	)

	if !imageExists(t, image) {
		t.Fatal("applying the stale completion performed image I/O")
	}
	if cmd == nil {
		t.Fatal("stale completion returned no captured cleanup command")
	}
	cmd()
	if imageExists(t, image) {
		t.Fatal("captured cleanup command kept the stale prompt image")
	}
	if !hintOpen(m) || hintFix(m).Binary != "newer-cli" {
		t.Fatalf("stale completion replaced the newer dialog: screen = %v fix=%+v", m.screenID(), hintFix(m))
	}
}

func TestInstallBlocksQuitUntilPendingRetrySettles(t *testing.T) {
	t.Run("start queued", func(t *testing.T) {
		m := buildModel(t)
		installFixture(t, m, "true")
		pressInLaunchHint(t, m, 'i')

		updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
		m = updated.(*Model)
		if cmd != nil || m.effects.Quitting {
			t.Fatalf("quit was accepted during install start: cmd=%v quitting=%t", cmd, m.effects.Quitting)
		}
		if m.effects.Main.Active == nil {
			t.Fatal("refused quit discarded the accepted install start")
		}
		if !strings.Contains(m.errBar.text, "starting") || !strings.Contains(m.errBar.text, "wait") {
			t.Fatalf("status = %q, want actionable starting guidance", m.errBar.text)
		}
	})

	t.Run("pending settles and retries", func(t *testing.T) {
		m := buildModel(t)
		retryName, _ := installFixture(t, m, fakeInstallCommand(t))
		m.applyCmd(t, pressInLaunchHint(t, m, 'i'))
		if m.ledger.install.Pending == nil {
			t.Fatal("fixture install did not become pending")
		}

		updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
		m = updated.(*Model)
		if cmd != nil || m.effects.Quitting {
			t.Fatalf("quit was accepted during pending install: cmd=%v quitting=%t", cmd, m.effects.Quitting)
		}
		if !strings.Contains(m.errBar.text, "kill") || !strings.Contains(m.errBar.text, m.ledger.install.Pending.Name) {
			t.Fatalf("status = %q, want the installer shell and lifecycle guidance", m.errBar.text)
		}

		waitForInstallToSettle(t, m)
		if !hasSessionNamed(m, retryName) {
			t.Fatalf("blocked quit lost the captured retry: %v", sessionNames(m))
		}
		_, cmd = m.requestQuit()
		if cmd == nil || !m.effects.Quitting {
			t.Fatal("quit stayed blocked after the install and retry settled")
		}
	})
}

func TestInstallSettleDefersStatusAndBinaryChecks(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	statusFile := filepath.Join(dir, "install.status")
	script := filepath.Join(dir, "install.sh")
	if err := os.WriteFile(statusFile, []byte("0"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	image := tempImage(t, "settle.png")
	m.ledger.install.Pending = &uiinstall.Pending{
		SessionID:  "installer",
		Name:       "install-am-fake-cli",
		Binary:     "am-fake-cli",
		StatusFile: statusFile,
		Script:     script,
		Images:     []composer.Attachment{{ID: 1, Path: image}},
	}

	started := make(chan struct{})
	release := make(chan struct{})
	original := uiinstall.CheckInstalled
	uiinstall.CheckInstalled = func(string) error {
		close(started)
		<-release
		return nil
	}
	t.Cleanup(func() { uiinstall.CheckInstalled = original })

	m.settleInstall()
	if m.effects.Main.Active != nil {
		t.Fatal("settle started its worker instead of only queuing it")
	}
	if len(m.effects.Main.Pending) != 1 {
		t.Fatalf("settle queue = %d, want one request", len(m.effects.Main.Pending))
	}
	if _, err := os.Stat(statusFile); err != nil {
		t.Fatalf("Update-path settle touched the status file: %v", err)
	}
	m.nextEffectCmd()
	completed := make(chan tea.Msg, 1)
	go func() { completed <- m.effects.Main.Active.Command() }()
	<-started
	updated := make(chan struct{})
	go func() {
		m.Update(tea.WindowSizeMsg{Width: 151, Height: 46})
		close(updated)
	}()
	select {
	case <-updated:
	case <-time.After(time.Second):
		close(release)
		<-completed
		t.Fatal("window update waited on the settle worker")
	}
	close(release)
	m.applyTestMsg(t, <-completed)
	m.drainEffects(t)

	if m.ledger.install.Pending != nil {
		t.Fatalf("completed settle still pending: %+v", m.ledger.install.Pending)
	}
	if imageExists(t, image) {
		t.Fatal("successful install with no retry kept an unowned prompt image")
	}
	for _, path := range []string{statusFile, script} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s should be removed by the worker, stat err = %v", path, err)
		}
	}
}

func TestInstallSettleKeepsTrackerOnTransientStatusReadFailure(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	statusDir := filepath.Join(dir, "status-dir")
	if err := os.Mkdir(statusDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(statusDir, "keep"), []byte("sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "install.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	image := tempImage(t, "transient.png")
	m.ledger.install.Pending = &uiinstall.Pending{
		SessionID:  "installer",
		Name:       "install-am-fake-cli",
		Binary:     "am-fake-cli",
		StatusFile: statusDir,
		Script:     script,
		Images:     []composer.Attachment{{ID: 1, Path: image}},
	}

	m.settleInstall()
	m.drainEffects(t)

	if m.ledger.install.Pending == nil {
		t.Fatal("transient status read failure cleared the install tracker")
	}
	for _, path := range []string{statusDir, script, image} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("transient observer failure removed %s: %v", path, err)
		}
	}
	if !strings.Contains(m.errBar.text, "read") || !strings.Contains(m.errBar.text, m.ledger.install.Pending.Name) {
		t.Fatalf("status = %q, want the observer failure and installer named", m.errBar.text)
	}

	statusFile := filepath.Join(dir, "install.status")
	if err := os.WriteFile(statusFile, []byte("0"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.ledger.install.Pending.StatusFile = statusFile
	original := uiinstall.CheckInstalled
	uiinstall.CheckInstalled = func(string) error { return nil }
	t.Cleanup(func() { uiinstall.CheckInstalled = original })
	m.settleInstall()
	m.drainEffects(t)
	if m.ledger.install.Pending != nil {
		t.Fatalf("later successful observation kept pending install: %+v", m.ledger.install.Pending)
	}
}

func TestInstallSettleRetriesTransientInstalledCheck(t *testing.T) {
	m := buildModel(t)
	retryName, image := installFixture(t, m, "true")
	dir := t.TempDir()
	statusFile := filepath.Join(dir, "install.status")
	script := filepath.Join(dir, "install.sh")
	if err := os.WriteFile(statusFile, []byte("0"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	m.ledger.install.Pending = &uiinstall.Pending{
		SessionID:  "installer",
		Name:       "install-am-fake-cli",
		Binary:     "am-fake-cli",
		StatusFile: statusFile,
		Script:     script,
		Retry:      hintFix(m).Retry,
		Images:     []composer.Attachment{{ID: 1, Path: image}},
	}
	checks := 0
	original := uiinstall.CheckInstalled
	uiinstall.CheckInstalled = func(string) error {
		checks++
		if checks == 1 {
			return errors.New("fixture PATH lookup unavailable")
		}
		return nil
	}
	t.Cleanup(func() { uiinstall.CheckInstalled = original })

	runSettle := func() {
		t.Helper()
		m.settleInstall()
		cmd := m.nextEffectCmd()
		if cmd == nil {
			t.Fatal("settle queued no worker")
		}
		m.applyTestMsg(t, cmd())
	}
	runSettle()

	if m.ledger.install.Pending == nil {
		t.Fatal("transient installed check cleared the install tracker")
	}
	for _, path := range []string{statusFile, script, image} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("transient installed check removed %s: %v", path, err)
		}
	}
	if m.effects.Main.Active != nil || len(m.effects.Main.Pending) != 0 {
		t.Fatal("transient installed check queued the captured retry")
	}
	if !strings.Contains(m.errBar.text, "looking") || !strings.Contains(m.errBar.text, "PATH lookup") {
		t.Fatalf("status = %q, want the installed-check failure", m.errBar.text)
	}

	runSettle()
	if m.ledger.install.Pending != nil {
		t.Fatalf("successful recheck kept the tracker: %+v", m.ledger.install.Pending)
	}
	if m.effects.Main.Active == nil {
		t.Fatal("successful recheck did not activate the captured retry")
	}
	request, ok := m.effects.Main.Active.Request.(uilaunch.Request)
	if !ok || request.Name != retryName {
		t.Fatalf("successful recheck active effect = %#v, want captured retry %q", m.effects.Main.Active.Request, retryName)
	}
	for _, path := range []string{statusFile, script} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("successful recheck kept %s: %v", path, err)
		}
	}
	if !imageExists(t, image) {
		t.Fatal("queued retry lost its prompt image")
	}
}

func TestInstallSettleKeepsTrackerWhenShellLivenessIsUnknown(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	script := filepath.Join(dir, "install.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	image := tempImage(t, "liveness.png")
	m.ledger.install.Pending = &uiinstall.Pending{
		SessionID:  "installer",
		Name:       "install-am-fake-cli",
		Binary:     "am-fake-cli",
		StatusFile: filepath.Join(dir, "missing.status"),
		Script:     script,
		Images:     []composer.Attachment{{ID: 1, Path: image}},
	}
	original := uiinstall.SessionExists
	uiinstall.SessionExists = func(*tmux.Driver, string) (bool, error) {
		return false, errors.New("fixture liveness timeout")
	}
	t.Cleanup(func() { uiinstall.SessionExists = original })

	m.settleInstall()
	m.drainEffects(t)

	if m.ledger.install.Pending == nil {
		t.Fatal("unknown shell liveness cleared the install tracker")
	}
	for _, path := range []string{script, image} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("unknown shell liveness removed %s: %v", path, err)
		}
	}
	if !strings.Contains(m.errBar.text, "liveness") || !strings.Contains(m.errBar.text, m.ledger.install.Pending.Name) {
		t.Fatalf("status = %q, want the liveness failure and installer named", m.errBar.text)
	}
	_, quit := m.requestQuit()
	if quit != nil || m.effects.Quitting {
		t.Fatal("unknown shell liveness allowed quit to discard the tracker")
	}
}

// The installer shell takes the screen from the quick bar. A start that
// lands after the dialog closed leaves the bar alone.
func TestInstallStartGivesTheInstallerShellTheKeys(t *testing.T) {
	session := store.Session{ID: "installer", Name: "install pi"}
	result := uiinstall.StartResult{
		Session: session,
		Pending: &uiinstall.Pending{SessionID: session.ID, Binary: "pi"},
	}
	for _, failure := range []error{nil, errors.New("send failed")} {
		m := buildModel(t)
		image := tempImage(t, "draft.png")
		pushQuick(m)
		m.quick().Attachments = []composer.Attachment{{ID: 1, Path: image}}
		m.gens.dialog = 1
		m.presentLaunchHint(uiinstall.Fix{})
		cmd := m.ledger.install.Started(installRoot{m}, uiinstall.StartRequest{Gen: 1, Binary: "pi"}, result, failure)
		if !imageExists(t, image) {
			t.Fatal("update deleted the image before the cleanup command ran")
		}
		m.applyCmd(t, cmd)
		if !m.onList() || (m.quick() != nil) {
			t.Fatalf("failure %v left mode %v quick %v", failure, m.screenID(), (m.quick() != nil))
		}
		if imageExists(t, image) {
			t.Fatal("the installer handoff left the draft image")
		}
	}

	m := buildModel(t)
	image := tempImage(t, "kept.png")
	pushQuick(m)
	m.quick().Attachments = []composer.Attachment{{ID: 1, Path: image}}
	m.gens.dialog = 2
	m.ledger.install.Started(installRoot{m}, uiinstall.StartRequest{Gen: 1, Binary: "pi"}, result, nil)
	if (m.quick() == nil) || !m.onList() {
		t.Fatalf("a start behind a closed dialog left mode %v quick %v", m.screenID(), (m.quick() != nil))
	}
	if !imageExists(t, image) {
		t.Fatal("a start behind a closed dialog deleted the draft image")
	}
}
