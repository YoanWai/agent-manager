package ui

import (
	"github.com/YoanWai/agent-manager/internal/store"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
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
	r.queueFork(forkRequest{source: r.source, name: name, gen: gen})
	return r.nextEffectCmd()
}

func (r forkRoot) Paint(form *uidialogs.Fork) string { return r.viewFork(form) }

// listClear is the list with no card over it: what "mode is the list" meant
// while the cards were modes. The quick bar and a resize sit on the list
// without covering it.
func (m *Model) listClear() bool {
	if m.base != baseList {
		return false
	}
	for _, s := range m.surfaces {
		switch s.(type) {
		case *quickSurface, *resizeSurface:
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

func newForkSurface(m *Model, source store.Session, gen int) *uidialogs.ForkSurface {
	return uidialogs.NewForkSurface(forkRoot{m, source}, source.Name, source.Group, gen)
}

// launchHintRoot is the root as the setup-step card reaches it. The fix it
// paints, the launch to retry and the images the refused prompt named all
// outlive the card, so they live on the launch ledger.
type launchHintRoot struct{ *Model }

func (r launchHintRoot) StartInstall() bool {
	if install := r.ledger.install; install != nil {
		r.reportErr("an install is already running in " + install.name)
		return false
	}
	if !r.startInstall(r.launchInstall()) {
		return false
	}
	// The install owns the images once it is admitted.
	r.ledger.launch.images = nil
	return true
}

func (r launchHintRoot) CopyLaunchCommand() tea.Cmd {
	command := r.ledger.launch.command
	return func() tea.Msg {
		return launchCommandCopiedMsg{err: copyLaunchCommand(command)}
	}
}

func (r launchHintRoot) CardWidth() int { return r.cardWidth() }

func (r launchHintRoot) CardChromeRows(hint [][2]string) int { return r.cardChromeRows(hint) }

func (r launchHintRoot) ReleaseLaunchHint() { r.releaseLaunchHint() }

// closeLaunchHint takes the card down once its install has the screen,
// without dropping the images the install now owns.
func (m *Model) closeLaunchHint() {
	m.ledger.launch = launchFix{}
	m.clearSurfaces()
	m.base = baseList
}

func (m *Model) releaseLaunchHint() {
	dropImages(m.ledger.launch.images)
	m.ledger.launch = launchFix{}
}

// launchInstall is the install the card's fix asks for, carrying the launch to
// retry and the images that launch's prompt named.
func (m *Model) launchInstall() installStartRequest {
	fix := m.ledger.launch
	retry := fix.effectRetry
	if spawn, ok := retry.(spawnRequest); ok && len(spawn.images) == 0 {
		spawn.images = append([]imageAttachment(nil), fix.images...)
		retry = spawn
	}
	return installStartRequest{
		command:     fix.script,
		binary:      fix.binary,
		effectRetry: retry,
		images:      fix.images,
	}
}

// launchHintSurface is the setup-step card on the stack.
func (m *Model) launchHintSurface() (*uidialogs.LaunchHintSurface, bool) {
	return surfaceOf[*uidialogs.LaunchHintSurface](m)
}

var _ uidialogs.LaunchHintRoot = launchHintRoot{}
