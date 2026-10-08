package ui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/YoanWai/agent-manager/internal/tmux"
	tea "github.com/charmbracelet/bubbletea"
)

// focusPreviewMsg carries a pane snapshot pushed by the focus watcher,
// with the pane's cursor cell so the focused view can show where typing
// will land. Process stats stay on the poller cadence.
type focusPreviewMsg struct {
	sessID      string
	preview     string
	cursorX     int
	cursorY     int
	cursorOK    bool
	paneStateOK bool
	// paneMouse and historySize let the wheel route without asking tmux
	// mid-Update: whether the pane's application owns the mouse, and how
	// far back its history goes. paneMotion narrows that to the apps that
	// asked for every pointer move, not just clicks and drags, and paneSGR
	// to those that asked for the modern report encoding.
	paneMouse   bool
	paneMotion  bool
	paneSGR     bool
	paneAlt     bool
	historySize int
}

// focusDebounce is how long the watcher lets a paint burst settle before
// capturing, so a stream of tmux output events becomes a few captures.
// It is also the preview's frame budget: every capture repaints the
// preview in the outer terminal, and a scrolling agent captured at 25ms
// drove forty full-frame repaints a second through it, which is what
// made the terminal fall behind the keyboard. At 80ms a typed key still
// echoes within a frame while a stream costs the terminal a third.
const focusDebounce = 80 * time.Millisecond

// focusWatch keeps one tmux control-mode client on the selected session.
// tmux pushes an event the moment the pane paints and the capture rides
// the same pipe, so the focused preview updates event-driven, with no
// polling interval and no process forks.
type focusWatch struct {
	driver *tmux.Driver
	send   func(tea.Msg)

	mu      sync.Mutex
	id      string
	stop    <-chan struct{}
	cancel  context.CancelFunc
	control *tmux.Control
	closed  bool
	running sync.WaitGroup
	// failedID/failedAt back off reopening a session whose client just
	// died. Selection sync retries every poll pass, and without the pause
	// a dead session's row costs one tmux fork per pass, forever.
	failedID string
	failedAt time.Time
}

// focusRetryBackoff is how long a failed session sits before the watcher
// tries its control client again.
const (
	focusRetryBackoff       = 15 * time.Second
	focusControlOpenTimeout = 5 * time.Second
	focusControlCallTimeout = 2 * time.Second
	focusWatchCloseTimeout  = 3 * time.Second
)

func newFocusWatch(driver *tmux.Driver, send func(tea.Msg)) *focusWatch {
	return &focusWatch{driver: driver, send: send}
}

// setFocus points the watcher at a session, replacing any previous one.
// An empty id stops watching. Repeated calls with the current id are
// no-ops while its watcher is alive, so this is safe to call from every
// selection sync.
func (w *focusWatch) setFocus(id string) {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	if w.id == id {
		w.mu.Unlock()
		return
	}
	w.stopLocked()
	w.id = id
	if id == "" {
		w.mu.Unlock()
		return
	}
	if id == w.failedID && time.Since(w.failedAt) < focusRetryBackoff {
		// Still in the failure window; leave the id unclaimed so the next
		// selection sync after the pause retries.
		w.id = ""
		w.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	stop := ctx.Done()
	w.stop, w.cancel = stop, cancel
	w.running.Add(1)
	w.mu.Unlock()
	go func() {
		defer w.running.Done()
		defer cancel()
		w.watch(ctx, id, stop)
	}()
}

// Close stops every watcher and waits a bounded interval for its control
// client to be killed and reaped.
func (w *focusWatch) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), focusWatchCloseTimeout)
	defer cancel()
	_ = w.CloseContext(ctx)
}

