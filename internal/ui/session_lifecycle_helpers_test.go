package ui

import (
	"fmt"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uilifecycle "github.com/YoanWai/agent-manager/internal/ui/session/lifecycle"
)

// Legacy fixture entry points drive the same captured executor and reconciliation
// as the UI. Production handlers always return a command instead of running it.
func (m *Model) lifecycleFixture(target confirmTarget) (uilifecycle.Result, error) {
	w, h := m.paneTargetSize()
	request := uilifecycle.Request{Target: packageTarget(target), Pane: sessioncmd.PaneSize{Width: w, Height: h}, Watched: m.focus.runtime.Watching()}
	request.Target.Sessions = append([]store.Session(nil), target.sessions...)
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
	_, err := m.lifecycleFixture(confirmTarget{action: uidialogs.ConfirmKill, sessions: []store.Session{sess}})
	return err
}

func (m *Model) restartSession(sess store.Session) error {
	_, err := m.lifecycleFixture(confirmTarget{action: uidialogs.ConfirmRestart, sessions: []store.Session{sess}})
	return err
}

func (m *Model) reviveSession(sess store.Session) error {
	if m.services.tmux.Exists(sess.ID) {
		return fmt.Errorf("session %s is still running; revive only applies to dead sessions", sess.Name)
	}
	_, err := m.lifecycleFixture(confirmTarget{action: uidialogs.ConfirmRevive, sessions: []store.Session{sess}})
	return err
}
