package ui

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
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
	// captureErr holds a background id-capture failure to surface on the
	// next poll, since that work no longer runs inline.
	captureErr error
	// sessions whose rename directive could not ride the first prompt;
	// sent as a message once the tool's input box appears
	pendingDirective map[string]struct{}

	// captureBusy guards the single in-flight id-capture goroutine.
	captureBusy atomic.Bool

	// guarded by runMu: refresh state shared between the polling loop
	// and one-off refresh commands
	runMu      sync.Mutex
	paneHashes map[string]uint64
	// quietSince is when each session's activity region last stopped
	// changing while unmatched; used to debounce marker-less turn ends.
	quietSince map[string]time.Time
	tick       int
}

// quietEndGrace is how long a working pane must stay region-stable and
// rule-unmatched before the quiet-region path may mark the turn finished.
// One poll is not enough: agents pause between tools (and a fast poll
// interval would flap working/finished every few ticks).
var quietEndGrace = time.Second

// startingGrace caps how long a session may show the launch state before the
// poll derives its real status regardless, so a tool that never paints its
// pane does not sit on "starting" forever.
const startingGrace = 30 * time.Second

// paneBooted reports whether the agent has painted anything to its pane yet,
// which marks the end of the launch state.
func paneBooted(pane string) bool {
	return strings.TrimSpace(ansi.Strip(pane)) != ""
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
		quietSince:       map[string]time.Time{},
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

// ignoreDeletedSession drops the error a store write returns when the
// session was deleted between this pass listing it and writing to it.
// The row is gone on purpose, so there is nothing left to write and
// nothing for the user to act on; any other failure still surfaces.
func ignoreDeletedSession(err error) error {
	if errors.Is(err, store.ErrSessionGone) {
		return nil
	}
	return err
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
	if err := ignoreDeletedSession(st.SetSnapshot(sessID, pane)); err != nil {
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
	captureErr := p.captureErr
	p.captureErr = nil
	p.mu.Unlock()
	if captureErr != nil {
		return errMsg{captureErr}
	}
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
		if err := p.applyPendingRename(&sessions[i]); err != nil {
			return errMsg{err}
		}
		if err := p.applyPendingReviewRepo(&sessions[i]); err != nil {
			return errMsg{err}
		}
		if err := p.applyPendingReviewBase(&sessions[i]); err != nil {
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
				// Hold the launch state until the agent first paints its pane,
				// so a just-created session reads "starting up" rather than
				// flashing idle before it has booted. A grace cap keeps a tool
				// that never paints from sticking on starting forever.
				if sess.Status == status.Starting && !paneBooted(pane) &&
					time.Since(sess.CreatedAt) < startingGrace {
					newStatus = status.Starting
				}
				// Any real transition re-arms the finished alert.
				if sess.Acked && newStatus != status.Idle && newStatus != status.Finished {
					if err := ignoreDeletedSession(p.store.SetAcked(sess.ID, false)); err != nil {
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
			if err := ignoreDeletedSession(p.store.UpdateStatus(sess.ID, newStatus)); err != nil {
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
	p.startCaptureIfIdle(sessions, panes)
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

// idMinting reports whether a live, not-yet-captured session belongs to a
// tool that mints its own conversation id (codex, opencode).
func (p *poller) idMinting(sess store.Session, panes map[string]int) bool {
	return !sess.Archived && panes[sess.ID] != 0 && sess.AgentSessionID == "" &&
		p.sessionStores[sess.Tool] != ""
}

// startCaptureIfIdle runs id capture off the poll lock. Capturing an
// opencode/codex id shells out to the tool's CLI, which can take seconds;
// doing it inside refreshOnce would hold runMu and stall every refresh,
// including a freshly submitted session's first appearance. One pass runs at
// a time, on a snapshot, and a pass that captures anything pokes a refresh so
// the UI and store pick up the new ids.
func (p *poller) startCaptureIfIdle(sessions []store.Session, panes map[string]int) {
	hasWork := false
	for _, sess := range sessions {
		if p.idMinting(sess, panes) {
			hasWork = true
			break
		}
	}
	if !hasWork || !p.captureBusy.CompareAndSwap(false, true) {
		return
	}
	snapshot := append([]store.Session(nil), sessions...)
	go func() {
		defer p.captureBusy.Store(false)
		captured, err := p.captureAgentSessionIDs(snapshot, panes)
		if err != nil {
			p.mu.Lock()
			p.captureErr = err
			p.mu.Unlock()
		}
		if captured > 0 || err != nil {
			p.requestRefresh()
		}
	}()
}

// captureAgentSessionIDs binds each not-yet-captured id-minting session
// (codex, opencode) to the conversation its CLI wrote, returning how many it
// bound. Sessions are processed in launch order so the earliest one claims
// the earliest unclaimed conversation in its directory; a later session
// started in the same directory then skips that one via claimed and captures
// its own. CreatedAt carries nanosecond precision, so sessions launched a
// moment apart in the same directory still order deterministically.
func (p *poller) captureAgentSessionIDs(sessions []store.Session, panes map[string]int) (int, error) {
	claimed := make(map[string]bool, len(sessions))
	for _, sess := range sessions {
		if sess.AgentSessionID != "" {
			claimed[sess.AgentSessionID] = true
		}
	}
	pending := make([]int, 0, len(sessions))
	for i, sess := range sessions {
		if p.idMinting(sess, panes) {
			pending = append(pending, i)
		}
	}
	sort.SliceStable(pending, func(a, b int) bool {
		return sessions[pending[a]].CreatedAt.Before(sessions[pending[b]].CreatedAt)
	})
	captured := 0
	for _, i := range pending {
		sess := sessions[i]
		agentID, ok := agentsession.Capture(p.sessionStores[sess.Tool], sess.Cwd, sess.CreatedAt, claimed)
		if !ok {
			continue
		}
		if err := ignoreDeletedSession(p.store.SetAgentSessionID(sess.ID, agentID)); err != nil {
			return captured, err
		}
		claimed[agentID] = true
		captured++
	}
	return captured, nil
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
		if err := ignoreDeletedSession(p.store.RenameSession(sess.ID, name)); err != nil {
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

func (p *poller) applyPendingReviewBase(sess *store.Session) error {
	root, ref, found := p.hooks.ReadReviewBase(sess.ID)
	if !found {
		return nil
	}
	if root != "" {
		if err := p.store.SetReviewBase(sess.ID, root, ref); err != nil {
			return err
		}
	}
	return p.hooks.RemoveReviewBase(sess.ID)
}

// reflowSessions drops activity-region hashes for ids and runs reflow
// while the poller is paused (runMu held). A poll must not capture mid-
// resize against a pre-resize hash: that comparison treats reflow as
// streaming and flashes every session as working for one tick.
func (p *poller) reflowSessions(ids []string, reflow func()) {
	if len(ids) == 0 {
		return
	}
	p.runMu.Lock()
	defer p.runMu.Unlock()
	for _, id := range ids {
		delete(p.paneHashes, id)
		delete(p.quietSince, id)
	}
	reflow()
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
//
// A missing prior hash (first observation, or post-resize rebaseline)
// never invents working and never collapses finished/waiting to the tool
// default: the stored status holds until the next poll has a baseline.
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
				delete(p.quietSince, sess.ID)
			} else if turnInFlight(sess.Status) {
				// Already resting: re-infer finished vs waiting without delay.
				if sess.Status != status.Working {
					newStatus = p.engine.TurnEndedState(sess.Tool, region)
				} else {
					// Mid-turn pauses (thinking, between tools) look quiet for
					// a poll or two; wait before treating that as turn end.
					now := time.Now()
					since, ok := p.quietSince[sess.ID]
					if !ok {
						p.quietSince[sess.ID] = now
						since = now
					}
					if now.Sub(since) >= quietEndGrace {
						newStatus = p.engine.TurnEndedState(sess.Tool, region)
						if newStatus != status.Working {
							delete(p.quietSince, sess.ID)
						}
					} else {
						newStatus = status.Working
					}
				}
			}
		} else if turnInFlight(sess.Status) {
			newStatus = sess.Status
		}
	} else {
		delete(p.quietSince, sess.ID)
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
