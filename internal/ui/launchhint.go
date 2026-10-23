package ui

import (
	"errors"
	"os"

	"github.com/YoanWai/agent-manager/internal/clipboard"
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/deps"
	"github.com/YoanWai/agent-manager/internal/mcpreg"
	"github.com/YoanWai/agent-manager/internal/ui/composer"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	"github.com/charmbracelet/lipgloss"
)

// launchFix is what the setup-step dialog holds: the text it shows, the
// command it prints and copies, the script i runs to unblock the launch,
// the binary that script puts on PATH, the launch to run again once it is
// there, and the images that launch's prompt names. command is empty when
// the manager knows no recipe, which leaves the dialog read-only. update
// marks a CLI that is installed but too old, where the same installer
// updates it.
type launchFix struct {
	text        string
	command     string
	script      string
	binary      string
	update      bool
	effectRetry effectRequest
	images      []composer.Attachment
}

// pendingInstall is an install the dialog started in a shell tab: the
// session running it, the files its script writes and runs from, and the
// launch to finish once the command's exit status lands. It outlives the
// dialog, which closes once the command is typed, so the ledger holds it.
type pendingInstall struct {
	sessionID   string
	name        string
	binary      string
	statusFile  string
	script      string
	effectRetry effectRequest
	images      []composer.Attachment
}

// advanceDialogGen fences completions submitted from the dialog it
// replaces.
// pendingInstall is the install still running in a shell tab, nil when
// none is. It outlives the card that started it.
func (m *Model) pendingInstall() *pendingInstall { return m.ledger.install }

func (m *Model) advanceDialogGen() {
	m.gens.dialog++
}

// takeComposerImages hands over the images the spawn form's prompt and the
// quick bar hold, leaving both composers empty.
func (m *Model) takeComposerImages() []composer.Attachment {
	var images []composer.Attachment
	if f := m.form(); f != nil {
		images = append(images, f.prompt.Attachments...)
		f.prompt.Attachments = nil
	}
	if q := m.quick(); q != nil {
		images = append(images, q.Attachments...)
		q.Attachments = nil
	}
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
		m.openLaunchHint(launchFix{
			text: "Hermes sessions carry the agent-manager MCP tools, and this Hermes cannot load them: its MCP SDK is not installed.\n\n" +
				step,
			command: hermesMCP.PipCommand,
			script:  hermesMCP.PipCommand,
			binary:  "hermes",
		})
		return
	}
	var missing config.MissingToolError
	if errors.As(err, &missing) {
		m.openLaunchHint(launchFix{
			text:    missingToolText(missing),
			command: deps.Shown(missing.Binary),
			script:  deps.Command(missing.Binary),
			binary:  missing.Binary,
		})
		return
	}
	var outdated config.OutdatedToolError
	if errors.As(err, &outdated) {
		m.openLaunchHint(launchFix{
			text:    outdatedToolText(outdated),
			command: outdatedToolCommand(outdated),
			script:  outdatedToolCommand(outdated),
			binary:  outdated.Binary,
			update:  true,
		})
		return
	}
	m.reportErr(err.Error())
}

func outdatedToolCommand(outdated config.OutdatedToolError) string {
	if outdated.ManagedExternally {
		return ""
	}
	return deps.Command(outdated.Binary)
}

func outdatedToolText(outdated config.OutdatedToolError) string {
	head := outdated.Binary + " " + outdated.Installed + " is older than " + outdated.Minimum +
		", the oldest version Agent Manager supports."
	if command := outdatedToolCommand(outdated); command != "" {
		return head + "\n\nUpdate it with: " + command
	}
	return head + "\n\nUpdate it the way you installed it, then spawn again."
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

// openLaunchHint shows a refusal and takes the refused prompt's images out
// of the composers: the prompt text already names their paths, and the
// dialog owns the files until the launch runs or is given up, so the form
// and the quick bar can be reopened meanwhile. A refused launch replaces
// whatever dialog it came from.
func (m *Model) openLaunchHint(fix launchFix) {
	m.advanceDialogGen()
	fix.images = append(fix.images, m.takeComposerImages()...)
	if _, open := m.launchHintSurface(); !open {
		// The quick bar is not a dialog: it stays under the card.
		for top, ok := m.top(); ok; top, ok = m.top() {
			if _, bar := top.(*quickSurface); bar {
				break
			}
			m.Pop()
		}
		m.base = baseList
	}
	m.presentLaunchHint(fix)
}

// presentLaunchHint shows a refusal that is not a spawn over the screen it
// came from, so a draft prompt keeps its images. A refusal already showing
// is replaced in place.
func (m *Model) presentLaunchHint(fix launchFix) {
	m.ledger.launch = fix
	if s, open := m.launchHintSurface(); open {
		s.SetFix(m.launchHintCard(fix))
		return
	}
	m.push(uidialogs.NewLaunchHintSurface(launchHintRoot{m}, m.launchHintCard(fix), m.glyphs(),
		lipgloss.NewStyle().Foreground(colorAccent()).Bold(true), mutedStyle()))
}

// launchHintCard is the copy of the fix the card paints. The launch to retry,
// the images and the install stay on the ledger.
func (m *Model) launchHintCard(fix launchFix) uidialogs.LaunchFix {
	return uidialogs.LaunchFix{Text: fix.text, Command: fix.command, Update: fix.update}
}

// launchHintFix is the fix the open card holds, for a completion that attaches
// the launch to retry to it.
func (m *Model) launchHintFix() (*launchFix, bool) {
	if _, ok := m.launchHintSurface(); !ok {
		return nil, false
	}
	return &m.ledger.launch, true
}

func removeInstallFiles(statusFile, script string) {
	_ = os.Remove(statusFile)
	_ = os.Remove(script)
}

func dropImages(images []composer.Attachment) {
	for _, att := range images {
		if att.path != "" {
			_ = os.Remove(att.path)
		}
	}
}

func (m *Model) handleLaunchCommandCopied(msg launchCommandCopiedMsg) {
	if msg.err != nil {
		m.reportErr("copy failed: " + msg.err.Error())
		return
	}
	m.reportDone("copied to clipboard")
}

// settleInstall queues one captured status check. Polls arriving while that
// request waits or runs do not add duplicates to the ordered effect lane.
func (m *Model) settleInstall() {
	install := m.ledger.install
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
