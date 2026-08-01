package tmux

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Control is a persistent control-mode client attached to one session.
// tmux pushes an %output notification the moment the pane paints, and
// commands ride the same pipe, so a focused preview needs no per-tick
// process forks and no polling: wait for Events, then Command a capture.
type Control struct {
	cmd *exec.Cmd

	// writeMu serializes stdin writes and, held across the queue append,
	// keeps write order identical to waiter order. Writes never run under
	// mu: the read loop needs mu to resolve replies, and a write blocked
	// on a full pipe while holding it would stop stdout draining - a
	// cycle no timeout breaks.
	writeMu sync.Mutex
	stdin   io.WriteCloser

	mu      sync.Mutex
	pending []chan reply
	closed  bool
	// greeted flips once the connect-time block has been consumed; until
	// then no reply block may resolve a command waiter.
	greeted bool

	// events coalesces %output notifications: it holds at most one signal,
	// so a burst of pane writes reads as "something changed" once.
	events chan struct{}
	// done closes when the control client exits (detach, kill, or error).
	done    chan struct{}
	exitErr error
}

type reply struct {
	text string
	err  error
}

// OpenControl attaches a control-mode client to a live session.
func (d *Driver) OpenControl(id string) (*Control, error) {
	cmd := exec.Command(d.bin, d.args("-C", "attach-session", "-t", sessionName(id))...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("control stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("control stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("control attach: %w", err)
	}
	control := newControl(stdin, stdout)
	control.cmd = cmd
	go func() {
		<-control.done
		cmd.Wait()
	}()
	return control, nil
}

// newControl wires the protocol loop over raw pipes. Split from OpenControl
// so tests can drive the parser without a tmux server.
func newControl(stdin io.WriteCloser, stdout io.Reader) *Control {
	control := &Control{
		stdin:  stdin,
		events: make(chan struct{}, 1),
		done:   make(chan struct{}),
	}
	go control.readLoop(stdout)
	return control
}

// Events signals whenever the session's panes produce output. Coalesced:
// a burst of writes may arrive as a single signal.
func (c *Control) Events() <-chan struct{} { return c.events }

// Done closes when the client exits; Err then reports why.
func (c *Control) Done() <-chan struct{} { return c.done }

func (c *Control) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.exitErr
}

// commandTimeout bounds how long a Command waits for its reply. Commands
// run synchronously on the UI loop, so a tmux server that stops answering
// must cost a beat, never a frozen interface.
const commandTimeout = 2 * time.Second

// Command runs one tmux command over the control pipe and returns its
// output. Replies arrive strictly in command order, so each call enqueues
// a waiter that the read loop resolves from the front of the queue.
func (c *Control) Command(command string) (string, error) {
	waiter, err := c.submit(command)
	if err != nil {
		return "", err
	}
	timeout := time.NewTimer(commandTimeout)
	defer timeout.Stop()
	select {
	case result := <-waiter:
		return result.text, result.err
	case <-c.done:
		return "", fmt.Errorf("control client exited")
	case <-timeout.C:
		// The waiter stays queued: if the reply does arrive later it pops
		// this abandoned (buffered) channel and the queue stays aligned.
		return "", fmt.Errorf("control reply timed out")
	}
}

// Send writes one tmux command and returns without waiting for its reply.
// For keystroke forwarding: once the write lands the pane owns the key,
// and waiting for the acknowledgement would only stall the caller. The
// discarded reply still pops this command's own waiter, so the queue
// stays aligned.
func (c *Control) Send(command string) error {
	_, err := c.submit(command)
	return err
}

// submit enqueues a reply waiter and writes the command line. The queue
// append rides inside the write lock so waiter order always matches write
// order, while mu itself is never held across the pipe write.
func (c *Control) submit(command string) (chan reply, error) {
	waiter := make(chan reply, 1)
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, fmt.Errorf("control client closed")
	}
	c.pending = append(c.pending, waiter)
	c.mu.Unlock()
	if _, err := io.WriteString(c.stdin, command+"\n"); err != nil {
		// Nothing went out, so no reply will come: leaving the waiter
		// queued would shift every later reply onto the wrong caller.
		c.mu.Lock()
		for i := len(c.pending) - 1; i >= 0; i-- {
			if c.pending[i] == waiter {
				c.pending = append(c.pending[:i], c.pending[i+1:]...)
				break
			}
		}
		c.mu.Unlock()
		return nil, fmt.Errorf("control write: %w", err)
	}
	return waiter, nil
}

// Close detaches the client. tmux exits on stdin EOF; one that ignores it
// is killed after a bounded wait so the caller can never hang here.
func (c *Control) Close() error {
	c.mu.Lock()
	closed := c.closed
	c.closed = true
	c.mu.Unlock()
	if closed {
		return nil
	}
	c.stdin.Close()
	timeout := time.NewTimer(commandTimeout)
	defer timeout.Stop()
	select {
	case <-c.done:
	case <-timeout.C:
		if c.cmd != nil && c.cmd.Process != nil {
			c.cmd.Process.Kill()
		}
		<-c.done
	}
	return nil
}

func (c *Control) readLoop(stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	// capture-pane of a colored 200-column pane produces lines far past
	// bufio's 64KB default.
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var block []string
	inBlock := false
	blockTag := ""
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case !inBlock && strings.HasPrefix(line, "%begin "):
			inBlock = true
			blockTag = blockID(line)
			block = block[:0]
		case inBlock && blockEnd(line, blockTag):
			c.resolve(strings.Join(block, "\n"), strings.HasPrefix(line, "%error "))
			inBlock = false
		case inBlock:
			// Everything else inside a block is command output verbatim -
			// including pane text that happens to start with %begin or
			// %end. Only the terminator carrying this block's own tag ends
			// it; anything less desyncs every reply after this one.
			block = append(block, line)
		case strings.HasPrefix(line, "%output "):
			select {
			case c.events <- struct{}{}:
			default:
			}
		}
		// Other notifications (%session-changed, %layout-change, %exit …)
		// carry nothing the preview needs.
	}
	c.fail(scanner.Err())
}

// blockID is the "<timestamp> <number>" pair tmux stamps on %begin and
// repeats on the matching %end/%error.
func blockID(line string) string {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return ""
	}
	return fields[1] + " " + fields[2]
}

// blockEnd reports whether line terminates the block tagged tag.
func blockEnd(line, tag string) bool {
	if !strings.HasPrefix(line, "%end ") && !strings.HasPrefix(line, "%error ") {
		return false
	}
	return blockID(line) == tag
}

// resolve hands a finished reply block to the oldest waiter. tmux emits one
// unsolicited block when the client connects, before it reads any command,
// so the first block is always the greeting and never resolves a waiter.
func (c *Control) resolve(text string, isError bool) {
	c.mu.Lock()
	if !c.greeted {
		c.greeted = true
		c.mu.Unlock()
		return
	}
	var waiter chan reply
	if len(c.pending) > 0 {
		waiter = c.pending[0]
		c.pending = c.pending[1:]
	}
	c.mu.Unlock()
	if waiter == nil {
		return
	}
	if isError {
		waiter <- reply{err: fmt.Errorf("tmux control: %s", text)}
		return
	}
	waiter <- reply{text: text}
}

// fail ends the client: records the read error, wakes every waiter via
// done, and marks the client closed so new commands are refused.
func (c *Control) fail(err error) {
	c.mu.Lock()
	c.closed = true
	c.exitErr = err
	c.pending = nil
	c.mu.Unlock()
	close(c.done)
}
