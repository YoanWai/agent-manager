package ui

import (
	"errors"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/deps"
	"github.com/YoanWai/agent-manager/internal/mcpreg"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/ui/composer"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uilaunchhint "github.com/YoanWai/agent-manager/internal/ui/launchhint"
	uiinstall "github.com/YoanWai/agent-manager/internal/ui/launchhint/install"
	uiquick "github.com/YoanWai/agent-manager/internal/ui/quick"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// installRoot is the root as the install runtime reaches it.
type installRoot struct{ *Model }

func (r installRoot) ReportErr(text string) { r.reportErr(text) }

func (r installRoot) ReportDone(text string) { r.reportDone(text) }

// SessionLaunched adds the installer shell to the loaded list the way a
// spawn does, so the screen shows it on the completion rather than the poll
// after it.
func (r installRoot) SessionLaunched(sess store.Session) {
	r.markFreshPane(sess.ID)
	r.recordLaunch(sess.ID)
	for _, known := range r.workspace.sessions {
		if known.ID == sess.ID {
			return
		}
	}
	r.workspace.sessions = append(r.workspace.sessions, sess)
	r.rebuildRows()
}

func (r installRoot) HintOpen(gen uint64) bool {
	_, open := r.launchHintSurface()
	return open && r.gens.dialog == gen
}

func (r installRoot) ShowInstall(sessionID string) tea.Cmd {
	return r.handInstallScreenTo(sessionID)
}

func (r installRoot) Refresh() tea.Cmd { return r.refreshCmd() }

// QueueRetry queues the launch the fix held. Every request a refused launch
// carries is a root effect request.
func (r installRoot) QueueRetry(retry uiinstall.Retry) {
	r.enqueueEffect(retry.(effectRequest), 0, false)
}

func (m *Model) pendingInstall() *uiinstall.Pending { return m.install.Pending }

// startInstall admits the setup dialog's install onto the effect lane,
// stamped with the dialog generation, a new shell row id, the shell tool, the
// placement group and the pane geometry. It refuses while another install is
// still starting.
func (m *Model) startInstall(request installStartRequest) bool {
	for _, job := range append([]*uieffects.Job{m.effects.Main.Active}, m.effects.Main.Pending...) {
		if job == nil {
			continue
		}
		if starting, ok := job.Request.(installStartRequest); ok {
			m.reportErr("an install is already starting for " + starting.request.Binary)
			return false
		}
	}
	toolName, _ := m.shellTool()
	w, h := m.paneTargetSize()
	request.request.Gen = m.gens.dialog
	request.request.ID = newID()
	request.request.ToolName = toolName
	request.request.Group = m.contextGroup()
	request.request.Pane = sessioncmd.PaneSize{Width: w, Height: h}
	m.enqueueEffect(request, 0, false)
	return true
}

func (m *Model) handInstallScreenTo(sessionID string) tea.Cmd {
	var cleanup tea.Cmd
	if q := m.quick(); q != nil {
		cleanup = uiinstall.CleanupImages(q.Attachments)
		q.Attachments = nil
	}
	m.closeLaunchHint()
	m.rail.ClearStatusFilter()
	m.focusSession(sessionID)
	return cleanup
}

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

// reportLaunchError routes a failed spawn to the right surface: a launch
// the manager refused for a missing prerequisite opens a dialog naming the
// command that unblocks it, anything else stays a line of status text.
// A retry, when one exists, is attached by the typed effect completion that
// failed; this error router never carries or invokes a Model closure.
func (m *Model) reportLaunchError(err error) {
	var hermesMCP mcpreg.HermesMCPUnavailableError
	if errors.As(err, &hermesMCP) {
		m.openLaunchHint(uiinstall.Fix{
			Text:    uilaunchhint.HermesText(hermesMCP.PipCommand),
			Command: hermesMCP.PipCommand,
			Script:  hermesMCP.PipCommand,
			Binary:  "hermes",
		})
		return
	}
	var missing config.MissingToolError
	if errors.As(err, &missing) {
		m.openLaunchHint(uiinstall.Fix{
			Text:    uilaunchhint.MissingText(missingTool(missing), deps.Hint(missing.Binary)),
			Command: deps.Shown(missing.Binary),
			Script:  deps.Command(missing.Binary),
			Binary:  missing.Binary,
		})
		return
	}
	var outdated config.OutdatedToolError
	if errors.As(err, &outdated) {
		m.openLaunchHint(uiinstall.Fix{
			Text:    uilaunchhint.OutdatedText(outdatedTool(outdated), outdatedToolCommand(outdated)),
			Command: outdatedToolCommand(outdated),
			Script:  outdatedToolCommand(outdated),
			Binary:  outdated.Binary,
			Update:  true,
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
func (m *Model) openLaunchHint(fix uiinstall.Fix) {
	m.advanceDialogGen()
	fix.Images = append(fix.Images, m.takeComposerImages()...)
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
func (m *Model) presentLaunchHint(fix uiinstall.Fix) {
	m.install.Launch = fix
	if s, open := m.launchHintSurface(); open {
		s.SetFix(m.launchHintCard(fix))
		return
	}
	m.push(uidialogs.NewLaunchHintSurface(launchHintRoot{m}, m.launchHintCard(fix), m.glyphs(),
		lipgloss.NewStyle().Foreground(colorAccent()).Bold(true), mutedStyle()))
}

// launchHintCard is the copy of the fix the card paints. The launch to retry,
// the images and the install stay on the runtime state.
func (m *Model) launchHintCard(fix uiinstall.Fix) uidialogs.LaunchFix {
	return uidialogs.LaunchFix{Text: fix.Text, Command: fix.Command, Update: fix.Update}
}

// launchHintFix is the fix the open card holds, for a completion that attaches
// the launch to retry to it.
func (m *Model) launchHintFix() (*uiinstall.Fix, bool) {
	if _, ok := m.launchHintSurface(); !ok {
		return nil, false
	}
	return &m.install.Launch, true
}

// settleInstall queues one captured status check. Polls arriving while that
// request waits or runs do not add duplicates to the ordered effect lane.
func (m *Model) settleInstall() {
	install := m.install.Pending
	if install == nil {
		return
	}
	for _, job := range append([]*uieffects.Job{m.effects.Main.Active}, m.effects.Main.Pending...) {
		if job == nil {
			continue
		}
		if request, ok := job.Request.(installSettleRequest); ok && request.install.Pending.SessionID == install.SessionID {
			return
		}
	}
	m.enqueueEffect(installSettleRequest{install: uiinstall.SettleRequest{Pending: *install}}, 0, false)
}
