package ui

import (
	"context"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/sysstat"
	"github.com/YoanWai/agent-manager/internal/tmux"
	tea "github.com/charmbracelet/bubbletea"
	"time"
)

// agentStats aggregates process-tree usage across all live sessions.
// cpu and ram are shares of this machine (0–100); rss is absolute bytes.
type agentStats struct {
	count int
	cpu   float64
	ram   float64
	rss   uint64
}

type refreshMsg struct {
	sessions []store.Session
	// listedAt is when the pass read that list, which is a whole pass of
	// tmux and ps calls before the UI sees it.
	listedAt       time.Time
	groups         []string
	groupPaths     map[string]string
	groupWorktrees map[string]string
	archivedGroups map[string]bool
	snap           sysstat.Snapshot
	snapOK         bool
	proc           sysstat.ProcStat
	procFor        string
	preview        string
	agents         agentStats
	queuedMessages map[string]int
	paneLines      map[string]string
	panePrompts    map[string]string
	// panes is the pass's agent pane geometry, read off the UI loop with
	// the liveness listing the poller already makes.
	panes map[string]tmux.Pane
	// tmuxSocket is the server this pass read panes from, which tells the
	// rows apart from ones another manager's server holds.
	tmuxSocket string
	// leadingManager is whether this pass held the store, which decides
	// whether the rows no server has claimed are this manager's to show as
	// current.
	leadingManager bool
	// focusID is the session a clicked notification named, taken from
	// the config directory by this pass.
	focusID string
}

// StartPoller launches the background polling loop. It runs outside the
// bubbletea event loop so statuses keep updating while the TUI is
// suspended inside a tmux attach.
func (m *Model) StartPoller(ctx context.Context, send func(tea.Msg)) <-chan struct{} {
	m.focusPane.focus = newFocusWatch(m.services.tmux, send)
	m.syncPollInput()
	results := m.poller.results(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer m.focusPane.focus.Close()
		for result := range results {
			send(pollMessage(result))
		}
	}()
	return done
}

func (m *Model) syncPollInput() {
	selectedID := ""
	focusID := ""
	if sess, ok := m.selected(); ok {
		selectedID = sess.ID
		if !sess.Archived {
			focusID = sess.ID
		}
	}
	m.poller.setInput(m.rail.showArchived, selectedID)
	// Only ever stop the watcher here. Opening a control client costs a
	// process and a tmux attach, so holding j through twenty rows would
	// pay that twenty times; the client is opened once the cursor settles
	// instead. The watcher only exists once StartPoller has a send
	// function; tests drive Update without one.
	if m.focusPane.focus != nil && m.focusPane.focus.watching() != focusID {
		m.focusPane.focus.setFocus("")
	}
}

// watchSelection points the control client at the current selection. Call
// it where the selection has come to rest, never on every cursor move.
func (m *Model) watchSelection() {
	if m.focusPane.focus == nil {
		return
	}
	sess, ok := m.selected()
	if !ok || sess.Archived {
		m.focusPane.focus.setFocus("")
		return
	}
	m.focusPane.focus.setFocus(sess.ID)
}

// requestRefresh publishes the current UI state to the poller and asks
// for an immediate pass.
func (m *Model) requestRefresh() {
	m.syncPollInput()
	m.poller.requestRefresh()
}

// refreshExistingSessionUX re-applies the tmux bindings and status bar to
// sessions that were already running when the manager started, so a session
// created before an update still gets the current key bindings (the
// server-global Ctrl+R review key) and footer.
func (m *Model) refreshExistingSessionUX() tea.Msg {
	if err := m.services.tmux.EnsureBindings(); err != nil {
		return errMsg{err}
	}
	sessions, err := m.services.store.ListSessions(true)
	if err != nil {
		return errMsg{err}
	}
	for _, sess := range sessions {
		if !m.services.tmux.Exists(sess.ID) {
			continue
		}
		// Best-effort per session: one that dies between the check and here
		// errors harmlessly and must not abort the rest, and the bindings that
		// matter are already installed above.
		_ = m.services.tmux.RefreshChrome(sess.ID)
		_ = m.services.tmux.SetLabel(sess.ID, sessionLabel(sess.Group, sess.Name))
	}
	return nil
}

// refreshCmd runs one synchronous polling pass; the background poller
// covers normal operation, this exists for tests and explicit refreshes.
func (m *Model) refreshCmd() tea.Cmd {
	m.syncPollInput()
	return func() tea.Msg {
		return m.poller.refreshOnce()
	}
}
