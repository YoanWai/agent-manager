package ui

import (
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/ui/composer"
	uilink "github.com/YoanWai/agent-manager/internal/ui/connections/link"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uiapply "github.com/YoanWai/agent-manager/internal/ui/dialogs/apply"
	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	uiinstall "github.com/YoanWai/agent-manager/internal/ui/launchhint/install"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	uilaunch "github.com/YoanWai/agent-manager/internal/ui/spawn/launch"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The small dialogs keep their state, keys and view in internal/ui/dialogs.
// The hosts here answer what their surface asks and run the request it holds;
// the adapter beside this file seeds them from the workspace and routes.

type confirmTarget struct {
	isGroup bool
	// archivedOnly marks a group delete issued from the archived view,
	// which clears the group's archive instead of the group itself.
	archivedOnly bool
	path         string
	label        string
	sessions     []store.Session
	action       uidialogs.ConfirmAction
	batch        bool
	selection    lifecycleSelection
	connection   string
	// remote is a call on an SSH row, run on the host once confirmed.
	remote *uilink.Call
}

type lifecycleSelectionKind uint8

const (
	lifecycleSelectionNone lifecycleSelectionKind = iota
	lifecycleSelectionSession
	lifecycleSelectionGroup
)

type lifecycleSelection struct {
	kind         lifecycleSelectionKind
	rootID       string
	archivedOnly bool
}

// confirmRoot is the root as the confirm card reaches it, holding the
// request the card was opened for.
type confirmRoot struct {
	*Model
	target confirmTarget
}

func (r confirmRoot) Paint(title, question, consequence string, destructive bool, answer string) string {
	return r.confirmCard(title, question, consequence, destructive, answer)
}

// Run runs a copy of the captured rows, so a later edit of the slice the
// opener passed cannot retarget it.
func (r confirmRoot) Run() tea.Cmd {
	target := r.target
	target.sessions = append(target.sessions[:0:0], target.sessions...)
	return r.runConfirmed(target)
}

// forkRoot is the root as the fork card reaches it, holding the source row
// the fork request carries.
type forkRoot struct {
	*Model
	source store.Session
}

func (r forkRoot) ReportErr(text string) { r.reportErr(text) }

func (r forkRoot) ClearErr() { r.clearErr() }

func (r forkRoot) Submit(name string, gen int) tea.Cmd {
	r.queueFork(uiapply.ForkEffect{ForkRequest: uiapply.ForkRequest{Source: r.source, Name: name, Gen: gen}})
	return r.nextEffectCmd()
}

func (r forkRoot) Paint(form *uidialogs.Fork) string { return r.viewFork(form) }

// renameRoot is the root as the rename row reaches it. The request a submit
// validated stays here: a RenameEffect names the session row and the effect
// lane it runs on, so the row hands over the values and the root builds it.
type renameRoot struct{ *Model }

func (r renameRoot) ReportErr(text string) { r.reportErr(text) }

func (r renameRoot) ApplyPathSuggestion() tea.Cmd { return r.applyPathSuggestion() }

func (r renameRoot) StepBase(s *uidialogs.RenameSurface, delta int) (string, tea.Cmd) {
	return r.stepGroupBase(uilaunch.BaseRename, r.gens.dialog, r.capturedAbsolutePath(s.Dir(), r.capturedGroupDefaultDir(uirail.ParentGroup(s.Path()))), s.Base, delta)
}

func (r renameRoot) WorktreeCount() int { return len(uidialogs.WorktreeOptions) }

func (r renameRoot) QueueRename(request uidialogs.RenameRequest, base string) tea.Cmd {
	if request.Group {
		return r.queueRename(uiapply.RenameEffect{Request: uiapply.Request{
			Kind:     uiapply.Group,
			OldGroup: request.OldGroup,
			NewGroup: request.NewGroup,
			RawDir:   request.Dir,
			DraftDir: request.Dir,
			Worktree: uidialogs.WorktreeValue(request.Worktree),
			Base:     base,
			Name:     request.Name,
		}})
	}
	return r.queueRename(uiapply.RenameEffect{Request: uiapply.Request{
		Kind:   uiapply.Session,
		SessID: request.SessionID,
		Name:   request.Name,
		Tool:   request.Tool,
	}})
}

// moveRoot is the root as the move card reaches it. The picker the card
// holds is the root's to paint and to read back on a submit.
type moveRoot struct{ *Model }

func (r moveRoot) PlaceMove(move uidialogs.Move, picker *spawn.Picker) tea.Cmd {
	return r.placeMove(move, picker)
}

func (r moveRoot) PaintMove(picker *spawn.Picker) string {
	return r.CardSized(r.cardWidth(), uidialogs.MoveTitle(r.prefs.glyphMode), r.viewGroupPicker(picker), uidialogs.MoveHints())
}

// groupRoot is the root as the New Group card reaches it.
type groupRoot struct{ *Model }

func (r groupRoot) CardWidth(cols int) int { return uiframe.CardWidthFor(cols) }

func (r groupRoot) ReportErr(text string) { r.reportErr(text) }

// launchHintRoot is the root as the setup-step card reaches it. The fix it
// paints, the launch to retry and the images the refused prompt named all
// outlive the card, so they live in the install runtime state.
type launchHintRoot struct{ *Model }

func (r launchHintRoot) StartInstall() bool {
	if install := r.ledger.install.Pending; install != nil {
		r.reportErr("an install is already running in " + install.Name)
		return false
	}
	if !r.startInstall(r.launchInstall()) {
		return false
	}
	// The install owns the images once it is admitted.
	r.ledger.install.Launch.Images = nil
	return true
}

func (r launchHintRoot) CopyLaunchCommand() tea.Cmd { return r.ledger.install.Copy() }

func (r launchHintRoot) CardWidth() int { return r.cardWidth() }

func (r launchHintRoot) CardChromeRows(hint [][2]string) int { return r.cardChromeRows(hint) }

func (r launchHintRoot) ReleaseLaunchHint() { r.ledger.install.Release() }

var (
	_ uidialogs.ConfirmRoot    = confirmRoot{}
	_ uidialogs.ForkRoot       = forkRoot{}
	_ uidialogs.RenameRoot     = renameRoot{}
	_ uidialogs.MoveRoot       = moveRoot{}
	_ uidialogs.GroupFormRoot  = groupRoot{}
	_ uidialogs.LaunchHintRoot = launchHintRoot{}
)

// runConfirmed is the answered yes: the remote call, the connection removal
// or the lifecycle request the card held.
func (m *Model) runConfirmed(target confirmTarget) tea.Cmd {
	switch {
	case target.remote != nil:
		return m.queueRemote(*target.remote)
	case target.connection != "":
		m.removeConnection(target.connection)
	default:
		m.queueLifecycle(target, false, "")
	}
	return m.nextEffectCmd()
}

// confirmCard is the shared yes/no card, also drawn by Settings' reset.
func (m *Model) confirmCard(title, question, consequence string, destructive bool, answer string) string {
	width := m.cardWidth()
	tone := lipgloss.NewStyle().Foreground(uitheme.ColorAccent()).Bold(true)
	if destructive {
		tone = uitheme.ErrStyle()
	}
	body := uidialogs.ConfirmBody(question, consequence, uiframe.CardInnerWidth(width), tone, uitheme.MutedStyle())
	return m.CardSized(width, title, body, uidialogs.ConfirmHints(answer))
}

// closeLaunchHint takes the card down once its install has the screen,
// without dropping the images the install now owns.
func (m *Model) closeLaunchHint() {
	m.ledger.install.Launch = uiinstall.Fix{}
	m.clearSurfaces()
	m.base = baseList
}

func (m *Model) releaseLaunchHint() { m.ledger.install.Release() }

// launchInstall is the install the card's fix asks for, carrying the launch to
// retry and the images that launch's prompt named.
func (m *Model) launchInstall() uiinstall.StartRequest {
	fix := m.ledger.install.Launch
	retry := fix.Retry
	if spawn, ok := retry.(uilaunch.Request); ok && len(spawn.Images) == 0 {
		spawn.Images = append([]composer.Attachment(nil), fix.Images...)
		retry = spawn
	}
	return uiinstall.StartRequest{
		Command: fix.Script,
		Binary:  fix.Binary,
		Retry:   retry,
		Images:  fix.Images,
	}
}
