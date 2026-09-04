package ui

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/YoanWai/agent-manager/internal/clipboard"
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/deps"
	"github.com/YoanWai/agent-manager/internal/mcpreg"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// launchFix is what the setup-step dialog holds: the text it shows, the
// command that unblocks the launch, the binary that command puts on PATH,
// the launch to run again once it is there, and the images that launch's
// prompt names. command is empty when the manager knows no recipe, which
// leaves the dialog read-only.
type launchFix struct {
	text    string
	command string
	binary  string
	retry   func() error
	images  []imageAttachment
}

// pendingInstall is an install the dialog started in a shell tab: the
// session running it, the files its script writes and runs from, and the
// launch to finish once the command's exit status lands.
type pendingInstall struct {
	sessionID  string
	name       string
	binary     string
	statusFile string
	script     string
	retry      func() error
	images     []imageAttachment
}

// copyLaunchCommand is the seam tests swap so a copy never reaches the
// desktop clipboard.
var copyLaunchCommand = clipboard.WriteText

type launchCommandCopiedMsg struct {
	err error
}

// reportLaunchError routes a failed spawn to the right surface: a launch
// the manager refused for a missing prerequisite opens a dialog naming the
// command that unblocks it, anything else stays a line of status text.
// retry is the launch to run again once that command has done its work;
// nil when the caller has no way to repeat it.
func (m *Model) reportLaunchError(err error, retry func() error) {
	if errors.Is(err, mcpreg.ErrHermesMCPUnavailable) {
		m.openLaunchHint(launchFix{
			text: "Hermes sessions carry the agent-manager MCP tools, and this Hermes cannot load them: its MCP SDK is not installed.\n\n" +
				"Run `hermes setup` to install MCP support, then spawn again.",
			command: "hermes setup",
			binary:  "hermes",
			retry:   retry,
		})
		return
	}
	var missing config.MissingToolError
	if errors.As(err, &missing) {
		m.openLaunchHint(launchFix{
			text:    missingToolText(missing),
			command: deps.Command(missing.Binary),
			binary:  missing.Binary,
			retry:   retry,
		})
		return
	}
	m.errBar.text = err.Error()
}

// missingToolText opens on why the launch stopped, then the command that
// unblocks it. A CLI installed only on Windows is on this PATH through
// WSL interop, which the distro cannot run as the agent, so the dialog
// says where that copy is rather than claiming nothing is installed.
func missingToolText(missing config.MissingToolError) string {
	head := missing.Binary + " is not installed."
	if missing.WindowsPath != "" {
		head = missing.Binary + " is installed on Windows, not in this WSL distro.\n\n" +
			"The Windows copy is at " + missing.WindowsPath + "."
	}
	return head + "\n\n" + deps.Hint(missing.Binary)
}

// openLaunchHint takes the refused prompt's images out of the composers:
// the prompt text already names their paths, and the dialog owns the
// files until the launch runs or is given up, so the form and the quick
// bar can be reopened meanwhile.
func (m *Model) openLaunchHint(fix launchFix) {
	fix.images = append(fix.images, m.form.prompt.attachments...)
	fix.images = append(fix.images, m.quick.attachments...)
	m.form.prompt.attachments = nil
	m.quick.attachments = nil
	m.launchFix = fix
	m.mode = modeLaunchHint
}

func removeInstallFiles(statusFile, script string) {
	_ = os.Remove(statusFile)
	_ = os.Remove(script)
}

func dropImages(images []imageAttachment) {
	for _, att := range images {
		if att.path != "" {
			_ = os.Remove(att.path)
		}
	}
}

func (m *Model) handleLaunchHintKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "c":
		if m.launchFix.command == "" {
			return m, nil
		}
		command := m.launchFix.command
		return m, func() tea.Msg {
			return launchCommandCopiedMsg{err: copyLaunchCommand(command)}
		}
	case "i":
		if m.launchFix.command == "" {
			return m, nil
		}
		return m.startInstall()
	case "esc", "q", "enter":
		m.closeLaunchHint()
	}
	return m, nil
}

// closeLaunchHint drops the dialog and the images the refused prompt was
// holding: they stayed alive while an install could still spawn it.
func (m *Model) closeLaunchHint() {
	dropImages(m.launchFix.images)
	m.launchFix = launchFix{}
	m.mode = modeList
}

func (m *Model) handleLaunchCommandCopied(msg launchCommandCopiedMsg) {
	if msg.err != nil {
		m.errBar.text = "copy failed: " + msg.err.Error()
		return
	}
	m.reportDone("copied to clipboard")
}

