package execution

import (
	"fmt"
	"strings"

	"github.com/YoanWai/agent-manager/internal/config"
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
// whose ping they must not miss. The notify command set with
// agent-manager settings runs for the same transitions, after the native
// banner and never instead of it.
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
	event := notify.Event{ID: sess.ID, Session: sess.Name, Tool: sess.Tool, Kind: kind, Dir: sess.Cwd, Branch: sess.WorktreeBranch}
	command, readErr := p.notifyCommand()
	unshown := command != "" && p.claimUnshownCommand(command)
	// Delivery can wait on an external process (osascript, notify-send,
	// the user's command), so it must never run inside refreshOnce, which
	// holds runMu.
	go func() {
		p.notifyFn(event)
		if unshown {
			p.showCommandFn(command)
		}
		if command != "" || readErr != nil {
			p.commandFn(event, command, readErr)
		}
	}()
}

func runNotifyCommand(event notify.Event, command string, readErr error) {
	dir, err := config.Dir()
	if err != nil {
		return
	}
	if readErr != nil {
		notify.LogCommandFailure(dir, event, fmt.Errorf("reading the notify command: %w", readErr))
		return
	}
	notify.RunCommand(command, dir, event)
}

func (p *Runner) notifyCommand() (string, error) {
	command, err := p.store.Setting(notifyCommandSetting)
	return strings.TrimSpace(command), err
}

// claimUnshownCommand records command as shown and reports whether it was
// new, in one write so managers sharing the store show it once between
// them. A store that cannot answer counts as unshown, never as hidden.
func (p *Runner) claimUnshownCommand(command string) bool {
	changed, err := p.store.SetSettingIfChanged(notifyCommandShownSetting, command)
	return changed || err != nil
}

func (p *Runner) notificationsOn() bool {
	chosen, err := p.store.Setting(notificationsSetting)
	return err != nil || chosen != "off"
}

func (p *Runner) notifyFinished() bool {
	chosen, err := p.store.Setting(notifyFinishedSetting)
	return err == nil && chosen == "on"
}