func (w *focusWatch) CloseContext(ctx context.Context) error {
	w.mu.Lock()
	w.closed = true
	w.stopLocked()
	w.id = ""
	w.mu.Unlock()
	done := make(chan struct{})
	go func() {
		w.running.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// watching is the session the watcher is currently pointed at, live client
// or not.
func (w *focusWatch) watching() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.id
}

// serving reports whether a live control client is pushing previews for
// this session. While it is, its pushes are the only truth about the pane:
// the poll and tick captures run on their own slower cadence and would
// otherwise paint a stale frame over a fresh one.
func (w *focusWatch) serving(id string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.id == id && w.control != nil
}

// forward sends one tmux command through the serving control client and
// waits for its acknowledgement. Unavailable is the only result that lets a
// caller safely use another transport: after Command starts, an error cannot
// prove whether tmux applied the command before the acknowledgement was lost.
func (w *focusWatch) forward(command string) (available bool, err error) {
	return w.forwardBlocks(command, 1)
}

// forwardBlocks is forward for a command tmux answers with several reply
// blocks, which the client has to be told to expect.
func (w *focusWatch) forwardBlocks(command string, blocks int) (available bool, err error) {
	w.mu.Lock()
	control := w.control
	w.mu.Unlock()
	if control == nil {
		return false, nil
	}
	return true, control.SendBlocks(command, blocks)
}

// query runs one tmux command over the control pipe and returns its
// output. Not ok when no client is up or the command failed.
func (w *focusWatch) query(command string) (string, bool) {
	w.mu.Lock()
	control := w.control
	w.mu.Unlock()
	if control == nil {
		return "", false
	}
	out, err := control.Command(command)
	if err != nil {
		return "", false
	}
	return out, true
}

func (w *focusWatch) unwatch(id string) {
	w.mu.Lock()
	if w.id == id {
		w.stopLocked()
		w.id = ""
	}
	w.mu.Unlock()
}

// stopLocked signals the watcher and returns immediately. It must never
// wait: it runs inside Update, and the watcher may at that moment be
// blocked in send, which only the Update loop can drain — waiting here
// deadlocks the whole UI. A signalled watcher exits on its own and any
// preview it was mid-sending is dropped by the sessID guard in Update.
func (w *focusWatch) stopLocked() {
	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}
	w.stop = nil
	// The stopped watcher's client is not ours to report or use anymore.
	// Left in place until its goroutine unwound, serving() would claim a
	// session nothing streams yet and freeze its preview on the old frame.
	w.control = nil
}

// clearIfCurrent lets a dead watcher release its claim so a later
// setFocus with the same id opens a fresh client (session restarted,
// server came back).
func (w *focusWatch) clearIfCurrent(id string, stop <-chan struct{}) {
	w.mu.Lock()
	if w.id == id && w.stop == stop {
		w.cancel = nil
		w.stop = nil
		w.id = ""
		w.failedID = id
		w.failedAt = time.Now()
	}
	w.mu.Unlock()
}

// retryNow lifts the failure backoff. A deliberate act on the session -
// focusing it, reviving it - is better evidence than any timer that a
// fresh attach is worth one fork.
func (w *focusWatch) retryNow() {
	w.mu.Lock()
	w.failedID = ""
	w.mu.Unlock()
}