// startInstall types the dialog's command into a new shell tab, so the
// user can watch it and answer its prompts, and remembers the launch to
// finish once the shell reports how the command ended. The command runs
// in the user's own shell so an installer sees the same PATH and tools
// a hand-typed one would.
func (m *Model) startInstall() (tea.Model, tea.Cmd) {
	if m.install != nil {
		m.errBar.text = "an install is already running in " + m.install.name
		return m, nil
	}
	fix := m.launchFix
	toolName, tool, ok := m.shellTool()
	if !ok {
		m.errBar.text = `no shell configured: add a tool block with shell = true to config.toml`
		return m, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
	sess := store.Session{
		ID:     newID(),
		Name:   "install-" + fix.binary,
		Tool:   toolName,
		Cwd:    home,
		Group:  m.contextGroup(),
		Status: status.Starting,
	}
	statusFile := m.hooks.InstallStatusFile(sess.ID)
	script, err := m.hooks.WriteInstallScript(sess.ID, installScript(fix.command, statusFile))
	if err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
	if err := m.launchNewSession(sess, tool, tool.Command, launchOptions{}); err != nil {
		removeInstallFiles(statusFile, script)
		m.errBar.text = err.Error()
		return m, nil
	}
	if err := m.tmux.SendText(sess.ID, "sh "+tmux.ShellQuote(script)); err != nil {
		// The shell is left open: it is a tab like any other, and the
		// command it never ran is still on the dialog to copy.
		removeInstallFiles(statusFile, script)
		m.errBar.text = err.Error()
		return m, nil
	}
	m.install = &pendingInstall{
		sessionID:  sess.ID,
		name:       sess.Name,
		binary:     fix.binary,
		statusFile: statusFile,
		script:     script,
		retry:      fix.retry,
		images:     fix.images,
	}
	m.launchFix = launchFix{}
	m.mode = modeList
	m.statusFilter = statusFilterAll
	m.focusSession(sess.ID)
	m.reportDone("installing " + fix.binary)
	return m, m.refreshCmd()
}

// installScript shows the command, runs it, and records how it ended. The
// command runs in a subshell so an installer that exits cannot skip the
// status write, and the whole thing is a file so the pane's shell is typed
// one short line: an installer's own quoting then reaches sh unchanged
// whatever shell the user runs.
func installScript(command, statusFile string) string {
	interrupted := "printf %s 130 > " + tmux.ShellQuote(statusFile) + "; exit 130"
	return "#!/bin/sh\n" +
		"trap " + tmux.ShellQuote(interrupted) + " INT TERM\n" +
		"printf '%s\\n' " + tmux.ShellQuote("$ "+command) + "\n" +
		"(" + command + ")\n" +
		`printf %s "$?" > ` + tmux.ShellQuote(statusFile) + "\n"
}

// settleInstall runs on every poll while an install is pending: once the
// shell has written the command's exit status, the launch the dialog
// refused runs again, or the failure is put on the status line with the
// tab still open to read.
func (m *Model) settleInstall() {
	install := m.install
	if install == nil {
		return
	}
	data, err := os.ReadFile(install.statusFile)
	if errors.Is(err, fs.ErrNotExist) {
		// A shell killed before the command ended takes the launch with it.
		if !m.tmux.Exists(install.sessionID) {
			m.install = nil
			removeInstallFiles(install.statusFile, install.script)
			dropImages(install.images)
		}
		return
	}
	if err != nil {
		m.install = nil
		removeInstallFiles(install.statusFile, install.script)
		m.errBar.text = err.Error()
		return
	}
	// The shell creates the file before printf fills it; an empty file is a
	// command that has just ended, and the next poll reads its status.
	code := strings.TrimSpace(string(data))
	if code == "" {
		return
	}
	m.install = nil
	removeInstallFiles(install.statusFile, install.script)
	if code != "0" {
		dropImages(install.images)
		m.errBar.text = fmt.Sprintf("%s install exited with status %s; its output is in %s", install.binary, code, install.name)
		return
	}
	if err := config.CheckInstalled(install.binary); err != nil {
		dropImages(install.images)
		var missing config.MissingToolError
		if !errors.As(err, &missing) {
			m.errBar.text = fmt.Sprintf("%s installer finished, and looking for it failed: %v", install.binary, err)
			return
		}
		m.errBar.text = fmt.Sprintf("%s installer finished, but %s is still not on PATH; add its directory to PATH, the installer's output names it", install.binary, install.binary)
		return
	}
	if install.retry == nil {
		dropImages(install.images)
		m.reportDone(install.binary + " installed")
		return
	}
	if err := install.retry(); err != nil {
		m.reportLaunchError(err, install.retry)
		if m.mode != modeLaunchHint {
			dropImages(install.images)
			return
		}
		// The dialog can still clear whatever stopped it this time and
		// spawn the same prompt, so it keeps holding the images.
		m.launchFix.images = append(install.images, m.launchFix.images...)
		return
	}
	// The launched prompt names the image paths, so the files stay for the
	// agent to read; the stale-paste sweep retires them.
	m.statusFilter = statusFilterAll
	m.rebuildRows()
	m.requestRefresh()
	m.reportDone(install.binary + " installed; the session it was holding up is launching")
}

func (m *Model) viewLaunchHint() string {
	width := m.cardWidth()
	inner := cardInnerWidth(width)
	tone := lipgloss.NewStyle().Foreground(colorAccent).Bold(true)

	var body strings.Builder
	for i, paragraph := range strings.Split(m.launchFix.text, "\n\n") {
		style := mutedStyle
		if i == 0 {
			style = tone
		}
		if i > 0 {
			body.WriteString("\n")
		}
		for _, line := range strings.Split(ansi.Wordwrap(paragraph, inner, "-"), "\n") {
			body.WriteString(style.Render(line) + "\n")
		}
	}
	hint := [][2]string{{"esc", "close"}}
	if m.launchFix.command != "" {
		hint = [][2]string{{"i", "install"}, {"c", "copy"}, {"esc", "close"}}
	}
	return m.cardSized(width, "◈ Session needs a setup step", strings.TrimRight(body.String(), "\n"), hint)
}
