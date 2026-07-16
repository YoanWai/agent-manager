package ui

import (
	"sync"
	"time"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/sysstat"
	"github.com/YoanWai/agent-manager/internal/tmux"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// poller drives the session polling loop in its own goroutine, so status
// updates keep landing in the store even while the TUI is suspended
// inside a tmux attach. The UI receives the results as refreshMsg values
// and merely renders them.
type poller struct {
	store    *store.Store
	tmux     *tmux.Driver
	engine   *status.Engine
	interval time.Duration
	poke     chan struct{}

	mu              sync.Mutex
	includeArchived bool
	selectedID      string

	// guarded by runMu: refresh state shared between the polling loop
	// and one-off refresh commands
	runMu      sync.Mutex
	paneHashes map[string]uint64
	tick       int
}

func newPoller(st *store.Store, driver *tmux.Driver, engine *status.Engine, interval time.Duration) *poller {
	return &poller{
		store:      st,
		tmux:       driver,
		engine:     engine,
		interval:   interval,
		poke:       make(chan struct{}, 1),
		paneHashes: map[string]uint64{},
	}
}

// setInput publishes the UI state the next refresh should honor.
func (p *poller) setInput(includeArchived bool, selectedID string) {
	p.mu.Lock()
	p.includeArchived = includeArchived
	p.selectedID = selectedID
	p.mu.Unlock()
}

// requestRefresh asks the polling loop for an immediate pass.
func (p *poller) requestRefresh() {
	select {
	case p.poke <- struct{}{}:
	default:
	}
}

// run polls until the program exits, pushing each result into the UI.
// Sends run on their own goroutine because the UI stops receiving while
// suspended inside a tmux attach; the store writes in refreshOnce must
// never wait on the UI.
func (p *poller) run(send func(tea.Msg)) {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	pending := make(chan tea.Msg, 1)
	go func() {
		for msg := range pending {
			send(msg)
		}
	}()
	for {
		msg := p.refreshOnce()
		// Keep only the newest result when the UI is not draining.
		select {
		case pending <- msg:
		default:
			select {
			case <-pending:
			default:
			}
			pending <- msg
		}
		select {
		case <-ticker.C:
		case <-p.poke:
		}
	}
}

// refreshOnce polls every live session's pane, derives and stores status,
// and samples system stats. Liveness and pane pids come from one tmux
// call, and every process tree from one ps call, so the poll cost stays
// flat as sessions are added. runMu serializes the loop with one-off
// refreshes issued as tea commands.
func (p *poller) refreshOnce() tea.Msg {
	p.runMu.Lock()
	defer p.runMu.Unlock()
	p.mu.Lock()
	includeArchived := p.includeArchived
	selectedID := p.selectedID
	p.mu.Unlock()
	// Machine gauges change slowly; sample them every other poll.
	sampleStats := p.tick%2 == 0
	p.tick++

	sessions, err := p.store.ListSessions(includeArchived)
	if err != nil {
		return errMsg{err}
	}

	panes, err := p.tmux.Panes()
	if err != nil {
		return errMsg{err}
	}
	var livePIDs []int
	for _, sess := range sessions {
		if !sess.Archived && panes[sess.ID] > 0 {
			livePIDs = append(livePIDs, panes[sess.ID])
		}
	}
	trees := sysstat.Trees(livePIDs)

	preview := ""
	var proc sysstat.ProcStat
	var agents agentStats
	paneHashes := make(map[string]uint64, len(sessions))
	for i, sess := range sessions {
		if sess.Archived {
			continue
		}
		newStatus := status.Dead
		if pid := panes[sess.ID]; pid > 0 {
			if stat := trees[pid]; stat.OK {
				agents.count++
				agents.cpu += stat.CPUPercent
				agents.rss += stat.RSS
				if sess.ID == selectedID {
					proc = stat
				}
			}
			if pane, err := p.tmux.CapturePane(sess.ID); err == nil {
				newStatus = p.derivePaneStatus(sess, pane, paneHashes)
				// Any real transition re-arms the finished alert.
				if sess.Acked && newStatus != status.Idle && newStatus != status.Finished {
					if err := p.store.SetAcked(sess.ID, false); err != nil {
						return errMsg{err}
					}
					sessions[i].Acked = false
				}
				if sess.ID == selectedID {
					preview = pane
				}
			}
		}
		if newStatus != sess.Status {
			if err := p.store.UpdateStatus(sess.ID, newStatus); err != nil {
				return errMsg{err}
			}
			sessions[i].Status = newStatus
		}
	}
	p.paneHashes = paneHashes

	groups, err := p.store.Groups()
	if err != nil {
		return errMsg{err}
	}
	names := make([]string, len(groups))
	paths := make(map[string]string, len(groups))
	for i, g := range groups {
		names[i] = g.Name
		paths[g.Name] = g.Path
	}

	msg := refreshMsg{
		sessions:   sessions,
		groups:     names,
		groupPaths: paths,
		proc:       proc,
		procFor:    selectedID,
		preview:    preview,
		agents:     agents,
	}
	if sampleStats {
		msg.snap = sysstat.Sample("/")
		msg.snapOK = true
	}
	return msg
}

// derivePaneStatus turns one captured pane into a session status. The
// capture carries ANSI escapes for the preview; rules match against the
// stripped text. Streaming output often renders without any spinner, so
// when no rule matches but the content region above the input box changed
// since the previous poll, the session counts as working. Finished is an
// alert: entering the session acknowledges it (acked), and the pane keeps
// deriving finished until the next turn, so acked maps it back to idle.
func (p *poller) derivePaneStatus(sess store.Session, pane string, paneHashes map[string]uint64) string {
	text := ansi.Strip(pane)
	newStatus, matched := p.engine.Match(sess.Tool, text)
	if region, ok := p.engine.ActivityRegion(sess.Tool, text); ok {
		hash := hashString(region)
		paneHashes[sess.ID] = hash
		if !matched {
			if previous, seen := p.paneHashes[sess.ID]; seen && previous != hash {
				newStatus = status.Working
			}
		}
	}
	if newStatus == status.Finished && sess.Acked {
		newStatus = status.Idle
	}
	return newStatus
}
