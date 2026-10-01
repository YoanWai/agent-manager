package tmux

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/semaphore"
)

// Control is a persistent control-mode client attached to one session.
// tmux pushes an %output notification the moment the pane paints, and
// commands ride the same pipe, so a focused preview needs no per-tick
// process forks and no polling: wait for Events, then Command a capture.
type Control struct {
	cmd    *exec.Cmd
	socket string

	// writeGate serializes stdin writes and, held across the queue append,
	// keeps write order identical to waiter order. A channel rather than a
	// mutex lets a canceled caller leave while another write is in flight.
	writeGate chan struct{}
	stdin     io.WriteCloser

	mu      sync.Mutex
	pending []chan reply
	closed  bool
	// greeted flips once the connect-time block has been consumed; until
	// then no reply block may resolve a command waiter.
	greeted bool

	// events coalesces %output notifications: it holds at most one signal,
	// so a burst of pane writes reads as "something changed" once.
	events chan struct{}
	// ready closes once tmux has answered the attach with its greeting block.
	ready chan struct{}
	// done closes when the control client exits (detach, kill, or error).
	done     chan struct{}
	exitErr  error
	doneOnce sync.Once

	// reaped closes after the control subprocess's Wait returns. In-memory
	// parser tests have no subprocess and receive an already-closed channel.
	reaped        chan struct{}
	cancelProcess context.CancelFunc
}

type reply struct {
	text string
	err  error
}

// tmux before 3.7 crashes if a control client is notified mid-handshake
// (tmux/tmux#4980). attachGate orders this process, and a lock file beside
// the socket orders every agent-manager process on that server.
const attachGateSlots int64 = 1 << 20

var attachGate = semaphore.NewWeighted(attachGateSlots)

// enterGate lets a tmux command run beside others until release. Exclusive,
// it holds off every agent-manager command on the server instead.
func enterGate(socket string, exclusive bool) (release func(), err error) {
	return enterGateContext(context.Background(), socket, exclusive)
}

func enterGateContext(ctx context.Context, socket string, exclusive bool) (release func(), err error) {
	weight := int64(1)
	if exclusive {
		weight = attachGateSlots
	}
	if err := attachGate.Acquire(ctx, weight); err != nil {
		return nil, err
	}
	unlockServer, err := lockServerContext(ctx, socket, exclusive)
	if err != nil {
		attachGate.Release(weight)
		return nil, err
	}
	return func() {
		unlockServer()
		attachGate.Release(weight)
	}, nil
}

// OpenControl attaches a control-mode client to a live session. Gate
// acquisition and the greeting are bounded by the same default as other tmux
// commands.
func (d *Driver) OpenControl(id string) (*Control, error) {
	ctx, cancel := context.WithTimeout(context.Background(), driverCommandTimeout)
	defer cancel()
	return d.OpenControlContext(ctx, id)
}

// OpenControlContext attaches a control-mode client and returns once tmux has
// greeted it. ctx bounds only the open; a returned Control owns its process
// until Close is called.
func (d *Driver) OpenControlContext(ctx context.Context, id string) (*Control, error) {
	release, err := enterGateContext(ctx, d.socket, true)
	if err != nil {
		return nil, err
	}
	defer release()
	processCtx, cancelProcess := context.WithCancel(context.Background())
	cmd := exec.CommandContext(processCtx, d.bin, d.args("-C", "attach-session", "-t", sessionName(id))...)
	configureBoundedCommand(cmd)
	cmd.WaitDelay = commandTimeout
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancelProcess()
		return nil, fmt.Errorf("control stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancelProcess()
		return nil, fmt.Errorf("control stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		cancelProcess()
		return nil, fmt.Errorf("control attach: %w", err)
	}
	control := newControl(stdin, stdout)
	control.cmd = cmd
	control.socket = d.socket
	control.cancelProcess = cancelProcess
	control.reaped = make(chan struct{})
	go func() {
		<-control.done
		_ = cmd.Wait()
		cancelProcess()
		close(control.reaped)
	}()
	// Keep the exclusive gate through the greeting. If opening is canceled,
	// kill and reap this client before releasing the gate so no cooperating
	// command can notify tmux while the handshake is half-open.
	select {
	case <-control.ready:
		if ctx.Err() != nil {
			control.abort(ctx.Err())
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), commandTimeout)
			reapErr := control.waitReaped(cleanupCtx)
			cleanupCancel()
			return nil, errors.Join(fmt.Errorf("control attach: %w", ctx.Err()), reapErr)
		}
		return control, nil
	case <-control.done:
		// The open context may expire at the same instant stdout closes. Use a
		// fresh cleanup budget so that race cannot release the attach gate
		// before Wait has reaped the already-exited client.
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), commandTimeout)
		reapErr := control.waitReaped(cleanupCtx)
		cleanupCancel()
		if reapErr != nil {
			return nil, fmt.Errorf("control attach exited: %w", reapErr)
		}
		// Preserve the original API: an ungreeted client is a successfully
		// opened, already-closed Control whose Done and Err explain the exit.
		// Callers can distinguish that from an attach/gate failure without
		// losing the process object they historically received.
		return control, nil
	case <-ctx.Done():
		control.abort(ctx.Err())
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), commandTimeout)
		reapErr := control.waitReaped(cleanupCtx)
		cleanupCancel()
		return nil, errors.Join(fmt.Errorf("control attach: %w", ctx.Err()), reapErr)
	}
}

