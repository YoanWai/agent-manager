package ui

import (
	"sort"
	"sync"
	"time"

	"github.com/YoanWai/agent-manager/internal/agentsession"
	"github.com/YoanWai/agent-manager/internal/hooks"
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
	store         *store.Store
	tmux          *tmux.Driver
	engine        *status.Engine
	hooks         *hooks.Manager
	statusSources map[string]string
	sessionStores map[string]string
	interval      time.Duration
	poke          chan struct{}

	mu              sync.Mutex
	includeArchived bool
	selectedID      string
	// sessions whose rename directive could not ride the first prompt;
	// sent as a message once the tool's input box appears
	pendingDirective map[string]struct{}

	// guarded by runMu: refresh state shared between the polling loop
	// and one-off refresh commands
	runMu      sync.Mutex
	paneHashes map[string]uint64
	tick       int
}

func newPoller(st *store.Store, driver *tmux.Driver, engine *status.Engine, hookManager *hooks.Manager, statusSources, sessionStores map[string]string, interval time.Duration) *poller {
	return &poller{
		store:            st,
		tmux:             driver,
		engine:           engine,
		hooks:            hookManager,
		statusSources:    statusSources,
		sessionStores:    sessionStores,
		interval:         interval,
		poke:             make(chan struct{}, 1),
		paneHashes:       map[string]uint64{},
		pendingDirective: map[string]struct{}{},
	}
}

func (p *poller) markDirectivePending(id string) {
	p.mu.Lock()
	p.pendingDirective[id] = struct{}{}
	p.mu.Unlock()
}

func (p *poller) directivePending(id string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, pending := p.pendingDirective[id]
	return pending
}

func (p *poller) clearDirective(id string) {
	p.mu.Lock()
	delete(p.pendingDirective, id)
	p.mu.Unlock()
}

