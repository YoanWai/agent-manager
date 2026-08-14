package sessioncmd

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
)

// Waiting on another session is one parked tool call instead of a polling
// loop that reads a pane, and a screen read per tick is what makes an
// orchestrating agent expensive.
const (
	DefaultWaitTimeout = 50 * time.Second
	// MaxWaitTimeout is generous, but clients cut a call short on their own
	// schedule: Codex CLI stops at 60s by default. A caller asking for more
	// gets it only where its own client allows it.
	MaxWaitTimeout = 5 * time.Minute
	minWaitPoll    = 500 * time.Millisecond

	WaitReached  = "reached"
	WaitTimedOut = "timed_out"
	WaitDied     = "died"
	// existsEvery keeps the liveness check off most ticks, since it forks a
	// tmux process, while the status read is a cheap indexed lookup.
	existsEvery = 4
)

// restingStates is what "the session stopped working" means. Finished is
// rewritten to idle once the manager acknowledges it, and a manager tick
// can pass through both between two polls, so waiting on the whole set is
// the only way not to miss the moment.
var restingStates = []string{status.Finished, status.Waiting, status.Idle, status.Errored, status.Dead}

type WaitResult struct {
	Session      Session `json:"session"`
	Reached      bool    `json:"reached" jsonschema:"true when the session reached one of the awaited states"`
	Outcome      string  `json:"outcome" jsonschema:"reached (it arrived at an awaited state), died (its pane is gone and dead was not among them), or timed_out"`
	Waited       string  `json:"waited" jsonschema:"how long the wait lasted"`
	ManagerAwake bool    `json:"manager_awake" jsonschema:"whether Agent Manager is running; session status only advances while it is, so a wait against a closed manager can only ever observe dead"`
}

func normalizeWaitStates(until []string) ([]string, error) {
	if len(until) == 0 {
		return restingStates, nil
	}
	known := map[string]bool{
		status.Starting: true, status.Working: true, status.Waiting: true,
		status.Finished: true, status.Idle: true, status.Errored: true, status.Dead: true,
	}
	seen := map[string]bool{}
	states := make([]string, 0, len(until))
	for _, raw := range until {
		state := strings.ToLower(strings.TrimSpace(raw))
		if !known[state] {
			valid := make([]string, 0, len(known))
			for name := range known {
				valid = append(valid, name)
			}
			sort.Strings(valid)
			return nil, fmt.Errorf("unknown state %q; valid states are %s", raw, strings.Join(valid, ", "))
		}
		if !seen[state] {
			seen[state] = true
			states = append(states, state)
		}
	}
	return states, nil
}

// Wait parks until the target session reaches one of the given states, or
// the timeout expires. A timeout is an outcome rather than a failure: the
// caller gets the session's current state and decides what to do with it.
func (s *Sessions) Wait(ctx context.Context, sessionID, targetID string, until []string, timeout time.Duration) (WaitResult, error) {
	states, err := normalizeWaitStates(until)
	if err != nil {
		return WaitResult{}, err
	}
	switch {
	case timeout < 0 || timeout > MaxWaitTimeout:
		return WaitResult{}, fmt.Errorf("timeout %s is outside 0 to %s; wait again when this one returns", timeout, MaxWaitTimeout)
	case timeout == 0:
		timeout = DefaultWaitTimeout
	}
	runtime, err := s.open()
	if err != nil {
		return WaitResult{}, err
	}
	defer runtime.store.Close()
	caller, err := runtime.caller(sessionID)
	if err != nil {
		return WaitResult{}, err
	}
	target, err := runtime.agent(targetID)
	if err != nil {
		return WaitResult{}, err
	}
	if target.ID == caller.ID {
		return WaitResult{}, errors.New("a session cannot wait on itself; it is the one making this call")
	}
	if target.Archived {
		return WaitResult{}, fmt.Errorf("session %s is archived, so its status no longer advances; restore it with %s first", target.ID, runtime.words.Restore)
	}

	wanted := map[string]bool{}
	for _, state := range states {
		wanted[state] = true
	}
	poll := runtime.cfg.PollInterval.Duration
	if poll < minWaitPoll {
		poll = minWaitPoll
	}
	started := time.Now()
	deadline := started.Add(timeout)
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	// Waking only on the poll would overshoot a timeout shorter than the
	// interval, which is the one thing a caller asked this call to bound.
	expiry := time.NewTimer(time.Until(deadline))
	defer expiry.Stop()

	for tick := 0; ; tick++ {
		current, err := runtime.agent(target.ID)
		if err != nil {
			return WaitResult{}, err
		}
		// The manager writes status only for a session it can still see, so
		// a pane that vanished reads dead here before the next poll lands.
		running := true
		if tick%existsEvery == 0 {
			running = runtime.driver.Exists(current.ID)
		}
		state := current.Status
		if !running {
			state = status.Dead
		}
		// A death ends the wait as surely as an awaited state does: the
		// status of a session with no pane will never move again.
		if wanted[state] || !running || !time.Now().Before(deadline) {
			// The cheap ticks trust the stored status; the answer we hand
			// back is worth one more fork to get right.
			running = runtime.driver.Exists(current.ID)
			if !running {
				state = status.Dead
			}
			outcome := WaitTimedOut
			switch {
			case wanted[state]:
				outcome = WaitReached
			case !running:
				outcome = WaitDied
			}
			return runtime.waitResult(current, running, state, started, outcome)
		}
		select {
		case <-ctx.Done():
			return WaitResult{}, ctx.Err()
		case <-ticker.C:
		case <-expiry.C:
		}
	}
}

func (r *runtime) waitResult(sess store.Session, running bool, state string, started time.Time, outcome string) (WaitResult, error) {
	awake, err := r.managerAwake(time.Now())
	if err != nil {
		return WaitResult{}, err
	}
	info := r.sessionInfo(sess, running, false)
	info.Status = state
	return WaitResult{
		Session:      info,
		Reached:      outcome == WaitReached,
		Outcome:      outcome,
		Waited:       time.Since(started).Round(time.Second).String(),
		ManagerAwake: awake,
	}, nil
}
