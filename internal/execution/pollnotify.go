package execution

import (
	"github.com/YoanWai/agent-manager/internal/notify"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
)

// notifyTransition fires a desktop notification when a session's status
// flips into one the user has asked to hear about. It runs only from the
// transition branch of refreshOnce, so a status that persists across polls
// never re-fires. Waiting and errored notify unless silenced in Settings;
// finished stays quiet unless opted in, since most turn ends are routine.
// There is no focus gate: the poll cannot tell whether the user is looking
// at the manager, and the session they are watching is precisely the one
// whose ping they must not miss.
func (p *Runner) notifyTransition(sess store.Session, newStatus string) {
	if p.notifyFn == nil {
		return
	}
	var kind notify.Kind
	switch newStatus {
	case status.Waiting:
		kind = notify.Waiting
	case status.Errored:
		kind = notify.Errored
	case status.Finished:
		if !p.notifyFinished() {
			return
		}
		kind = notify.Finished
	default:
		return
	}
	if !p.notificationsOn() {
		return
	}
	// Delivery can wait on an external process (osascript, notify-send),
	// so it must never run inside refreshOnce, which holds runMu.
	go p.notifyFn(notify.Event{ID: sess.ID, Session: sess.Name, Tool: sess.Tool, Kind: kind})
}

func (p *Runner) notificationsOn() bool {
	chosen, err := p.store.Setting(notificationsSetting)
	return err != nil || chosen != "off"
}

func (p *Runner) notifyFinished() bool {
	chosen, err := p.store.Setting(notifyFinishedSetting)
	return err == nil && chosen == "on"
}
