package ui

import (
	"errors"
	"os"
	"strings"

	"github.com/YoanWai/agent-manager/internal/clipboard"
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/deps"
	"github.com/YoanWai/agent-manager/internal/mcpreg"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
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
	text        string
	command     string
	binary      string
	effectRetry effectRequest
	images      []imageAttachment
}

// pendingInstall is an install the dialog started in a shell tab: the
// session running it, the files its script writes and runs from, and the
// launch to finish once the command's exit status lands.
type pendingInstall struct {
	sessionID   string
	name        string
	binary      string
	statusFile  string
	script      string
	effectRetry effectRequest
	images      []imageAttachment
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
// A retry, when one exists, is attached by the typed effect completion that
// failed; this error router never carries or invokes a Model closure.
func (m *Model) reportLaunchError(err error) {
	var hermesMCP mcpreg.HermesMCPUnavailableError
	if errors.As(err, &hermesMCP) {
		step := "Install the mcp package into the Python that runs Hermes, then spawn again."
		if hermesMCP.PipCommand != "" {
			step = "Run `" + hermesMCP.PipCommand + "` to add the mcp package to the Python that runs Hermes, then spawn again."
		}
		m.openLaunchHint(launchFix{
			text: "Hermes sessions carry the agent-manager MCP tools, and this Hermes cannot load them: its MCP SDK is not installed.\n\n" +
				step,
			command: hermesMCP.PipCommand,
			binary:  "hermes",
		})
		return
	}
	var missing config.MissingToolError
	if errors.As(err, &missing) {
		m.openLaunchHint(launchFix{
			text:    missingToolText(missing),
			command: deps.Command(missing.Binary),
			binary:  missing.Binary,
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
	m.gens.dialog++
	fix.images = append(fix.images, m.form.prompt.attachments...)
	fix.images = append(fix.images, m.quick.attachments...)
	m.form.prompt.attachments = nil
	m.quick.attachments = nil
	m.launchHint.fix = fix
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
		return m.requestQuit()
	case "c":
		if m.launchHint.fix.command == "" {
			return m, nil
		}
		command := m.launchHint.fix.command
		return m, func() tea.Msg {
			return launchCommandCopiedMsg{err: copyLaunchCommand(command)}
		}
	case "i":
		if m.launchHint.fix.command == "" {
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
	dropImages(m.launchHint.fix.images)
	m.launchHint.fix = launchFix{}
	m.mode = modeList
}

func (m *Model) handleLaunchCommandCopied(msg launchCommandCopiedMsg) {
	if msg.err != nil {
		m.errBar.text = "copy failed: " + msg.err.Error()
		return
	}
	m.reportDone("copied to clipboard")
}

// startInstall captures the setup dialog and queues its filesystem, store,
// lifecycle, and tmux work. The dialog stays in front until that job says
// the command was typed into a durable shell row.
func (m *Model) startInstall() (tea.Model, tea.Cmd) {
	if m.launchHint.install != nil {
		m.errBar.text = "an install is already running in " + m.launchHint.install.name
		return m, nil
	}
	for _, job := range append([]*effectJob{m.effects.active}, m.effects.pending...) {
		if job == nil {
			continue
		}
		if request, ok := job.request.(installStartRequest); ok {
			m.errBar.text = "an install is already starting for " + request.binary
			return m, nil
		}
	}
	fix := m.launchHint.fix
	toolName, _ := m.shellTool()
	w, h := m.paneTargetSize()
	retry := fix.effectRetry
	if spawn, ok := retry.(spawnRequest); ok && len(spawn.images) == 0 {
		spawn.images = append([]imageAttachment(nil), fix.images...)
		retry = spawn
	}
	m.launchHint.fix.images = nil
	m.enqueueEffect(installStartRequest{
		gen:         m.gens.dialog,
		id:          newID(),
		command:     fix.command,
		binary:      fix.binary,
		toolName:    toolName,
		group:       m.contextGroup(),
		pane:        sessioncmd.PaneSize{Width: w, Height: h},
		effectRetry: retry,
		images:      fix.images,
	}, 0, false)
	return m, nil
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

// settleInstall queues one captured status check. Polls arriving while that
// request waits or runs do not add duplicates to the ordered effect lane.
func (m *Model) settleInstall() {
	install := m.launchHint.install
	if install == nil {
		return
	}
	for _, job := range append([]*effectJob{m.effects.active}, m.effects.pending...) {
		if job == nil {
			continue
		}
		if request, ok := job.request.(installSettleRequest); ok && request.install.sessionID == install.sessionID {
			return
		}
	}
	m.enqueueEffect(installSettleRequest{install: *install}, 0, false)
}

func (m *Model) viewLaunchHint() string {
	width := m.cardWidth()
	inner := cardInnerWidth(width)
	tone := lipgloss.NewStyle().Foreground(colorAccent).Bold(true)

	var body strings.Builder
	for i, paragraph := range strings.Split(m.launchHint.fix.text, "\n\n") {
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
	if m.launchHint.fix.command != "" {
		hint = [][2]string{{"i", "install"}, {"c", "copy"}, {"esc", "close"}}
	}
	return m.cardSized(width, "◈ Session needs a setup step", strings.TrimRight(body.String(), "\n"), hint)
}