// newControl wires the protocol loop over raw pipes. Split from OpenControl
// so tests can drive the parser without a tmux server.
func newControl(stdin io.WriteCloser, stdout io.Reader) *Control {
	reaped := make(chan struct{})
	close(reaped)
	control := &Control{
		stdin:     stdin,
		writeGate: make(chan struct{}, 1),
		events:    make(chan struct{}, 1),
		ready:     make(chan struct{}),
		done:      make(chan struct{}),
		reaped:    reaped,
	}
	control.writeGate <- struct{}{}
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
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	text, err := c.CommandContext(ctx, command)
	if errors.Is(err, context.DeadlineExceeded) {
		return "", fmt.Errorf("control command timed out: %w", err)
	}
	return text, err
}

// CommandContext bounds the shared attach gate, the write turn, the pipe
// write, and the reply. A real control client holds the shared gate through
// tmux's acknowledgement: its command can notify other control clients, so it
// must not overlap another process's exclusive attach handshake. Parser-only
// controls have no subprocess or server and skip that gate.
func (c *Control) CommandContext(ctx context.Context, command string) (string, error) {
	var release func()
	if c.cmd != nil {
		var err error
		release, err = enterGateContext(ctx, c.socket, false)
		if err != nil {
			return "", fmt.Errorf("control command gate: %w", err)
		}
		defer release()
	}
	waiter, err := c.submitContext(ctx, command)
	if err != nil {
		if c.cmd != nil {
			select {
			case <-c.done:
				return "", errors.Join(err, c.waitReapedForCleanup("control command cleanup"))
			default:
			}
		}
		return "", err
	}
	select {
	case result := <-waiter:
		return result.text, result.err
	case <-c.done:
		exitErr := errors.New("control client exited")
		if c.cmd != nil {
			return "", errors.Join(exitErr, c.waitReapedForCleanup("control command cleanup"))
		}
		return "", exitErr
	case <-ctx.Done():
		if c.cmd == nil {
			// Parser-only tests have no process to terminate. The waiter stays
			// queued so a later reply preserves protocol alignment.
			return "", fmt.Errorf("control command: %w", ctx.Err())
		}
		// Once the write has completed, a timeout cannot establish whether
		// tmux applied the command. End the unusable stream and reap it while
		// still holding the shared gate; callers must treat this as ambiguous.
		c.abort(ctx.Err())
		return "", errors.Join(
			fmt.Errorf("control command: %w", ctx.Err()),
			c.waitReapedForCleanup("control command cleanup"),
		)
	}
}

// Send runs one tmux command and waits for its acknowledgement. Waiting keeps
// the shared attach gate around the complete notification boundary; an error
// after submission is ambiguous and must not trigger an automatic retry.
func (c *Control) Send(command string) error {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	_, err := c.CommandContext(ctx, command)
	return err
}

