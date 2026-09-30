package execution

import (
	"errors"
	"github.com/YoanWai/agent-manager/internal/launch"
	"os"

	"sync"
	"sync/atomic"
	"time"

	"context"

	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/hooks"

	"github.com/YoanWai/agent-manager/internal/notify"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/sysstat"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

// Runner drives the session polling loop in its own goroutine, so status
// updates keep landing in the store even while the TUI is suspended
// inside a tmux attach. Consumers receive typed Result observations.
type Runner struct {
	store         *store.Store
	inboxOwner    sessioncmd.InboxMaintenance
	tmux          *tmux.Driver
	engine        *status.Engine
	hooks         *hooks.Manager
	gitDrv        *git.Driver
	statusSources map[string]string
	sessionStores map[string]string
	claudeTails   map[string]claudeTailCache
	mcpStyles     map[string]string
	shellTools    map[string]bool
	binaries      ToolBinaries
	interval      time.Duration
	poke          chan struct{}

	mu              sync.Mutex
	includeArchived bool
	selectedID      string
	// captureErr holds a background id-capture failure to surface on the
	// next poll, since that work no longer runs inline.
	captureErr error

	// captureBusy guards the single in-flight id-capture goroutine.
	captureBusy atomic.Bool
	captureWait sync.WaitGroup
	running     atomic.Bool
	stopped     bool

	// notifyFn delivers one desktop notification; tests swap in a recorder.
	notifyFn func(notify.Event)
	// takeFocus collects the session a clicked notification named; tests
	// swap in a stub.
	takeFocus func() (string, bool)

	// guarded by runMu: refresh state shared between the polling loop
	// and one-off refresh commands
	runMu      sync.Mutex
	paneHashes map[string]uint64
	// quietSince is when each session's activity region last stopped
	// changing while the pane read as working; used to debounce both
	// marker-less turn ends and spinner rows that never resolve. The stuck
	// flag travels with the timer so a rule classification change restarts
	// the grace instead of inheriting the previous one.
	quietSince map[string]quietTimer
	// heartbeatAt is when this manager last tried to claim the store and
	// stamp its liveness row.
	heartbeatAt time.Time
	// leading records whether the last claim left this manager holding the
	// store, which decides who speaks for sessions no socket has claimed.
	leading bool
	tick    int
	// prevTreeCPU / prevTreeAt drive interval agent CPU: cumulative
	// CPU-seconds per pane root from the last poll, so host share uses
	// the same "over this window" idea as the computer gauge.
	prevTreeCPU map[int]float64
	prevTreeAt  time.Time
	// recaptureSeen holds the last single candidate for each launch, so a
	// transient store update cannot bind on its first appearance.
	recaptureSeen map[string]recaptureSighting
}

type recaptureSighting struct {
	agentID    string
	launchedAt time.Time
}

// quietEndGrace is how long a working pane must stay region-stable and
// rule-unmatched before the quiet-region path may mark the turn finished.
// One poll is not enough: agents pause between tools (and a fast poll
// interval would flap working/finished every few ticks).
var quietEndGrace = time.Second

// stuckEndGrace is the longer stability a rule-matched working pane needs
// before the same quiet path may settle it. A live agent animates its pane
// (spinner frames, ticking timers) within a poll or two, so a spinner row
// that sits unchanged this long belongs to a turn that died without ever
// printing its end marker.
var stuckEndGrace = 15 * time.Second

// quietTimer pairs the quiet timestamp with the rule classification it
// started under, so an unmatched-to-working flip cannot spend the previous
// classification's grace.
type quietTimer struct {
	since time.Time
	stuck bool
	// pane hashes the whole pane while stuck: pi animates its spinner below the activity region.
	pane uint64
}

// startingGrace caps how long a session may show the launch state before the
// poll derives its real status regardless, so a tool that never paints its
// pane does not sit on "starting" forever.
const startingGrace = 30 * time.Second

// rowQuoteCap bounds what a row quote stores; a row shows far less, and
// a flattened message can span a whole pane.
const rowQuoteCap = 400

// quoteHistoryLines is how far above the visible pane the second capture
// reaches when a quote's anchor — the message bullet, the prompt echo —
// scrolled off the screen.
const quoteHistoryLines = 300

// managerEchoOpenings open the lines the manager itself types into a
// session — rename directives, the coordination note, an inbox envelope —
// which echo back exactly like a user prompt and must not read as one.
var managerEchoOpenings = []string{
	"Run this exact shell command once,",
	"First, run this exact shell command once,",
	"This session is already named.",
	"Other agent sessions may be running beside you",
	"[agent-manager] ",
}

// claudeTailCache keeps one transcript's extraction keyed to its file
// stats, so an unchanged transcript is not re-read every tick.
type claudeTailCache struct {
	path    string
	size    int64
	modTime time.Time
	prompt  string
	reply   string
}

func (p *Runner) SetInput(includeArchived bool, selectedID string) {
	p.mu.Lock()
	p.includeArchived, p.selectedID = includeArchived, selectedID
	p.mu.Unlock()
}

func (p *Runner) RequestRefresh() {
	select {
	case p.poke <- struct{}{}:
	default:
	}
}

// Run keeps the newest snapshot while its subscriber is suspended.
// The first unread error takes priority over later results.
// Cancellation is observed between steps; tmux calls already in flight finish first.
func (p *Runner) Run(ctx context.Context) <-chan Result { return p.run(ctx, p.Step) }

func (p *Runner) run(ctx context.Context, step func() Result) <-chan Result {
	results := make(chan Result, 1)
	if !p.running.CompareAndSwap(false, true) {
		results <- Result{Err: errors.New("execution runner already started")}
		close(results)
		return results
	}
	go func() {
		defer close(results)
		defer func() { p.runMu.Lock(); p.stopped = true; p.runMu.Unlock(); p.captureWait.Wait() }()
		ticker := time.NewTicker(p.interval)
		defer ticker.Stop()
		for {
			if ctx.Err() != nil {
				return
			}
			result := step()
			if ctx.Err() != nil {
				return
			}
			select {
			case results <- result:
			default:
				select {
				case unread := <-results:
					if unread.Err != nil {
						result = unread
					}
				default:
				}
				select {
				case results <- result:
				case <-ctx.Done():
					return
				}
			}
			select {
			case <-ticker.C:
			case <-p.poke:
			case <-ctx.Done():
				return
			}
		}
	}()
	return results
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

// storedPreview serves a session's saved pane snapshot, which is what the
// preview shows for any session with no window left to capture, however it
// lost it. It backfills from a still-live tmux window for sessions archived
// before snapshots existed.
func StoredPreview(st *store.Store, driver *tmux.Driver, sessID string) (string, error) {
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
// call, and every process tree from one ps pass with a second scoped to
// the pane children, so the poll cost stays flat as sessions are added.
// runMu serializes the loop with one-off refreshes issued as tea commands.
func (p *Runner) Step() Result {
	p.runMu.Lock()
	defer p.runMu.Unlock()
	if p.stopped {
		return Result{Err: errors.New("execution runner stopped")}
	}
	p.mu.Lock()
	includeArchived := p.includeArchived
	selectedID := p.selectedID
	captureErr := p.captureErr
	p.captureErr = nil
	p.mu.Unlock()
	if captureErr != nil {
		return Result{Err: captureErr}
	}
	// Machine gauges change slowly; sample them every other poll.
	sampleStats := p.tick%2 == 0
	socket := p.tmux.SocketPath()
	// Delivering a queued message needs this process, so stamp a heartbeat
	// the session tools can read to tell a sender whether anyone is home.
	// The same write claims the store for this manager's tmux server, which
	// decides who speaks for sessions no server has claimed yet. Claiming
	// every poll would be a write transaction every couple of seconds for as
	// long as the manager is open; its readers allow the stamp to age instead.
	if time.Since(p.heartbeatAt) >= store.PollerHeartbeatPeriod {
		claimed := time.Now()
		holder, err := p.store.ClaimPoller(socket, claimed, p.interval)
		if err != nil {
			return Result{Err: err}
		}
		p.leading = holder == socket
		p.heartbeatAt = claimed
	}
	leading := p.leading
	if p.tick%inboxPruneEvery == 0 {
		if err := p.inboxOwner.MaintainInbox(); err != nil {
			return Result{Err: err}
		}
	}
	p.tick++

	listedAt := time.Now()
	sessions, err := p.store.ListSessions(includeArchived)
	if err != nil {
		return Result{Err: err}
	}

	panes, err := p.tmux.Panes()
	if err != nil {
		return Result{Err: err}
	}
	for id, pane := range panes {
		if pane.Path != "" {
			info, err := os.Stat(pane.Path)
			if err != nil || !info.IsDir() {
				pane.Path = ""
				panes[id] = pane
			}
		}
	}
	var livePIDs []int
	for _, sess := range sessions {
		if !sess.Archived && panes[sess.ID].PID > 0 {
			livePIDs = append(livePIDs, panes[sess.ID].PID)
		}
	}
	trees := sysstat.Trees(livePIDs)
	ncpu := sysstat.LogicalCPUs()
	memTotal, _ := sysstat.MemTotalBytes()
	now := time.Now()
	elapsed := now.Sub(p.prevTreeAt).Seconds()
	haveDelta := !p.prevTreeAt.IsZero() && elapsed > 0.05
	nextTreeCPU := make(map[int]float64, len(livePIDs))

	preview := ""
	var proc sysstat.ProcStat
	var agents AgentStats
	var cpuSecDelta float64
	paneHashes := make(map[string]uint64, len(sessions))
	paneLastLines := make(map[string]string, len(sessions))
	panePrompts := make(map[string]string, len(sessions))
	for i, sess := range sessions {
		if sess.Archived {
			continue
		}
		// A session belongs to the tmux server its pane runs on. Panes on
		// another server are invisible from here, and reading that silence
		// as a dead agent is how a second manager stamps dead over sessions
		// that are alive and alerts their owner's user for every flip back.
		if sess.TmuxSocket != "" && sess.TmuxSocket != socket {
			continue
		}
		live := panes[sess.ID].PID > 0
		claimed, delivered := false, false
		if sess.TmuxSocket == "" {
			// Sessions that predate the column are the leading manager's to
			// speak for until one of them shows a pane here to claim.
			if !live && !leading {
				continue
			}
			if live {
				if err := ignoreDeletedSession(p.store.SetTmuxSocket(sess.ID, socket)); err != nil {
					return Result{Err: err}
				}
				sessions[i].TmuxSocket = socket
				claimed = true
			}
		}
		if err := p.applyPendingRename(&sessions[i]); err != nil {
			return Result{Err: err}
		}
		if err := p.applyPendingReviewRepo(&sessions[i]); err != nil {
			return Result{Err: err}
		}
		if err := p.applyPendingReviewBase(&sessions[i]); err != nil {
			return Result{Err: err}
		}
		if err := p.applyPendingReviewScope(&sessions[i]); err != nil {
			return Result{Err: err}
		}
		newStatus := status.Dead
		if pid := panes[sess.ID].PID; pid > 0 {
			stat := trees[pid]
			if stat.OK {
				nextTreeCPU[pid] = stat.CPUSeconds
				agents.Count++
				agents.RSS += stat.RSS
				var hostCPU float64
				if haveDelta {
					delta := stat.CPUSeconds - p.prevTreeCPU[pid]
					if delta < 0 {
						delta = 0
					}
					cpuSecDelta += delta
					hostCPU = sysstat.HostCPUFromDelta(delta, elapsed, ncpu)
				} else {
					hostCPU = sysstat.HostCPUPercent(stat.PCPU, ncpu)
				}
				stat.CPUPercent = hostCPU
				stat.RamPercent = sysstat.HostRAMPercent(stat.RSS, memTotal)
				if !haveDelta {
					// First sample: fleet CPU still uses pcpu sum path below.
					agents.CPU += stat.PCPU
				}
				if sess.ID == selectedID {
					proc = stat
				}
				if err := p.applyRelaunchedTool(&sessions[i], stat.Children); err != nil {
					return Result{Err: err}
				}
				// The rest of this pass reads its own copy of the row, so a
				// retyped session derives its status under the new tool's
				// rules this tick rather than the next one.
				sess.Tool, sess.AgentSessionID = sessions[i].Tool, sessions[i].AgentSessionID
			}
			// The pane pid is the shell; the agent runs as its child. A
			// tree of one process means the agent is gone. A failed ps
			// sample proves nothing, so it counts as alive.
			agentAlive := !stat.OK || stat.Procs > 1
			if pane, err := p.tmux.CapturePane(sess.ID); err == nil {
				paneLastLines[sess.ID], panePrompts[sess.ID] = p.rowLines(sess, pane, panes[sess.ID].Path)
				derived, err := p.derivePaneStatus(sess, pane, agentAlive, paneHashes)
				if err != nil {
					return Result{Err: err}
				}
				newStatus = derived
				// Hold the launch state until the agent first paints its pane,
				// so a just-created session reads "starting up" rather than
				// flashing idle before it has booted. A grace cap keeps a tool
				// that never paints from sticking on starting forever.
				if sess.Status == status.Starting && !paneBooted(pane) &&
					time.Since(sess.LaunchTime()) < startingGrace {
					newStatus = status.Starting
				}
				sent, err := p.maybeSendPendingInputWhenReady(sess, pane, newStatus, agentAlive)
				if err != nil {
					return Result{Err: err}
				}
				if sent {
					if prompt := launch.DeliveredPrompt(sess.PendingInputs[0]); prompt != "" {
						if err := ignoreDeletedSession(p.store.SetLastPrompt(sess.ID, prompt)); err != nil {
							return Result{Err: err}
						}
						sessions[i].LastPrompt = prompt
					}
					sessions[i].PendingInputs = sessions[i].PendingInputs[1:]
				}
				// Launch inputs open the conversation, so they go first; a
				// message from another agent waits its turn behind them.
				// A launch input sent this tick leaves pane and derived
				// describing the moment before it was typed, so the message
				// waits for the next capture rather than landing on a pane
				// that is already starting a turn.
				if !sent && len(sessions[i].PendingInputs) == 0 {
					delivered, err = p.maybeDeliverInbox(sess, pane, derived, agentAlive)
					if err != nil {
						return Result{Err: err}
					}
					// Typing it in starts a turn only the next capture can show.
					if delivered {
						newStatus = status.Working
					}
				}
				// Any real transition re-arms the finished alert.
				if sess.Acked && newStatus != status.Idle && newStatus != status.Finished {
					if err := ignoreDeletedSession(p.store.SetAcked(sess.ID, false)); err != nil {
						return Result{Err: err}
					}
					sessions[i].Acked = false
				}
				if sess.ID == selectedID {
					preview = pane
				}
			}
		}
		// A row claimed on this pass is written even when the status did not
		// move, because the row was anyone's until the claim: a manager that
		// cannot see this pane may have stamped it dead since this pass read
		// the list, and that stamp is corrected here rather than a poll later.
		// A delivery rewrites it too, stamping the status after the message.
		if newStatus != sess.Status || claimed || delivered {
			// The row can be claimed by the manager that can see its pane
			// between this pass listing it and reaching here, and a status
			// derived without that pane must not land on top of the claim.
			written, err := p.store.UpdateStatusOnSocket(sess.ID, newStatus, socket)
			if err != nil {
				return Result{Err: err}
			}
			if written && newStatus != sess.Status {
				sessions[i].Status = newStatus
				p.notifyTransition(sess, newStatus)
			}
		}
	}
	if preview == "" && selectedID != "" {
		for _, sess := range sessions {
			if sess.ID == selectedID && (sess.Archived || panes[sess.ID].PID == 0) {
				snapshot, err := StoredPreview(p.store, p.tmux, sess.ID)
				if err != nil {
					return Result{Err: err}
				}
				preview = snapshot
				break
			}
		}
	}
	p.startCaptureIfIdle(sessions, panes)
	p.paneHashes = paneHashes

	groups, err := p.store.Groups()
	if err != nil {
		return Result{Err: err}
	}
	// Counted after the delivery loop, so a message typed into a pane on
	// this pass has already dropped out of the badge.
	queued, err := p.store.QueuedCounts()
	if err != nil {
		return Result{Err: err}
	}
	names := make([]string, len(groups))
	paths := make(map[string]string, len(groups))
	worktrees := make(map[string]string, len(groups))
	archivedGroups := make(map[string]bool, len(groups))
	for i, g := range groups {
		names[i] = g.Name
		paths[g.Name] = g.Path
		if g.Worktree != "" {
			worktrees[g.Name] = g.Worktree
		}
		if g.Archived {
			archivedGroups[g.Name] = true
		}
	}

	if agents.Count > 0 {
		if haveDelta {
			agents.CPU = sysstat.HostCPUFromDelta(cpuSecDelta, elapsed, ncpu)
		} else {
			agents.CPU = sysstat.HostCPUPercent(agents.CPU, ncpu)
		}
		agents.RAM = sysstat.HostRAMPercent(agents.RSS, memTotal)
	}
	p.prevTreeCPU = nextTreeCPU
	p.prevTreeAt = now

	msg := Snapshot{
		TmuxSocket:     socket,
		LeadingManager: leading,
		Sessions:       sessions,
		ListedAt:       listedAt,
		Groups:         names,
		GroupPaths:     paths,
		GroupWorktrees: worktrees,
		ArchivedGroups: archivedGroups,
		Proc:           proc,
		ProcFor:        selectedID,
		Preview:        preview,
		Agents:         agents,
		QueuedMessages: queued,
		PaneLines:      paneLastLines,
		PanePrompts:    panePrompts,
		Panes:          panes,
	}
	if p.takeFocus != nil {
		if id, ok := p.takeFocus(); ok {
			msg.FocusID = id
		}
	}
	if sampleStats {
		msg.Snap = sysstat.Sample("/")
		msg.SnapOK = true
	}
	return Result{Snapshot: msg}
}

const (
	// inboxPruneEvery keeps the delivered-message sweep off the hot path;
	// at the default 2s interval this is roughly every ten minutes.
	inboxPruneEvery = 300
	// inboxClaimGrace is how long a claim may sit undelivered before the
	// message counts as abandoned. Claiming and pasting are two steps, and
	// nothing stops a second manager polling the same store, so a claim
	// that is milliseconds old belongs to a paste in flight; only one this
	// old belongs to a manager that died between the two.
	inboxClaimGrace = 30 * time.Second
)

// launchPromptGrace releases pending input for a session whose prompt
// scrolled out of the pane, or whose agent never drew it there.
const launchPromptGrace = 30 * time.Second

// reflowSessions drops activity-region hashes for ids and runs reflow
// while the Runner is paused (runMu held). A poll must not capture mid-
// resize against a pre-resize hash: that comparison treats reflow as
// streaming and flashes every session as working for one tick.
func (p *Runner) ReflowSessions(ids []string, reflow func()) {
	if len(ids) == 0 {
		return
	}
	p.runMu.Lock()
	defer p.runMu.Unlock()
	if p.stopped {
		return
	}
	for _, id := range ids {
		delete(p.paneHashes, id)
		delete(p.quietSince, id)
	}
	reflow()
}

func sessionLabel(group, name string) string {
	if group == "" {
		return name
	}
	return group + " · " + name
}

const notificationsSetting = "notifications"
const notifyFinishedSetting = "notify_finished"
