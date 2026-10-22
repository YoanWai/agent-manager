package execution

import (
	"fmt"
	"strings"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/launch"
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
// whose ping they must not miss. The banner quotes what the row shows:
// a waiting session's question, a finished session's prompt. The notify
// command set with agent-manager settings runs for the same transitions,
// after the native banner and never instead of it.
func (p *Runner) notifyTransition(sess store.Session, newStatus, prompt, quote string) {
	if p.notifyFn == nil {
		return
	}
	var kind notify.Kind
	var text string
	switch newStatus {
	case status.Waiting:
		kind, text = notify.Waiting, quote
	case status.Errored:
		kind = notify.Errored
	case status.Finished:
		if !p.notifyFinished() {
			return
		}
		kind, text = notify.Finished, sessionPrompt(sess, prompt)
	default:
		return
	}
	if !p.notificationsOn() {
		return
	}
	event := notify.Event{
		ID: sess.ID, Session: sess.Name, Tool: sess.Tool, Kind: kind,
		Dir: sess.Cwd, Branch: sess.WorktreeBranch, Repo: sess.WorktreeRepo, Text: text,
	}
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

// sessionPrompt is the line a finished banner quotes, the same fallback
// the session row uses: the prompt echoed in the pane, else the last one
// delivered through the manager, else the one the session launched with.
func sessionPrompt(sess store.Session, echoed string) string {
	if echoed != "" {
		return echoed
	}
	if sess.LastPrompt != "" {
		return sess.LastPrompt
	}
	return launch.DeliveredPrompt(sess.LaunchPrompt)
}

func (p *Runner) notifyFinished() bool {
	chosen, err := p.store.Setting(notifyFinishedSetting)
	return err == nil && chosen == "on"
}