func (w *focusWatch) watch(ctx context.Context, id string, stop <-chan struct{}) {
	openCtx, cancelOpen := context.WithTimeout(ctx, focusControlOpenTimeout)
	exists := w.driver.ExistsContext(openCtx, id)
	cancelOpen()
	if !exists {
		w.clearIfCurrent(id, stop)
		return
	}
	openCtx, cancelOpen = context.WithTimeout(ctx, focusControlOpenTimeout)
	control, err := w.driver.OpenControlContext(openCtx, id)
	cancelOpen()
	if err != nil {
		// No control client, no pushed previews; the settle/tick capture
		// path still serves this session.
		w.clearIfCurrent(id, stop)
		return
	}
	closeControl := func() {
		closeCtx, cancelClose := context.WithTimeout(context.Background(), focusControlCallTimeout)
		_ = control.CloseContext(closeCtx)
		cancelClose()
	}
	defer closeControl()
	// A Bubble Tea send normally unblocks when its program stops, but send is
	// an injected synchronous callback and may block forever. Cancellation
	// must still detach and reap the control client without waiting for this
	// goroutine to reach its defer.
	stopCancelClose := context.AfterFunc(ctx, closeControl)
	defer stopCancelClose()
	w.mu.Lock()
	if w.stop == stop {
		w.control = control
	}
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		if w.control == control {
			w.control = nil
		}
		w.mu.Unlock()
	}()
	target := tmux.PaneTarget(id)
	capture := func() bool {
		callCtx, cancelCall := context.WithTimeout(ctx, focusControlCallTimeout)
		pane, err := control.CommandContext(callCtx, "capture-pane -p -e -t "+target)
		cancelCall()
		if err != nil {
			w.report(stop, fmt.Errorf("preview client for %s: %w", id, err))
			return false
		}
		msg := focusPreviewMsg{sessID: id, preview: matchExecShape(pane)}
		// The capture carries no cursor, and a terminal without a visible
		// cursor gives no sense of where typing lands. Mouse ownership and
		// history depth ride along so the wheel can route from cached state
		// instead of a round trip on the UI loop.
		// The format must be quoted: tmux's parser reads a bare { as the
		// start of a command block and swallows the argument, which comes
		// back as its default status message instead of coordinates.
		callCtx, cancelCall = context.WithTimeout(ctx, focusControlCallTimeout)
		state, stateErr := control.CommandContext(callCtx,
			`display-message -p -t `+target+
				` "#{cursor_x},#{cursor_y},#{cursor_flag},#{mouse_any_flag}#{mouse_button_flag}#{mouse_standard_flag},#{history_size},#{mouse_all_flag},#{mouse_sgr_flag},#{alternate_on}"`)
		cancelCall()
		if stateErr == nil {
			applyPaneState(&msg, state)
		}
		// Skip the send once stopped: it could block on the UI loop for
		// a frame, and its result is already stale.
		select {
		case <-stop:
			return false
		default:
		}
		w.send(msg)
		return true
	}
	if !capture() {
		w.clearIfCurrent(id, stop)
		return
	}
	for {
		select {
		case <-stop:
			return
		case <-control.Done():
			w.clearIfCurrent(id, stop)
			lost := fmt.Errorf("preview client for %s exited", id)
			if err := control.Err(); err != nil {
				lost = fmt.Errorf("%w: %w", lost, err)
			}
			w.report(stop, lost)
			return
		case <-control.Events():
		}
		// Let the paint burst settle, then fold everything queued since
		// into this one capture.
		debounce := time.NewTimer(focusDebounce)
		select {
		case <-stop:
			if !debounce.Stop() {
				<-debounce.C
			}
			return
		case <-debounce.C:
		}
		for {
			select {
			case <-control.Events():
				continue
			default:
			}
			break
		}
		if !capture() {
			w.clearIfCurrent(id, stop)
			return
		}
	}
}

// report surfaces a client the watcher lost, so the preview dropping to
// the poll cadence shows its reason instead of reading as lag. A stopped
// watcher stays quiet: its send could block on the UI loop for a frame,
// and the loss was asked for.
func (w *focusWatch) report(stop <-chan struct{}, err error) {
	select {
	case <-stop:
		return
	default:
	}
	w.send(errMsg{err})
}

// matchExecShape gives the control-pipe capture the same shape the exec
// CapturePane path produces. The control client joins the reply's lines
// with newlines and never appends a terminator, so the terminator is added
// here and nothing is ever trimmed: a trailing newline in the join IS a
// blank pane row, and trimming it drops the very row the caret parks on
// when a tool rests its cursor at the pane's bottom edge.
func matchExecShape(pane string) string {
	return pane + "\n"
}

// applyPaneState ignores malformed replies because tmux can substitute its
// default status text when it does not receive the format expression.
func applyPaneState(msg *focusPreviewMsg, reply string) {
	parts := strings.Split(strings.TrimSpace(reply), ",")
	if len(parts) != 8 {
		return
	}
	x, errX := strconv.Atoi(strings.TrimSpace(parts[0]))
	y, errY := strconv.Atoi(strings.TrimSpace(parts[1]))
	historySize, errHistory := strconv.Atoi(strings.TrimSpace(parts[4]))
	if errX != nil || errY != nil || errHistory != nil {
		return
	}
	msg.paneStateOK = true
	msg.cursorX, msg.cursorY = x, y
	msg.cursorOK = strings.TrimSpace(parts[2]) == "1"
	msg.paneMouse = strings.Contains(parts[3], "1")
	if historySize > 0 {
		msg.historySize = historySize
	}
	msg.paneMotion = strings.TrimSpace(parts[5]) == "1"
	msg.paneSGR = strings.TrimSpace(parts[6]) == "1"
	msg.paneAlt = strings.TrimSpace(parts[7]) == "1"
}