// sweepDirectives drops pending flags for sessions that no longer exist,
// so a session deleted before its directive fired leaves nothing behind.
func (p *poller) sweepDirectives(sessions []store.Session) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.pendingDirective) == 0 {
		return
	}
	alive := make(map[string]struct{}, len(sessions))
	for _, sess := range sessions {
		alive[sess.ID] = struct{}{}
	}
	for id := range p.pendingDirective {
		if _, ok := alive[id]; !ok {
			delete(p.pendingDirective, id)
		}
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

// archivedPreview serves an archived session's stored pane snapshot,
// backfilling it from a still-live tmux window for sessions archived
// before snapshots existed.
func archivedPreview(st *store.Store, driver *tmux.Driver, sessID string) (string, error) {
	snapshot, err := st.Snapshot(sessID)
	if err != nil || snapshot != "" {
		return snapshot, err
	}
	if !driver.Exists(sessID) {
		return "", nil
	}
	pane, err := driver.CapturePane(sessID)
	if err != nil || pane == "" {
		return "", nil
	}
	if err := st.SetSnapshot(sessID, pane); err != nil {
		return "", err
	}
	return pane, nil
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
	// Conversation ids already bound to a session, so a tool that mints its
	// own id (codex, opencode) never has two sessions capture the same one.
	claimed := make(map[string]bool, len(sessions))
	for _, sess := range sessions {
		if sess.AgentSessionID != "" {
			claimed[sess.AgentSessionID] = true
		}
	}
	for i, sess := range sessions {
		if sess.Archived {
			continue
		}
		if err := p.applyPendingRename(&sessions[i]); err != nil {
			return errMsg{err}
		}
		if err := p.applyPendingReviewRepo(&sessions[i]); err != nil {
			return errMsg{err}
		}
		newStatus := status.Dead
		if pid := panes[sess.ID]; pid > 0 {
			stat := trees[pid]
			if stat.OK {
				agents.count++
				agents.cpu += stat.CPUPercent
				agents.rss += stat.RSS
				if sess.ID == selectedID {
					proc = stat
				}
			}
			// The pane pid is the shell; the agent runs as its child. A
			// tree of one process means the agent is gone. A failed ps
			// sample proves nothing, so it counts as alive.
			agentAlive := !stat.OK || stat.Procs > 1
			if pane, err := p.tmux.CapturePane(sess.ID); err == nil {
				p.maybeSendDirective(sess, pane, agentAlive)
				derived, err := p.derivePaneStatus(sess, pane, agentAlive, paneHashes)
				if err != nil {
					return errMsg{err}
				}
				newStatus = derived
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
	if preview == "" && selectedID != "" {
		for _, sess := range sessions {
			if sess.ID == selectedID && sess.Archived {
				snapshot, err := archivedPreview(p.store, p.tmux, sess.ID)
				if err != nil {
					return errMsg{err}
				}
				preview = snapshot
				break
			}
		}
	}
	if err := p.captureAgentSessionIDs(sessions, panes, claimed); err != nil {
		return errMsg{err}
	}
	p.paneHashes = paneHashes
	p.sweepDirectives(sessions)

	groups, err := p.store.Groups()
	if err != nil {
		return errMsg{err}
	}
	names := make([]string, len(groups))
	paths := make(map[string]string, len(groups))
	archivedGroups := make(map[string]bool, len(groups))
	for i, g := range groups {
		names[i] = g.Name
		paths[g.Name] = g.Path
		if g.Archived {
			archivedGroups[g.Name] = true
		}
	}

	msg := refreshMsg{
		sessions:       sessions,
		groups:         names,
		groupPaths:     paths,
		archivedGroups: archivedGroups,
		proc:           proc,
		procFor:        selectedID,
		preview:        preview,
		agents:         agents,
	}
	if sampleStats {
		msg.snap = sysstat.Sample("/")
		msg.snapOK = true
	}
	return msg
}

// captureAgentSessionIDs binds each not-yet-captured id-minting session
// (codex, opencode) to the conversation its CLI wrote. Sessions are
// processed in launch order so the earliest one claims the earliest
// unclaimed rollout in its directory; a later session started in the same
// directory then skips that rollout via claimed and captures its own.
// Capturing out of launch order would let a later session claim an earlier
// one's conversation. CreatedAt carries nanosecond precision, so sessions
// launched a moment apart in the same directory still order deterministically.
func (p *poller) captureAgentSessionIDs(sessions []store.Session, panes map[string]int, claimed map[string]bool) error {
	pending := make([]int, 0, len(sessions))
	for i, sess := range sessions {
		if sess.Archived || panes[sess.ID] == 0 || sess.AgentSessionID != "" {
			continue
		}
		if p.sessionStores[sess.Tool] != "" {
			pending = append(pending, i)
		}
	}
	sort.SliceStable(pending, func(a, b int) bool {
		return sessions[pending[a]].CreatedAt.Before(sessions[pending[b]].CreatedAt)
	})
	for _, i := range pending {
		sess := sessions[i]
		agentID, ok := agentsession.Capture(p.sessionStores[sess.Tool], sess.Cwd, sess.CreatedAt, claimed)
		if !ok {
			continue
		}
		if err := p.store.SetAgentSessionID(sess.ID, agentID); err != nil {
			return err
		}
		sessions[i].AgentSessionID = agentID
		claimed[agentID] = true
	}
	return nil
}

// maybeSendDirective delivers the deferred rename directive once the
// tool's input box shows in the pane, proving the agent booted and can
// take a message. Sent even mid-turn: tools queue typed input and
// process it when the current turn ends. A failed send keeps the flag
// for the next poll. Tools without an activity_cutoff never look ready,
// so their sessions keep the placeholder name.
func (p *poller) maybeSendDirective(sess store.Session, pane string, agentAlive bool) {
	if !agentAlive || !p.directivePending(sess.ID) {
		return
	}
	if _, ready := p.engine.ActivityRegion(sess.Tool, ansi.Strip(pane)); !ready {
		return
	}
	if err := p.tmux.SendText(sess.ID, deferredRenameDirective); err == nil {
		p.clearDirective(sess.ID)
	}
}

// applyPendingRename picks up a name the session's agent left via the
// rename subcommand: the store row and tmux label update together here,
// keeping the manager the sole database writer. The file is consumed
// even when the name is unchanged so it never lingers. A dead tmux
// session cannot take a label, which is fine; the label is rewritten on
// revive.
func (p *poller) applyPendingRename(sess *store.Session) error {
	name, found := p.hooks.ReadName(sess.ID)
	if !found {
		return nil
	}
	if name != "" && name != sess.Name {
		if err := p.store.RenameSession(sess.ID, name); err != nil {
			return err
		}
		sess.Name = name
		_ = p.tmux.SetLabel(sess.ID, sessionLabel(sess.Group, name))
	}
	return p.hooks.RemoveName(sess.ID)
}

func (p *poller) applyPendingReviewRepo(sess *store.Session) error {
	root, found := p.hooks.ReadReviewRepo(sess.ID)
	if !found {
		return nil
	}
	if root != "" {
		if err := p.store.SetReviewRepo(sess.ID, root); err != nil {
			return err
		}
	}
	return p.hooks.RemoveReviewRepo(sess.ID)
}

// derivePaneStatus turns one captured pane into a session status. The
// capture carries ANSI escapes for the preview; rules match against the
// stripped text. Streaming output often renders without any spinner, so
// when no rule matches but the content region above the input box changed
// since the previous poll, the session counts as working. The reverse
// transition closes marker-less turns: a session that was mid-turn whose
// region stopped changing has ended its turn even when the tool printed
// no turn_end line, so the region's last content line decides finished
// versus waiting. Finished is an alert: entering the session acknowledges
// it (acked), and the pane keeps deriving finished until the next turn,
// so acked maps it back to idle.
func (p *poller) derivePaneStatus(sess store.Session, pane string, agentAlive bool, paneHashes map[string]uint64) (string, error) {
	text := ansi.Strip(pane)
	region, hasRegion := p.engine.ActivityRegion(sess.Tool, text)
	var regionHash uint64
	if hasRegion {
		regionHash = hashString(region)
		paneHashes[sess.ID] = regionHash
	}
	if p.statusSources[sess.Tool] == hooks.StatusSourceClaude {
		if !agentAlive {
			// The agent died without its SessionEnd cleanup hook
			// (crash, SIGKILL); a stale file must not mask the pane.
			if err := p.hooks.Remove(sess.ID); err != nil {
				return "", err
			}
		} else if hookStatus, ok := p.hooks.Read(sess.ID); ok {
			return p.applyHookStatus(sess, text, hookStatus), nil
		}
	}
	newStatus, matched := p.engine.Match(sess.Tool, text)
	if hasRegion && !matched {
		if previous, seen := p.paneHashes[sess.ID]; seen {
			if previous != regionHash {
				newStatus = status.Working
			} else if turnInFlight(sess.Status) {
				newStatus = p.engine.TurnEndedState(sess.Tool, region)
			}
		}
	}
	if newStatus == status.Finished && sess.Acked {
		newStatus = status.Idle
	}
	return newStatus, nil
}

// turnInFlight reports whether a status means a turn is running or resting
// unacknowledged. Only then can a quiet region mean the turn just ended;
// finished and waiting stay in the set so the inferred status persists
// across polls instead of collapsing to idle on the next pass.
func turnInFlight(current string) bool {
	return current == status.Working || current == status.Finished || current == status.Waiting
}

// applyHookStatus trusts the hook-reported status over pane heuristics
// for the states hooks can see. They cannot see a plain-text question,
// an interrupt banner, or an error line, so a matched pane verdict
// upgrades finished to waiting or errored, and working to waiting (an
// Esc interrupt fires no Stop event). A working hook also reconciles to
// the pane verdict when the pane shows the turn already ended: background
// subagents write working via PreToolUse/PostToolUse but fire no Stop
// when they finish, so the file would otherwise stay pinned at working
// forever. The pane only reports finished/waiting/errored once the newest
// turn is quiet, so this never fires while the agent is still streaming.
func (p *poller) applyHookStatus(sess store.Session, text, hookStatus string) string {
	paneStatus, matched := p.engine.Match(sess.Tool, text)
	switch hookStatus {
	case status.Finished:
		if matched && (paneStatus == status.Waiting || paneStatus == status.Errored) {
			return paneStatus
		}
		if sess.Acked {
			return status.Idle
		}
	case status.Working:
		if matched && (paneStatus == status.Waiting || paneStatus == status.Finished || paneStatus == status.Errored) {
			if paneStatus == status.Finished && sess.Acked {
				return status.Idle
			}
			return paneStatus
		}
	}
	return hookStatus
}
