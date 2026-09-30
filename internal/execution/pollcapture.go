package execution

import (
	"path/filepath"
	"sort"

	"time"

	"github.com/YoanWai/agent-manager/internal/agentsession"

	"github.com/YoanWai/agent-manager/internal/store"

	"github.com/YoanWai/agent-manager/internal/tmux"
)

// idMinting reports whether a live, not-yet-captured session belongs to a
// tool that mints its own conversation id.
func (p *Runner) idMinting(sess store.Session, panes map[string]tmux.Pane) bool {
	return !sess.Archived && panes[sess.ID].PID != 0 && sess.AgentSessionID == "" &&
		p.sessionStores[sess.Tool] != ""
}

// startCaptureIfIdle runs id capture off the poll lock. Capturing an
// some ids shell out to the tool's CLI, which can take seconds;
// doing it inside refreshOnce would hold runMu and stall every refresh,
// including a freshly submitted session's first appearance. One pass runs at
// a time, on a snapshot, and a pass that captures anything pokes a refresh so
// the UI and store pick up the new ids.
func (p *Runner) startCaptureIfIdle(sessions []store.Session, panes map[string]tmux.Pane) {
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
	p.captureWait.Add(1)
	go func() {
		defer p.captureWait.Done()
		defer p.captureBusy.Store(false)
		captured, err := p.captureAgentSessionIDs(snapshot, panes)
		if err != nil {
			p.mu.Lock()
			p.captureErr = err
			p.mu.Unlock()
		}
		if captured > 0 || err != nil {
			p.RequestRefresh()
		}
	}()
}

// captureAgentSessionIDs binds each not-yet-captured id-minting session to
// the conversation its CLI wrote, returning how many it bound. Sessions are
// processed in launch order so the earliest one claims
// the earliest unclaimed conversation in its directory; a later session
// started in the same directory then skips that one via claimed and captures
// its own. Launch times carry nanosecond precision, so sessions launched a
// moment apart in the same directory still order deterministically.
func (p *Runner) captureAgentSessionIDs(sessions []store.Session, panes map[string]tmux.Pane) (int, error) {
	claimed := make(map[string]bool, len(sessions))
	for _, sess := range sessions {
		if sess.AgentSessionID != "" {
			claimed[sess.AgentSessionID] = true
		}
		// A restart's old conversation still sits in the directory, newer
		// than every other candidate; claiming it keeps the fresh run from
		// binding straight back to the context the restart dropped.
		if sess.RetiredAgentSessionID != "" {
			claimed[sess.RetiredAgentSessionID] = true
		}
	}
	pending := make([]int, 0, len(sessions))
	for i, sess := range sessions {
		if p.idMinting(sess, panes) {
			pending = append(pending, i)
		}
	}
	sort.SliceStable(pending, func(a, b int) bool {
		return sessions[pending[a]].LaunchTime().Before(sessions[pending[b]].LaunchTime())
	})
	recaptureScope := func(sess store.Session) string {
		cwd := sess.Cwd
		if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
			cwd = resolved
		}
		return p.sessionStores[sess.Tool] + "\x00" + cwd
	}
	recaptureCounts := map[string]int{}
	for _, i := range pending {
		// The persisted snapshot marks a relaunch even while AgentLaunchedAt
		// is still in flight, so the shared-scope refusal covers the
		// stamping window too.
		if !sessions[i].AgentLaunchedAt.IsZero() || sessions[i].RelaunchSnapshot != nil {
			recaptureCounts[recaptureScope(sessions[i])]++
		}
	}
	captured := 0
cands:
	for _, i := range pending {
		sess := sessions[i]
		// A resumed session cannot use the earliest-write tie-break: in a
		// shared cwd it would bind the wrong conversation, so only one
		// exact candidate counts.
		var agentID string
		var ok bool
		if sess.AgentLaunchedAt.IsZero() && sess.RelaunchSnapshot == nil {
			agentID, ok = agentsession.Capture(p.sessionStores[sess.Tool], sess.Cwd, sess.LaunchTime(), claimed)
		} else {
			if recaptureCounts[recaptureScope(sess)] > 1 {
				p.clearRecaptureSeen(sess.ID)
				continue
			}
			if sess.RelaunchSnapshot == nil {
				// No pre-launch snapshot means the relaunch predates snapshot
				// capture; recapture refuses rather than guess.
				continue
			}
			agentID, ok = agentsession.Recapture(p.sessionStores[sess.Tool], sess.Cwd, sess.RelaunchSnapshot, claimed)
			if ok && !p.stableRecapture(sess.ID, sess.AgentLaunchedAt, agentID) {
				// First sighting: bind once the next pass sees it again, so
				// a picker still settling is not mistaken for the resumed
				// conversation.
				continue cands
			}
		}
		if !ok {
			p.clearRecaptureSeen(sess.ID)
			continue
		}
		// The raw field, never LaunchTime(): the compare-and-set matches the
		// stored column, which is zero for a session that never restarted,
		// while LaunchTime() answers CreatedAt and would match nothing.
		bound, err := p.store.BindAgentSessionID(sess.ID, agentID, sess.AgentLaunchedAt)
		if err != nil {
			return captured, err
		}
		if !bound {
			// The session was restarted (or bound elsewhere) while this pass
			// was reading the tool's store, so this id answers a launch that
			// is over. The next pass captures the one now in the pane.
			continue
		}
		claimed[agentID] = true
		captured++
	}
	return captured, nil
}

func (p *Runner) stableRecapture(sessID string, launchedAt time.Time, agentID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	want := recaptureSighting{agentID: agentID, launchedAt: launchedAt}
	if p.recaptureSeen[sessID] == want {
		delete(p.recaptureSeen, sessID)
		return true
	}
	p.recaptureSeen[sessID] = want
	return false
}

// A later sighting starts its two-pass count fresh.
func (p *Runner) clearRecaptureSeen(sessID string) {
	p.mu.Lock()
	delete(p.recaptureSeen, sessID)
	p.mu.Unlock()
}
