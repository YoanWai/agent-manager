package ui

import (
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/sysstat"
	"github.com/YoanWai/agent-manager/internal/tmux"
	tea "github.com/charmbracelet/bubbletea"
)

type netStats struct {
	up       uint64
	down     uint64
	rates    bool
	prevSent uint64
	prevRecv uint64
	prevAt   time.Time
	prevOK   bool
}

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
func (m *Model) StartPoller(send func(tea.Msg)) {
	m.focus = newFocusWatch(m.tmux, send)
	m.syncPollInput()
	go m.poller.run(send)
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
	m.poller.setInput(m.showArchived, selectedID)
	// Only ever stop the watcher here. Opening a control client costs a
	// process and a tmux attach, so holding j through twenty rows would
	// pay that twenty times; the client is opened once the cursor settles
	// instead. The watcher only exists once StartPoller has a send
	// function; tests drive Update without one.
	if m.focus != nil && m.focus.watching() != focusID {
		m.focus.setFocus("")
	}
}

// requestRefresh publishes the current UI state to the poller and asks
// for an immediate pass.
func (m *Model) requestRefresh() {
	m.syncPollInput()
	m.poller.requestRefresh()
}

// refreshCmd runs one synchronous polling pass; the background poller
// covers normal operation, this exists for tests and explicit refreshes.
func (m *Model) refreshCmd() tea.Cmd {
	m.syncPollInput()
	return func() tea.Msg {
		return m.poller.refreshOnce()
	}
}

// sessionGone reports whether id is absent from a refresh's session list.
func sessionGone(sessions []store.Session, id string) bool {
	for _, sess := range sessions {
		if sess.ID == id {
			return false
		}
	}
	return true
}

// updateNetRates diffs cumulative interface counters between polls into
// bytes-per-second rates. Counters can reset (sleep, interface changes),
// so a backwards jump just reseeds the baseline.
func (m *Model) updateNetRates(snap sysstat.Snapshot) {
	now := time.Now()
	if m.net.prevOK && snap.NetOK &&
		snap.NetSent >= m.net.prevSent && snap.NetRecv >= m.net.prevRecv {
		if dt := now.Sub(m.net.prevAt).Seconds(); dt > 0 {
			m.net.up = uint64(float64(snap.NetSent-m.net.prevSent) / dt)
			m.net.down = uint64(float64(snap.NetRecv-m.net.prevRecv) / dt)
			m.net.rates = true
		}
	}
	m.net.prevSent = snap.NetSent
	m.net.prevRecv = snap.NetRecv
	m.net.prevAt = now
	m.net.prevOK = snap.NetOK
}
