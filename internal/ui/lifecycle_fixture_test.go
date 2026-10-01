package ui

import (
	"fmt"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
)

// Legacy fixture entry points drive the same captured executor and reconciliation
// as the UI. Production handlers always return a command instead of running it.
func (m *Model) lifecycleFixture(target confirmTarget) (lifecycleEffectResult, error) {
	w, h := m.paneTargetSize()
	request := lifecycleRequest{target: target, pane: sessioncmd.PaneSize{Width: w, Height: h}, watched: m.watchedSession()}
	request.target.sessions = append([]store.Session(nil), target.sessions...)
	completed, err := m.captureEffect(request)()
	result := completed.(lifecycleEffectResult)
	m.applyLifecycleEffect(request, result, err)
	if err == nil {
		err = result.warning
	}
	return result, err
}
func (m *Model) archiveConfirmed() error { _, err := m.lifecycleFixture(m.confirm); return err }
func (m *Model) restoreConfirmed() error { _, err := m.lifecycleFixture(m.confirm); return err }
func (m *Model) deleteConfirmed() (sessioncmd.DeleteResult, error) {
	result, err := m.lifecycleFixture(m.confirm)
	return result.deleted, err
}
func (m *Model) killSession(sess store.Session) error {
	_, err := m.lifecycleFixture(confirmTarget{action: actionKill, sessions: []store.Session{sess}})
	return err
}
func (m *Model) restartSession(sess store.Session) error {
	_, err := m.lifecycleFixture(confirmTarget{action: actionRestart, sessions: []store.Session{sess}})
	return err
}
func (m *Model) reviveSession(sess store.Session) error {
	if m.services.tmux.Exists(sess.ID) {
		return fmt.Errorf("session %s is still running; revive only applies to dead sessions", sess.Name)
	}
	_, err := m.lifecycleFixture(confirmTarget{action: actionRevive, sessions: []store.Session{sess}})
	return err
}

func (m *Model) persistCollapsed(paths []string) error {
	_, err := m.captureEffect(railRequest{mutation: uirail.Mutation{Kind: uirail.SaveCollapsed, Collapsed: append([]string(nil), paths...)}})()
	return err
}
