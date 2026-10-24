package ui

import (
	"fmt"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	uirailpersist "github.com/YoanWai/agent-manager/internal/ui/rail/persist"
	uilifecycle "github.com/YoanWai/agent-manager/internal/ui/session/lifecycle"
)

// Legacy fixture entry points drive the same captured executor and reconciliation
// as the UI. Production handlers always return a command instead of running it.
func (m *Model) lifecycleFixture(target confirmTarget) (uilifecycle.Result, error) {
	w, h := m.paneTargetSize()
	request := uilifecycle.Request{Target: target.lifecycle, Pane: sessioncmd.PaneSize{Width: w, Height: h}, Watched: m.focus.runtime.Watching()}
	request.Target.Sessions = append([]store.Session(nil), target.lifecycle.Sessions...)
	completed, err := m.bindEffect(request)()
	result := completed.(uilifecycle.Result)
	m.applyLifecycleEffect(request, result, err)
	if err == nil {
		err = result.Warning
	}
	return result, err
}

func (m *Model) archiveConfirmed() error {
	_, err := m.lifecycleFixture(pendingConfirm(m))
	return err
}

func (m *Model) restoreConfirmed() error {
	_, err := m.lifecycleFixture(pendingConfirm(m))
	return err
}

func (m *Model) deleteConfirmed() (sessioncmd.DeleteResult, error) {
	result, err := m.lifecycleFixture(pendingConfirm(m))
	return result.Deleted, err
}

func (m *Model) killSession(sess store.Session) error {
	_, err := m.lifecycleFixture(confirmTarget{prompt: uidialogs.Confirm{Action: uidialogs.ConfirmKill}, lifecycle: uilifecycle.Target{Action: uilifecycle.Action(uidialogs.ConfirmKill), Sessions: []store.Session{sess}}})
	return err
}

func (m *Model) restartSession(sess store.Session) error {
	_, err := m.lifecycleFixture(confirmTarget{prompt: uidialogs.Confirm{Action: uidialogs.ConfirmRestart}, lifecycle: uilifecycle.Target{Action: uilifecycle.Action(uidialogs.ConfirmRestart), Sessions: []store.Session{sess}}})
	return err
}

func (m *Model) reviveSession(sess store.Session) error {
	if m.services.tmux.Exists(sess.ID) {
		return fmt.Errorf("session %s is still running; revive only applies to dead sessions", sess.Name)
	}
	_, err := m.lifecycleFixture(confirmTarget{prompt: uidialogs.Confirm{Action: uidialogs.ConfirmRevive}, lifecycle: uilifecycle.Target{Action: uilifecycle.Action(uidialogs.ConfirmRevive), Sessions: []store.Session{sess}}})
	return err
}

func (m *Model) persistCollapsed(paths []string) error {
	_, err := m.bindEffect(uirailpersist.Request{Mutation: uirail.Mutation{Kind: uirail.SaveCollapsed, Collapsed: append([]string(nil), paths...)}})()
	return err
}
