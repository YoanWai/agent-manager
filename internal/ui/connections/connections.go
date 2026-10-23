// Package connections is the SSH connections in the UI: the stored list,
// what each host's last refresh left, which refreshes are in flight, the
// row a selection names, the selected row's screen and the add and edit
// dialog. It works on copied values; the root adapters run the SSH client,
// the store and the effect lane, and paint the rows into the rail.
package connections

import (
	"slices"
	"time"

	remote "github.com/YoanWai/agent-manager/internal/remote/value"
)

// Host is what the last refreshes of one connection left, copied from the
// client. The rows are the last good snapshot's, kept while the host is
// unreachable.
type Host struct {
	OK bool
	// Err is the last refresh's failure, and Trouble its kind.
	Err      string
	Trouble  remote.Trouble
	Failures int
	// Attempted is when the last refresh ran, successful or not.
	Attempted    time.Time
	ManagerAwake bool
	Groups       []Group
	Sessions     []Session
	Terminals    []Session
}

type Group struct {
	Path     string
	Archived bool
}

// Session is an agent or a terminal on a host, as its snapshot listed it.
type Session struct {
	ID, Name, Tool, Directory, Group, Status, ParentID string
	Archived                                           bool
}

// Connections holds the list and each host's state. gen counts list
// changes, so a refresh of a connection since removed or repointed cannot
// land on the list that replaced it.
type Connections struct {
	list    []remote.Connection
	gen     uint64
	hosts   map[string]Host
	polling map[string]bool
	Preview Preview
	Dialog  Dialog
}

// SetList adopts the stored connections. A host no longer listed, or
// listed with another destination or path, loses its last refresh.
func (c *Connections) SetList(list []remote.Connection) {
	c.gen++
	previous := c.list
	c.list = slices.Clone(list)
	kept := map[string]Host{}
	for _, conn := range c.list {
		if host, ok := c.hosts[conn.Name]; ok && slices.Contains(previous, conn) {
			kept[conn.Name] = host
		}
	}
	c.hosts = kept
	c.polling = map[string]bool{}
}

func (c *Connections) List() []remote.Connection { return c.list }

func (c *Connections) Gen() uint64 { return c.gen }

func (c *Connections) Connection(name string) (remote.Connection, bool) {
	for _, conn := range c.list {
		if conn.Name == name {
			return conn, true
		}
	}
	return remote.Connection{}, false
}

// Host is the host's last refresh; seen is false before its first.
func (c *Connections) Host(name string) (host Host, seen bool) {
	host, seen = c.hosts[name]
	return host, seen
}

// StartRefresh claims a refresh of host, unless it is no longer listed,
// one is already in flight, or it failed and is still resting.
func (c *Connections) StartRefresh(host string, now time.Time) bool {
	if _, listed := c.Connection(host); !listed || c.polling[host] || c.resting(host, now) {
		return false
	}
	c.polling[host] = true
	return true
}

// Refreshed records a refresh of host started for list generation gen,
// and reports whether it landed.
func (c *Connections) Refreshed(host string, gen uint64, state Host) bool {
	if gen != c.gen {
		return false
	}
	delete(c.polling, host)
	c.hosts[host] = state
	return true
}

// resting reports whether a host that failed its last refresh is still
// inside the wait before its next try.
func (c *Connections) resting(host string, now time.Time) bool {
	state, seen := c.hosts[host]
	return seen && !state.OK && now.Sub(state.Attempted) < remote.RetryWait(state.Failures)
}

// Offline is the refusal for a call to a host whose last refresh failed;
// a host never reached yet is still tried.
func (c *Connections) Offline(host string) (string, bool) {
	state, seen := c.hosts[host]
	if !seen || state.OK {
		return "", false
	}
	return state.Err, true
}
