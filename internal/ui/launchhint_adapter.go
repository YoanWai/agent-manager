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
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uilaunchhint "github.com/YoanWai/agent-manager/internal/ui/launchhint"
	uiquick "github.com/YoanWai/agent-manager/internal/ui/quick"
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

// pendingInstall is the install still running in a shell tab: the session
// running it, the files its script writes and runs from, the binary it puts on
// PATH, and the launch to finish once its exit status lands. It outlives the
// card that started it, so the ledger holds it.
type pendingInstall struct {
	sessionID   string
	name        string
	binary      string
	statusFile  string
	script      string
	effectRetry effectRequest
	images      []composer.Attachment
}

func (m *Model) pendingInstall() *pendingInstall { return m.ledger.install }

// advanceDialogGen fences completions submitted from the dialog it
// replaces.
func (m *Model) advanceDialogGen() {
	m.gens.dialog++
}

// takeComposerImages hands over the images the spawn form's prompt and the
// quick bar hold, leaving both composers empty.
func (m *Model) takeComposerImages() []composer.Attachment {
	var images []composer.Attachment
	if prompt := m.formPromptFor(); prompt != nil {
		images = append(images, prompt.Attachments...)
		prompt.Attachments = nil
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
		m.openLaunchHint(launchFix{
			text:    uilaunchhint.HermesText(hermesMCP.PipCommand),
			command: hermesMCP.PipCommand,
			script:  hermesMCP.PipCommand,
			binary:  "hermes",
		})
		return
	}
	var missing config.MissingToolError
	if errors.As(err, &missing) {
		m.openLaunchHint(launchFix{
			text:    uilaunchhint.MissingText(missingTool(missing), deps.Hint(missing.Binary)),
			command: deps.Shown(missing.Binary),
			script:  deps.Command(missing.Binary),
			binary:  missing.Binary,
		})
		return
	}
	var outdated config.OutdatedToolError
	if errors.As(err, &outdated) {
		m.openLaunchHint(launchFix{
			text:    uilaunchhint.OutdatedText(outdatedTool(outdated), outdatedToolCommand(outdated)),
			command: outdatedToolCommand(outdated),
			script:  outdatedToolCommand(outdated),
			binary:  outdated.Binary,
			update:  true,
		})
		return
	}
	m.reportErr(err.Error())
}

// outdatedToolCommand is the installer line a too-old CLI updates with, empty
// when the manager ships none and the user updates the way they installed it.
func outdatedToolCommand(outdated config.OutdatedToolError) string {
	if outdated.ManagedExternally {
		return ""
	}
	return deps.Command(outdated.Binary)
}

// outdatedToolText is the copy the spawn form shows for a too-old CLI.
func outdatedToolText(outdated config.OutdatedToolError) string {
	return uilaunchhint.OutdatedText(outdatedTool(outdated), outdatedToolCommand(outdated))
}

func missingTool(missing config.MissingToolError) uilaunchhint.Tool {
	return uilaunchhint.Tool{Binary: missing.Binary, WindowsPath: missing.WindowsPath}
}

func outdatedTool(outdated config.OutdatedToolError) uilaunchhint.Tool {
	return uilaunchhint.Tool{
		Binary:    outdated.Binary,
		Installed: outdated.Installed,
		Minimum:   outdated.Minimum,
	}
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
			if _, bar := top.(*uiquick.Surface); bar {
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
		if att.Path != "" {
			_ = os.Remove(att.Path)
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
	for _, job := range append([]*uieffects.Job{m.effects.Main.Active}, m.effects.Main.Pending...) {
		if job == nil {
			continue
		}
		if request, ok := job.Request.(installSettleRequest); ok && request.install.sessionID == install.sessionID {
			return
		}
	}
	m.enqueueEffect(installSettleRequest{install: *install}, 0, false)
}
