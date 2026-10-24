package ui

import (
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/ui/composer"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uiapply "github.com/YoanWai/agent-manager/internal/ui/dialogs/apply"
	uigeometry "github.com/YoanWai/agent-manager/internal/ui/geometry"
	uiinstall "github.com/YoanWai/agent-manager/internal/ui/launchhint/install"
	uiquick "github.com/YoanWai/agent-manager/internal/ui/quick"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	uilaunch "github.com/YoanWai/agent-manager/internal/ui/spawn/launch"
	tea "github.com/charmbracelet/bubbletea"
)

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
	r.queueFork(forkRequest{ForkRequest: uiapply.ForkRequest{Source: r.source, Name: name, Gen: gen}})
	return r.nextEffectCmd()
}

func (r forkRoot) Paint(form *uidialogs.Fork) string { return r.viewFork(form) }

// renameRoot is the root as the rename row reaches it. The request a submit
// validated stays here: a renameRequest names the session row and the effect
// lane it runs on, so the row hands over the values and the root builds it.
type renameRoot struct{ *Model }

func (r renameRoot) ReportErr(text string) { r.reportErr(text) }

func (r renameRoot) ApplyPathSuggestion() tea.Cmd { return r.applyPathSuggestion() }

func (r renameRoot) StepBase(s *uidialogs.RenameSurface, delta int) (string, tea.Cmd) {
	return r.stepGroupBase(uilaunch.BaseRename, r.gens.dialog, r.capturedAbsolutePath(s.Dir(), r.capturedGroupDefaultDir(parentGroup(s.Path()))), s.Base, delta)
}

func (r renameRoot) WorktreeCount() int { return len(groupWorktreeOptions) }

func (r renameRoot) QueueRename(request uidialogs.RenameRequest, base string) tea.Cmd {
	if request.Group {
		return r.queueRename(renameRequest{Request: uiapply.Request{
			Kind:     uiapply.Group,
			OldGroup: request.OldGroup,
			NewGroup: request.NewGroup,
			RawDir:   request.Dir,
			DraftDir: request.Dir,
			Worktree: groupWorktreeValue(request.Worktree),
			Base:     base,
			Name:     request.Name,
		}})
	}
	return r.queueRename(renameRequest{Request: uiapply.Request{
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
	return r.card(uidialogs.MoveTitle(r.prefs.glyphMode), r.viewGroupPicker(picker), uidialogs.MoveHints())
}

var (
	_ uidialogs.RenameRoot = renameRoot{}
	_ uidialogs.MoveRoot   = moveRoot{}
)

// listClear is the list with no card over it: what "mode is the list" meant
// while the cards were modes. The quick bar and a resize sit on the list
// without covering it.
func (m *Model) listClear() bool {
	if m.base != baseList {
		return false
	}
	for _, s := range m.surfaces {
		switch s.(type) {
		case *uiquick.Surface, *uigeometry.Surface:
		default:
			return false
		}
	}
	return true
}

func (m *Model) openConfirm(target confirmTarget) {
	prompt := uidialogs.Confirm{Action: target.action, Group: target.isGroup, Label: target.label}
	m.push(uidialogs.NewConfirmSurface(confirmRoot{m, target}, prompt, m.prefs.glyphMode))
}

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
func (m *Model) launchInstall() installStartRequest {
	fix := m.ledger.install.Launch
	retry := fix.Retry
	if spawn, ok := retry.(spawnRequest); ok && len(spawn.images) == 0 {
		spawn.images = append([]composer.Attachment(nil), fix.Images...)
		retry = spawn
	}
	return installStartRequest{request: uiinstall.StartRequest{
		Command: fix.Script,
		Binary:  fix.Binary,
		Retry:   retry,
		Images:  fix.Images,
	}}
}

var _ uidialogs.LaunchHintRoot = launchHintRoot{}
