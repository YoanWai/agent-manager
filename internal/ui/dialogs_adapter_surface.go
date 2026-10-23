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
