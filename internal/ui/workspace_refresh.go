package ui

import (
	"context"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
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
	groupBases     map[string]string
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
	// turnsEnded are the sessions whose turn this pass saw end after they
	// asked to be archived or killed once it did.
	turnsEnded []string
}

// StartPoller launches the background polling loop. It runs outside the
// bubbletea event loop so statuses keep updating while the TUI is
// suspended inside a tmux attach.
func (m *Model) StartPoller(ctx context.Context, send func(tea.Msg)) <-chan struct{} {
	m.focus.runtime.watch = newFocusWatch(m.services.tmux, send)
	m.syncPollInput()
	results := m.poller.results(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer m.focus.runtime.watch.Close()
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
	// Keep the loaded projection complete in both views. Rail policy decides
	// which archive state to render, while confirmation cards need hidden
	// children too and must not read SQLite on the Update path.
	m.poller.setInput(true, selectedID)
	// Only ever stop the watcher here. Opening a control client costs a
	// process and a tmux attach, so holding j through twenty rows would
	// pay that twenty times; the client is opened once the cursor settles
	// instead. The watcher only exists once StartPoller has a send
	// function; tests drive Update without one.
	if m.focus.runtime.watch != nil && m.focus.runtime.watch.watching() != focusID {
		m.focus.runtime.watch.setFocus("")
	}
}

// watchSelection points the control client at the current selection. Call
// it where the selection has come to rest, never on every cursor move.
func (m *Model) watchSelection() {
	if m.focus.runtime.watch == nil {
		return
	}
	sess, ok := m.selected()
	if !ok || sess.Archived {
		m.focus.runtime.watch.setFocus("")
		return
	}
	m.focus.runtime.watch.setFocus(sess.ID)
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
		_ = m.services.tmux.SetLabel(sess.ID, sessioncmd.SessionLabel(sess.Group, sess.Name))
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

func (m *Model) routeRefreshMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case refreshMsg:
		// A listing older than a committed mutation or a newer listing keeps
		// its rows out, but still carries focus consumed from a notification,
		// the turns it saw end and its pane facts.
		fenced := !msg.listedAt.IsZero() && !m.effects.latestObservation.IsZero() && !msg.listedAt.After(m.effects.latestObservation)
		staleListing := fenced || (!msg.listedAt.IsZero() && msg.listedAt.Before(m.workspace.lastListedAt))
		if !staleListing && !msg.listedAt.IsZero() {
			m.workspace.lastListedAt = msg.listedAt
		}
		if fenced {
			m.requestRefresh()
		}
		m.startup.booting = false
		m.ageError()
		var focusExit tea.Cmd
		if !staleListing {
			// The focused session can die or vanish under us; fall back to the
			// list rather than typing into nothing.
			sessions := m.dropRecentlyRemoved(m.keepPendingLaunches(msg.sessions, msg.listedAt), msg.listedAt)
			stripDeletedGroups(&msg, m.ledger.goneGroups)
			if m.mode == modeFocus {
				if sess, ok := m.selected(); !ok || sessionGone(sessions, sess.ID) {
					focusExit = m.leaveFocus()
				}
			}
			m.workspace.sessions = sessions
			m.workspace.groups = msg.groups
			m.workspace.groupPaths = msg.groupPaths
			m.workspace.groupWorktrees = msg.groupWorktrees
			m.workspace.groupBases = msg.groupBases
			m.workspace.archivedGroups = msg.archivedGroups
		}
		m.endAfterTurns(msg.turnsEnded)
		m.workspace.tmuxSocket = msg.tmuxSocket
		m.workspace.leadingManager = msg.leadingManager
		m.workspace.panes = msg.panes
		m.workspace.agents = msg.agents
		m.workspace.queuedMessages = msg.queuedMessages
		if m.workspace.paneLines == nil {
			m.workspace.paneLines = map[string]string{}
		}
		for id, line := range msg.paneLines {
			m.workspace.paneLines[id] = line
		}
		if m.workspace.panePrompts == nil {
			m.workspace.panePrompts = map[string]string{}
		}
		for id, prompt := range msg.panePrompts {
			if prompt != "" {
				m.workspace.panePrompts[id] = prompt
			}
		}
		m.commitTypedPrompt()
		if msg.snapOK {
			m.workspace.snap = msg.snap
			m.updateNetRates(msg.snap)
		}
		// Sessions left from a previous run carry that run's window size,
		// which the cache knows nothing about; seedPaneGeom adopts their
		// real geometry on the first pass, so nothing resets the cache here.
		if !m.startup.sessionsSized && m.layout.width > 0 && len(m.workspace.sessions) > 0 {
			m.startup.sessionsSized = true
		}
		m.publishPaneSize()
		// The preview box changes height for more reasons than a terminal
		// resize: the quick bar opening, the status line appearing, a new
		// badge in the header. A pane shorter than the box paints a dead
		// band under its output, so every pass grows what falls short.
		// The call is free when nothing moved: it diffs against paneGeom.
		if m.startup.sessionsSized && m.layout.width > 0 {
			m.resizeSessions()
		}
		m.settleInstall()
		m.rebuildRows()
		if msg.focusID != "" {
			focused, ok := m.selected()
			// Moving the cursor under a focused pane would leave the
			// keyboard pinned to the session the user was in while the
			// list claims another. The click steps back to the list, and
			// only once its session turns out to have a row to land on.
			if m.focusSession(msg.focusID) && m.mode == modeFocus && (!ok || focused.ID != msg.focusID) {
				focusExit = m.leaveFocus()
			}
		}
		reviewStatuses := m.reviewStatusesCmd()
		// A pass that ran with a stale selection (a session created this
		// tick, or one a notification click just chose) carries the wrong
		// preview; resync and fetch it directly.
		if sess, ok := m.selected(); ok && sess.ID != msg.procFor {
			m.syncPollInput()
			gen := m.focus.pane.MovePreview()
			return routed(m, tea.Batch(focusExit, m.previewCmd(sess, gen), m.diffRefreshCmd(), reviewStatuses, m.startStartupTick(), m.pollConnections()))
		}
		m.workspace.proc = msg.proc
		m.workspace.procFor = msg.procFor
		m.setPreview(msg.procFor, msg.preview)
		// A selection that has not moved since the last pass is at rest,
		// so this covers the startup case where no settle ever fired.
		if m.focus.pane.ObservePoll() {
			m.watchSelection()
		}
		return routed(m, tea.Batch(focusExit, m.diffRefreshCmd(), reviewStatuses, m.startStartupTick(), m.pollConnections()))
	}
	return nil, nil, false
}