// submitContext enqueues a reply waiter and writes the command line. The queue
// append rides inside the write gate so waiter order always matches write
// order, while mu itself is never held across the pipe write.
func (c *Control) submitContext(ctx context.Context, command string) (chan reply, error) {
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("control write: %w", ctx.Err())
	case <-c.done:
		return nil, errors.New("control client exited")
	case <-c.writeGate:
	}
	defer func() { c.writeGate <- struct{}{} }()

	waiter := make(chan reply, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, fmt.Errorf("control client closed")
	}
	c.pending = append(c.pending, waiter)
	c.mu.Unlock()
	writeDone := make(chan error, 1)
	go func() {
		_, err := io.WriteString(c.stdin, command+"\n")
		writeDone <- err
	}()
	select {
	case err := <-writeDone:
		if err == nil {
			return waiter, nil
		}
		wrapped := fmt.Errorf("control write: %w", err)
		c.abort(wrapped)
		return nil, wrapped
	case <-ctx.Done():
		c.abort(ctx.Err())
		c.waitWriter(writeDone)
		return nil, fmt.Errorf("control write: %w", ctx.Err())
	case <-c.done:
		_ = c.stdin.Close()
		c.waitWriter(writeDone)
		return nil, errors.New("control client exited")
	}
}

// Close detaches the client. tmux exits on stdin EOF; one that ignores it
// is killed after a bounded wait so the caller can never hang here.
func (c *Control) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	return c.CloseContext(ctx)
}

// CloseContext stops accepting commands, closes the pipe, and waits for the
// control subprocess to be reaped. It never closes or kills past an exclusive
// attach handshake. An error means the caller cannot assume the client was
// reaped and may call CloseContext again with a fresh budget.
func (c *Control) CloseContext(ctx context.Context) error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	if c.cmd == nil {
		_ = c.stdin.Close()
		return nil
	}
	select {
	case <-c.reaped:
		return nil
	default:
	}
	// Leaving notifies every other control client. The client leaves even
	// after the caller's deadline only once a fresh cleanup budget acquires
	// the shared gate. Bypassing an exclusive attach handshake can crash tmux
	// before 3.7, so a cleanup-gate failure leaves this client alive and
	// returns the explicit error for a later close attempt.
	release, gateErr := enterGateContext(ctx, c.socket, false)
	if gateErr != nil {
		if ctx.Err() == nil {
			return gateErr
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), driverCommandTimeout)
		var cleanupErr error
		release, cleanupErr = enterGateContext(cleanupCtx, c.socket, false)
		cleanupCancel()
		if cleanupErr != nil {
			return errors.Join(
				fmt.Errorf("control close: %w", gateErr),
				fmt.Errorf("control close cleanup gate: %w", cleanupErr),
			)
		}
	}
	defer release()
	_ = c.stdin.Close()
	if gateErr != nil {
		c.abort(gateErr)
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), commandTimeout)
		reapErr := c.waitReaped(cleanupCtx)
		cleanupCancel()
		return errors.Join(fmt.Errorf("control close: %w", gateErr), reapErr)
	}
	select {
	case <-c.reaped:
		return nil
	case <-ctx.Done():
		c.abort(ctx.Err())
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), commandTimeout)
		reapErr := c.waitReaped(cleanupCtx)
		cleanupCancel()
		return errors.Join(fmt.Errorf("control close: %w", ctx.Err()), reapErr)
	}
}

func (c *Control) waitReaped(ctx context.Context) error {
	select {
	case <-c.reaped:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Control) waitReapedForCleanup(operation string) error {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	if err := c.waitReaped(ctx); err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	return nil
}

func (c *Control) waitWriter(done <-chan error) {
	timer := time.NewTimer(250 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
	}
}

// abort makes a partially written protocol stream terminal and wakes every
// waiter. Closing stdin unblocks a pipe write; cancelProcess kills the whole
// tmux-client process group when one exists.
func (c *Control) abort(reason error) {
	c.mu.Lock()
	c.closed = true
	if c.exitErr == nil {
		c.exitErr = reason
	}
	c.pending = nil
	c.mu.Unlock()
	_ = c.stdin.Close()
	if c.cancelProcess != nil {
		c.cancelProcess()
	} else if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	c.doneOnce.Do(func() { close(c.done) })
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
		close(c.ready)
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
	if c.exitErr == nil {
		c.exitErr = err
	}
	c.pending = nil
	c.mu.Unlock()
	_ = c.stdin.Close()
	c.doneOnce.Do(func() { close(c.done) })
}
