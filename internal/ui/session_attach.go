package ui

import (
	"errors"
	"fmt"
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
	m.clearErr()
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
	m.clearErr()
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
	m.clearErr()
	// A shell has no reply, and its scrollback is the user's own commands
	// and their output rather than anything an agent said.
	if m.isShell(sess.Tool) {
		m.reportErr(shellPromptHint(sess.Name))
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

func (m *Model) routeSessionMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case replyCopiedMsg:
		if msg.unreadable {
			m.reportWarn(fmt.Sprintf("no reply to read in %s: a %s pane is not read that way", msg.name, msg.tool))
			return routed(m, nil)
		}
		if msg.chars == 0 {
			m.reportErr("nothing to copy from " + msg.name)
			return routed(m, nil)
		}
		if msg.unbounded {
			m.reportWarn(fmt.Sprintf("copied %d chars from %s: %s marks no turn start here, so this is the whole pane",
				msg.chars, msg.name, msg.tool))
			return routed(m, nil)
		}
		m.reportDone(fmt.Sprintf("copied %d chars from %s", msg.chars, msg.name))
		return routed(m, nil)

	case launchCommandCopiedMsg:
		m.handleLaunchCommandCopied(msg)
		return routed(m, nil)

	case attachDoneMsg:
		// An agent that repainted the terminal background for itself leaves
		// it on ours; the resume's WindowSizeMsg skips its own sync when the
		// size is unchanged, so the detach restores the theme's here.
		SyncTerminalColors()
		// The attach client sized the window to the full terminal and tmux
		// keeps that size on detach; pin it back to the current layout's
		// box so the capture is not clipped on the right.
		if m.focus.runtime.lastPaneSizes != nil {
			delete(m.focus.runtime.lastPaneSizes, msg.sessID)
		}
		width, height := m.paneTargetSize()
		m.queueGeometry(geometryRequest{targets: []paneResize{{id: msg.sessID, size: [2]int{width, height}}}})
		if msg.err != nil {
			m.reportErr(msg.err.Error())
			m.requestRefresh()
			return routed(m, nil)
		}
		// Ctrl+R and F3 inside the session leave a marker before
		// detaching; the lane reads it once and clears it, and the
		// completion carries it out for the session just attached.
		m.enqueueEffect(detachRequest{sessionID: msg.sessID, generation: m.gens.foreground}, 0, false)
		return routed(m, nil)
	}
	return nil, nil, false
}
