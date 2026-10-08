package ui

import (
	"errors"
	"github.com/YoanWai/agent-manager/internal/status"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
)

// shellPromptHint refuses to write into a shell. SendText pastes and then
// presses Enter, so a sentence meant for an agent would run as a command
// on the user's machine. Entering the session is how text reaches a shell,
// where what is typed is plainly a command.
func shellPromptHint(name string) string {
	return name + " is a shell, not an agent - enter it to type there"
}

func (m *Model) attachSelected() (tea.Model, tea.Cmd) {
	sess, ok := m.selected()
	if !ok {
		return m, nil
	}
	// The dead-pane probe and the finished acknowledgement run inside the
	// attach effect; a dead session reports back from its completion.
	m.errBar.text = ""
	return m, m.attachCmd(sess.ID)
}

// acknowledgeSelected marks the selected finished session idle and acked
// without entering it. Archived sessions keep their preserved status: the
// poller never re-derives it for them, so an ack would stick forever.
func (m *Model) acknowledgeSelected() (tea.Model, tea.Cmd) {
	sess, ok := m.selected()
	if !ok || sess.Archived || sess.Status != status.Finished {
		return m, nil
	}
	m.errBar.text = ""
	m.enqueueEffect(ackRequest{sessionID: sess.ID}, 0, false)
	return m, m.nextEffectCmd()
}

func (m *Model) attachCmd(id string) tea.Cmd {
	m.enqueueEffect(attachRequest{id: id, foregroundGen: m.gens.foreground, mode: m.mode}, 0, false)
	return m.nextEffectCmd()
}

func (m *Model) reattach(id string, diffGen int) tea.Cmd {
	m.enqueueEffect(attachRequest{id: id, reattach: true, generation: diffGen, foregroundGen: m.gens.foreground, mode: m.mode}, 0, false)
	return m.nextEffectCmd()
}

// copyReplySelected puts the selected session's newest reply on the system
// clipboard without entering it.
func (m *Model) copyReplySelected() (tea.Model, tea.Cmd) {
	entry, ok := m.selectedRow()
	if !ok || entry.isGroup || m.services.engine == nil || m.services.tmux == nil {
		return m, nil
	}
	sess := entry.sess
	m.errBar.text = ""
	// A shell has no reply, and its scrollback is the user's own commands
	// and their output rather than anything an agent said.
	if m.isShell(sess.Tool) {
		m.errBar.text = shellPromptHint(sess.Name)
		return m, nil
	}
	engine, driver := m.services.engine, m.services.tmux
	return m, func() tea.Msg {
		if !driver.Exists(sess.ID) {
			return errMsg{errors.New(deadSessionHint)}
		}
		pane, err := driver.CapturePaneHistory(sess.ID, quoteHistoryLines)
		if err != nil {
			return errMsg{err}
		}
		text, bounded, ok := engine.FullTurnText(sess.Tool, engine.Plain(sess.Tool, pane))
		if !ok {
			return replyCopiedMsg{name: sess.Name, tool: sess.Tool, unreadable: true}
		}
		if strings.TrimSpace(text) == "" {
			return replyCopiedMsg{name: sess.Name, tool: sess.Tool}
		}
		return copyTextCmd(text, func(chars int) tea.Msg {
			return replyCopiedMsg{chars: chars, name: sess.Name, tool: sess.Tool, unbounded: !bounded}
		})()
	}
}

// replyCopiedMsg reports a finished copy. No chars means the turn held
// nothing; unreadable means the tool draws no region a reply can be read
// from, which no amount of retrying will change; unbounded means nothing
// in the pane said where the turn began, so the copy is the whole screen
// rather than one answer.
type replyCopiedMsg struct {
	chars      int
	name       string
	tool       string
	unreadable bool
	unbounded  bool
}

type attachDoneMsg struct {
	sessID string
	err    error
}
