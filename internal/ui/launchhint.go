package ui

import (
	"errors"
	"os"
	"strings"

	"github.com/YoanWai/agent-manager/internal/clipboard"
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/deps"
	"github.com/YoanWai/agent-manager/internal/mcpreg"
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

type launchHintState struct {
	fix launchFix
	// install is the setup-dialog install still running in a shell tab,
	// nil when none is.
	install *pendingInstall
}

// launchHintDialog holds a refused launch's fix: the text, the command
// that unblocks it, the images the refused prompt named and the install it
// started. It owns its keys, view and the install request it builds. The
// root routes launch errors into it, starts that install on the effect lane,
// and releases the mouse while it is open (syncMouseCapture keys
// mouse.released on modeLaunchHint) so a drag selects the command.
type launchHintDialog struct{ launchHintState }

// launchHintHost is what the setup dialog reaches on the root.
type launchHintHost interface {
	requestQuit() (tea.Model, tea.Cmd)
	setMode(next mode)
	advanceDialogGen()
	takeComposerImages() []imageAttachment
	reportErr(text string)
	startInstall(request installStartRequest) bool
	cardWidth() int
	cardSized(width int, title, body string, hint [][2]string) string
}

var _ launchHintHost = (*Model)(nil)

// advanceDialogGen fences completions submitted from the dialog it
// replaces.
func (m *Model) advanceDialogGen() {
	m.gens.dialog++
}

// takeComposerImages hands over the images the spawn form's prompt and the
// quick bar hold, leaving both composers empty.
func (m *Model) takeComposerImages() []imageAttachment {
	var images []imageAttachment
	images = append(images, m.form.prompt.attachments...)
	images = append(images, m.quick.attachments...)
	m.form.prompt.attachments = nil
	m.quick.attachments = nil
	return images
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
		m.launchHint.open(m, launchFix{
			text: "Hermes sessions carry the agent-manager MCP tools, and this Hermes cannot load them: its MCP SDK is not installed.\n\n" +
				step,
			command: hermesMCP.PipCommand,
			binary:  "hermes",
		})
		return
	}
	var missing config.MissingToolError
	if errors.As(err, &missing) {
		m.launchHint.open(m, launchFix{
			text:    missingToolText(missing),
			command: deps.Command(missing.Binary),
			binary:  missing.Binary,
		})
		return
	}
	m.reportErr(err.Error())
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

// open takes the refused prompt's images out of the composers:
// the prompt text already names their paths, and the dialog owns the
// files until the launch runs or is given up, so the form and the quick
// bar can be reopened meanwhile.
func (d *launchHintDialog) open(h launchHintHost, fix launchFix) {
	h.advanceDialogGen()
	fix.images = append(fix.images, h.takeComposerImages()...)
	d.fix = fix
	h.setMode(modeLaunchHint)
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

func (d *launchHintDialog) handleKey(h launchHintHost, msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+c":
		_, cmd := h.requestQuit()
		return cmd
	case "c":
		if d.fix.command == "" {
			return nil
		}
		command := d.fix.command
		return func() tea.Msg {
			return launchCommandCopiedMsg{err: copyLaunchCommand(command)}
		}
	case "i":
		if d.fix.command == "" {
			return nil
		}
		d.startInstall(h)
		return nil
	case "esc", "q", "enter":
		d.close(h)
	}
	return nil
}

// close drops the dialog and the images the refused prompt was
// holding: they stayed alive while an install could still spawn it.
func (d *launchHintDialog) close(h launchHintHost) {
	dropImages(d.fix.images)
	d.fix = launchFix{}
	h.setMode(modeList)
}

func (m *Model) handleLaunchCommandCopied(msg launchCommandCopiedMsg) {
	if msg.err != nil {
		m.reportErr("copy failed: " + msg.err.Error())
		return
	}
	m.reportDone("copied to clipboard")
}

// startInstall captures the setup dialog's install and hands it to the
// root, which queues its filesystem, store, lifecycle, and tmux work. The
// dialog stays in front until that job says the command was typed into a
// durable shell row; the images move to the request once it is admitted.
func (d *launchHintDialog) startInstall(h launchHintHost) {
	if d.install != nil {
		h.reportErr("an install is already running in " + d.install.name)
		return
	}
	fix := d.fix
	retry := fix.effectRetry
	if spawn, ok := retry.(spawnRequest); ok && len(spawn.images) == 0 {
		spawn.images = append([]imageAttachment(nil), fix.images...)
		retry = spawn
	}
	if h.startInstall(installStartRequest{
		command:     fix.command,
		binary:      fix.binary,
		effectRetry: retry,
		images:      fix.images,
	}) {
		d.fix.images = nil
	}
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
	for _, job := range append([]*effectJob{m.effects.main.active}, m.effects.main.pending...) {
		if job == nil {
			continue
		}
		if request, ok := job.request.(installSettleRequest); ok && request.install.sessionID == install.sessionID {
			return
		}
	}
	m.enqueueEffect(installSettleRequest{install: *install}, 0, false)
}

func (d *launchHintDialog) view(h launchHintHost) string {
	width := h.cardWidth()
	inner := cardInnerWidth(width)
	tone := lipgloss.NewStyle().Foreground(colorAccent).Bold(true)

	var body strings.Builder
	for i, paragraph := range strings.Split(d.fix.text, "\n\n") {
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
	if d.fix.command != "" {
		hint = [][2]string{{"i", "install"}, {"c", "copy"}, {"esc", "close"}}
	}
	return h.cardSized(width, "◈ Session needs a setup step", strings.TrimRight(body.String(), "\n"), hint)
}
