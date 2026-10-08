// Package remote drives the agent-manager on another machine over SSH,
// through that machine's own CLI.
package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	callTimeout   = 15 * time.Second
	createTimeout = 60 * time.Second
	maxInFlight   = 4
	// exitSSH is ssh's own failure; the remote command's status passes
	// through otherwise.
	exitSSH = 255
	// exitNotFound is the login shell's answer to a command not on PATH.
	exitNotFound = 127
)

type Client struct {
	controlDir string
	run        Runner
	now        func() time.Time
	inFlight   chan struct{}

	mu          sync.Mutex
	connections []Connection
	hostSlots   map[string]chan struct{}
	states      map[string]hostRecord
	starts      map[string]time.Time
}

type Option func(*Client)

// WithRunner replaces ssh, so a test sees the exact argv.
func WithRunner(run Runner) Option {
	return func(c *Client) { c.run = run }
}

func WithClock(now func() time.Time) Option {
	return func(c *Client) { c.now = now }
}

// New keeps one control socket directory per profile, so two profiles on
// one machine never share an SSH master.
func New(profileDir string, opts ...Option) *Client {
	c := &Client{
		controlDir: controlDir(profileDir),
		run:        runSSH,
		now:        time.Now,
		inFlight:   make(chan struct{}, maxInFlight),
		hostSlots:  map[string]chan struct{}{},
		states:     map[string]hostRecord{},
		starts:     map[string]time.Time{},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// SetConnections replaces the connection list. A connection that is gone,
// or now points at another destination, loses its cached state.
func (c *Client) SetConnections(connections []Connection) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connections = slices.Clone(connections)
	for name, record := range c.states {
		if !c.unchanged(name, record.destination) {
			delete(c.states, name)
			delete(c.starts, name)
		}
	}
}

func (c *Client) Connections() []Connection {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.connections)
}

func (c *Client) connection(host string) (Connection, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, conn := range c.connections {
		if conn.Name == host {
			return conn, nil
		}
	}
	return Connection{}, &Error{Host: host, Err: errors.New("no connection has that name")}
}

// unchanged reports whether host still names destination; c.mu is held.
func (c *Client) unchanged(host, destination string) bool {
	for _, conn := range c.connections {
		if conn.Name == host {
			return conn.Destination == destination
		}
	}
	return false
}

func (c *Client) hostSlot(host string) chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	slot, ok := c.hostSlots[host]
	if !ok {
		slot = make(chan struct{}, 1)
		c.hostSlots[host] = slot
	}
	return slot
}

// call runs agent-manager on host with args. The host's slot is taken
// before the global one, so a call queued behind its own host never holds
// a slot another host could use. stdout comes back on failure too.
func (c *Client) call(ctx context.Context, host string, timeout time.Duration, args ...string) ([]byte, error) {
	conn, err := c.connection(host)
	if err != nil {
		return nil, err
	}
	slot := c.hostSlot(host)
	select {
	case slot <- struct{}{}:
		defer func() { <-slot }()
	case <-ctx.Done():
		return nil, &Error{Host: host, Err: ctx.Err()}
	}
	select {
	case c.inFlight <- struct{}{}:
		defer func() { <-c.inFlight }()
	case <-ctx.Done():
		return nil, &Error{Host: host, Err: ctx.Err()}
	}
	if err := ensureControlDir(c.controlDir); err != nil {
		return nil, &Error{Host: host, Err: err}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	argv := c.sshArgv(conn.Destination, false, append([]string{"agent-manager"}, args...))
	stdout, stderr, err := c.run(ctx, argv)
	if err != nil {
		return stdout, classify(ctx, host, stderr, err)
	}
	return stdout, nil
}

// Error is a failed call to one connection.
type Error struct {
	Host string
	Err  error
	// sshFailed marks ssh's own exit status, as opposed to the remote
	// command's.
	sshFailed bool
}

func (e *Error) Error() string {
	return e.Host + ": " + e.Err.Error()
}

func (e *Error) Unwrap() error {
	return e.Err
}

// Unreachable reports a failure of the connection itself rather than of the
// remote command: ssh failed, or the host did not answer in time.
func Unreachable(err error) bool {
	var remote *Error
	return errors.As(err, &remote) && remote.sshFailed || errors.Is(err, context.DeadlineExceeded)
}

func classify(ctx context.Context, host string, stderr []byte, err error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return &Error{Host: host, Err: fmt.Errorf("did not answer in time: %w", context.DeadlineExceeded)}
	}
	if ctx.Err() != nil {
		return &Error{Host: host, Err: ctx.Err()}
	}
	text := lastLine(stderr)
	var exited interface{ ExitCode() int }
	if !errors.As(err, &exited) {
		return &Error{Host: host, Err: err}
	}
	switch exited.ExitCode() {
	case exitSSH:
		if text == "" {
			text = err.Error()
		}
		return &Error{Host: host, Err: errors.New("unreachable over SSH: " + text), sshFailed: true}
	case exitNotFound:
		return &Error{Host: host, Err: errors.New("agent-manager is not on the login shell's PATH there; install it or add it to PATH in the login profile")}
	}
	if text == "" {
		return &Error{Host: host, Err: err}
	}
	return &Error{Host: host, Err: errors.New(strings.TrimPrefix(text, "agent-manager: "))}
}

// lastLine skips whatever a login profile or an ssh warning printed before
// the line that says what went wrong.
func lastLine(output []byte) string {
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func (c *Client) decode(ctx context.Context, host string, timeout time.Duration, into any, args ...string) error {
	out, err := c.call(ctx, host, timeout, args...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(out, into); err != nil {
		return &Error{Host: host, Err: fmt.Errorf("unreadable answer to %s: %w", args[0], err)}
	}
	return nil
}
