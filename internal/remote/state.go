package remote

import (
	"context"
	"time"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
)

// managerStartInterval spaces the attempts to start a manager on a host
// that keeps reporting none awake, so a failing start is not retried on
// every poll.
const managerStartInterval = time.Minute

// HostState is what the last refreshes of one connection left behind.
type HostState struct {
	// Snapshot is the last good one, kept while the host is unreachable.
	Snapshot sessioncmd.Snapshot
	// OK reports whether the last snapshot succeeded.
	OK bool
	// Err is the last refresh's failure: the snapshot's, or starting the
	// manager's when the snapshot succeeded.
	Err    error
	LastOK time.Time
	// Failures counts the snapshots that failed since the last good one.
	Failures int
	// StartedManager reports that the last refresh started a manager.
	StartedManager bool
}

type hostRecord struct {
	state       HostState
	destination string
}

// Refresh takes a snapshot of host and caches the outcome for State.
func (c *Client) Refresh(ctx context.Context, host string) HostState {
	conn, err := c.connection(host)
	if err != nil {
		return HostState{Err: err}
	}
	snapshot, err := c.Snapshot(ctx, host)
	state := c.State(host)
	state.StartedManager = false
	if err != nil {
		state.OK = false
		state.Err = err
		state.Failures++
		return c.record(conn, state)
	}
	state = HostState{Snapshot: snapshot, OK: true, LastOK: c.now()}
	if !snapshot.ManagerAwake && c.claimManagerStart(host) {
		state.StartedManager, state.Err = c.StartManager(ctx, host)
	}
	return c.record(conn, state)
}

// State returns the cached outcome of host's refreshes without any I/O.
func (c *Client) State(host string) HostState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.states[host].state
}

// record drops a result for a connection removed or repointed while the
// refresh ran, so it cannot land on the new one.
func (c *Client) record(conn Connection, state HostState) HostState {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.unchanged(conn.Name, conn.Destination) {
		c.states[conn.Name] = hostRecord{state: state, destination: conn.Destination}
	}
	return state
}

func (c *Client) claimManagerStart(host string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if last, ok := c.starts[host]; ok && now.Sub(last) < managerStartInterval {
		return false
	}
	c.starts[host] = now
	return true
}
