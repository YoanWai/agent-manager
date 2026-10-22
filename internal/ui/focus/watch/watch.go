// Package watch follows one focused pane over a tmux control client: it
// streams the pane's frames to a Sink, forwards keys over the same client,
// counts the writes it is owed echoes for, and falls back to polling when
// the client is lost.
package watch

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/YoanWai/agent-manager/internal/tmux"
)

// Frame is a pane snapshot pushed by the watcher, with the pane's cursor
// cell so the focused view can show where typing will land. Process stats
// stay on the poller cadence.
type Frame struct {
	SessionID   string
	Preview     string
	CursorX     int
	CursorY     int
	CursorOK    bool
	PaneStateOK bool
	// Mouse and History let the wheel route without asking tmux
	// mid-Update: whether the pane's application owns the mouse, and how
	// far back its history goes. Motion narrows that to the apps that
	// asked for every pointer move, not just clicks and drags, and SGR
	// to those that asked for the modern report encoding.
	Mouse   bool
	Motion  bool
	SGR     bool
	Alt     bool
	History int
	// Writes is how many queued writes had landed when the capture was
	// taken.
	Writes uint64
}

// Sink receives what the watcher pushes: frames, and the loss of a client
// it was serving. Either call may block, the way a Bubble Tea send does.
type Sink struct {
	Frame func(Frame)
	Lost  func(error)
}

// frameBudget spaces captures that change the preview, so a stream
// cannot repaint the outer terminal faster than it keeps up with.
const frameBudget = 80 * time.Millisecond

// captureGap spaces every other capture by one 60Hz display frame.
const captureGap = 16 * time.Millisecond

// pollQuiet is how long a polled watcher leaves a pane that showed
// nothing new before it looks again. Every look forks the multiplexer,
// about 25 ms of CPU on psmux, so a changing pane is looked at on the live
// tick cadence rather than the frame budget, and only typing sooner.
const pollQuiet = 500 * time.Millisecond

// paneStateFormat asks for the cursor cell and whether it shows, mouse
// ownership, history depth and the alternate screen, in the order
// applyPaneState reads them.
const paneStateFormat = "#{cursor_x},#{cursor_y},#{cursor_flag},#{mouse_any_flag}#{mouse_button_flag}#{mouse_standard_flag},#{history_size},#{mouse_all_flag},#{mouse_sgr_flag},#{alternate_on}"

// Watcher keeps one tmux control-mode client on the selected session.
// tmux pushes an event the moment the pane paints and the capture rides
// the same pipe, so the focused preview updates event-driven, with no
// polling interval and no process forks. Where the multiplexer has no
// usable control client the watcher polls forked captures instead.
type Watcher struct {
	driver *tmux.Driver
	sink   Sink
	// liveTick is how soon a polled watcher looks again at a pane that just
	// changed: the poller's live preview cadence.
	liveTick time.Duration

	mu      sync.Mutex
	id      string
	stop    <-chan struct{}
	cancel  context.CancelFunc
	input   chan struct{}
	control *tmux.Control
	closed  bool
	running sync.WaitGroup
	// polled marks a watcher serving id with forked captures, for a
	// multiplexer whose control client is not used.
	polled bool
	// failedID/failedAt back off reopening a session whose client just
	// died. Selection sync retries every poll pass, and without the pause
	// a dead session's row costs one tmux fork per pass, forever.
	failedID string
	failedAt time.Time
	// queued and landed count the writes to a polled pane in and out, so a
	// frame can tell whether it shows them all.
	queued, landed uint64
}

// retryBackoff is how long a failed session sits before the watcher
// tries its control client again.
const (
	retryBackoff       = 15 * time.Second
	controlOpenTimeout = 5 * time.Second
	controlCallTimeout = 2 * time.Second
	closeTimeout       = 3 * time.Second
)

// New returns a watcher on driver's server that pushes to sink. Its owner
// points it with SetFocus and closes it on quit.
func New(driver *tmux.Driver, liveTick time.Duration, sink Sink) *Watcher {
	return &Watcher{driver: driver, sink: sink, liveTick: liveTick}
}

// SetFocus points the watcher at a session, replacing any previous one.
// An empty id stops watching. Repeated calls with the current id are
// no-ops while its watcher is alive, so this is safe to call from every
// selection sync.
func (w *Watcher) SetFocus(id string) {
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
	if id == w.failedID && time.Since(w.failedAt) < retryBackoff {
		// Still in the failure window; leave the id unclaimed so the next
		// selection sync after the pause retries.
		w.id = ""
		w.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	stop := ctx.Done()
	input := make(chan struct{}, 1)
	w.stop, w.cancel, w.input = stop, cancel, input
	w.running.Add(1)
	w.mu.Unlock()
	go func() {
		defer w.running.Done()
		defer cancel()
		w.watch(ctx, id, stop, input)
	}()
}

// InputSent tells the watcher a write just reached the pane, so it looks
// for the echo. A polled watcher hears it from WriteLanded instead.
func (w *Watcher) InputSent(id string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.polled {
		w.landedLocked(id)
	}
}

// landedLocked wakes the watcher on id to look for a write's echo.
func (w *Watcher) landedLocked(id string) {
	if w.id != id {
		return
	}
	select {
	case w.input <- struct{}{}:
	default:
	}
}

// CountWrite counts one write to pane id that the polled watcher on id
// should see land before it trusts a frame's caret, reporting whether it
// was counted; WriteLanded must follow each counted write.
func (w *Watcher) CountWrite(id string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.polled || w.id != id {
		return false
	}
	w.queued++
	return true
}

// WriteLanded marks a counted write as done and wakes the watcher on id to
// look for its echo.
func (w *Watcher) WriteLanded(id string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.landed++
	w.landedLocked(id)
}

// CaughtUp reports whether a frame taken once writes of the counted writes
// had landed shows every write counted so far. One that does not predates
// keys still on their way, and its caret is not where typing lands.
func (w *Watcher) CaughtUp(writes uint64) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.queued == writes
}

// Close stops every watcher and waits a bounded interval for its control
// client to be killed and reaped.
func (w *Watcher) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
	defer cancel()
	_ = w.CloseContext(ctx)
}

func (w *Watcher) CloseContext(ctx context.Context) error {
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

// Watching is the session the watcher is currently pointed at, live client
// or not.
func (w *Watcher) Watching() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.id
}

// Serving reports whether the watcher is pushing previews for this
// session, over a live control client or by polling. While it is, its
// pushes are the only truth about the pane: the poll and tick captures run
// on their own slower cadence and would otherwise paint a stale frame over
// a fresh one.
func (w *Watcher) Serving(id string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.id == id && (w.control != nil || w.polled)
}

// Forward sends one tmux command through the serving control client and
// waits for its acknowledgement. Unavailable is the only result that lets a
// caller safely use another transport: after Command starts, an error cannot
// prove whether tmux applied the command before the acknowledgement was lost.
func (w *Watcher) Forward(command string) (available bool, err error) {
	return w.ForwardBlocks(command, 1)
}

// ForwardBlocks is Forward for a command tmux answers with several reply
// blocks, which the client has to be told to expect.
func (w *Watcher) ForwardBlocks(command string, blocks int) (available bool, err error) {
	w.mu.Lock()
	control := w.control
	w.mu.Unlock()
	if control == nil {
		return false, nil
	}
	return true, control.SendBlocks(command, blocks)
}

// Query runs one tmux command over the control pipe and returns its
// output. Not ok when no client is up or the command failed.
func (w *Watcher) Query(command string) (string, bool) {
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

// History captures the pane of id with lines rows of history above the
// visible ones, in the exec capture's shape: over the serving control
// client, or by a forked capture of the same rows when there is none.
func (w *Watcher) History(id string, lines int) (string, bool) {
	out, ok := w.Query(fmt.Sprintf(`capture-pane -p -e -t %s -S %d -E -`, tmux.PaneTarget(id), -lines))
	if ok {
		return MatchExecShape(out), true
	}
	// No control client: a forked capture of the same rows comes back in
	// the exec shape already.
	ctx, cancel := context.WithTimeout(context.Background(), controlCallTimeout)
	defer cancel()
	out, err := w.driver.CapturePaneHistoryContext(ctx, id, lines)
	return out, err == nil
}

func (w *Watcher) Unwatch(id string) {
	w.mu.Lock()
	if w.id == id {
		w.stopLocked()
		w.id = ""
	}
	w.mu.Unlock()
}

// stopLocked signals the watcher and returns immediately. It must never
// wait: it runs inside Update, and the watcher may at that moment be
// blocked in the sink, which only the Update loop can drain — waiting here
// deadlocks the whole UI. A signalled watcher exits on its own and any
// preview it was mid-sending is dropped by the session guard in Update.
func (w *Watcher) stopLocked() {
	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}
	w.stop = nil
	w.input = nil
	// The stopped watcher's client is not ours to report or use anymore.
	// Left in place until its goroutine unwound, Serving() would claim a
	// session nothing streams yet and freeze its preview on the old frame.
	w.control = nil
	w.polled = false
}

// clearIfCurrent lets a dead watcher release its claim so a later
// SetFocus with the same id opens a fresh client (session restarted,
// server came back).
func (w *Watcher) clearIfCurrent(id string, stop <-chan struct{}) {
	w.mu.Lock()
	if w.id == id && w.stop == stop {
		w.cancel = nil
		w.stop = nil
		w.id = ""
		w.polled = false
		w.failedID = id
		w.failedAt = time.Now()
	}
	w.mu.Unlock()
}

// RetryNow lifts the failure backoff. A deliberate act on the session -
// focusing it, reviving it - is better evidence than any timer that a
// fresh attach is worth one fork.
func (w *Watcher) RetryNow() {
	w.mu.Lock()
	w.failedID = ""
	w.mu.Unlock()
}

func (w *Watcher) watch(ctx context.Context, id string, stop <-chan struct{}, input chan struct{}) {
	openCtx, cancelOpen := context.WithTimeout(ctx, controlOpenTimeout)
	exists := w.driver.ExistsContext(openCtx, id)
	cancelOpen()
	if !exists {
		w.clearIfCurrent(id, stop)
		return
	}
	openCtx, cancelOpen = context.WithTimeout(ctx, controlOpenTimeout)
	control, err := w.driver.OpenControlContext(openCtx, id)
	cancelOpen()
	if errors.Is(err, tmux.ErrControlUnavailable) {
		w.poll(ctx, id, stop, input)
		return
	}
	if err != nil {
		// No control client, no pushed previews; the settle/tick capture
		// path still serves this session.
		w.clearIfCurrent(id, stop)
		return
	}
	closeControl := func() {
		closeCtx, cancelClose := context.WithTimeout(context.Background(), controlCallTimeout)
		_ = control.CloseContext(closeCtx)
		cancelClose()
	}
	defer closeControl()
	// A Bubble Tea send normally unblocks when its program stops, but the sink
	// is an injected synchronous callback and may block forever. Cancellation
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
	capture := func() (string, bool) {
		callCtx, cancelCall := context.WithTimeout(ctx, controlCallTimeout)
		pane, err := control.CommandContext(callCtx, "capture-pane -p -e -t "+target)
		cancelCall()
		if err != nil {
			w.report(stop, fmt.Errorf("preview client for %s: %w", id, err))
			return "", false
		}
		msg := Frame{SessionID: id, Preview: MatchExecShape(pane)}
		// The capture carries no cursor, and a terminal without a visible
		// cursor gives no sense of where typing lands. Mouse ownership and
		// history depth ride along so the wheel can route from cached state
		// instead of a round trip on the UI loop.
		// The format must be quoted: tmux's parser reads a bare { as the
		// start of a command block and swallows the argument, which comes
		// back as its default status message instead of coordinates.
		callCtx, cancelCall = context.WithTimeout(ctx, controlCallTimeout)
		state, stateErr := control.CommandContext(callCtx,
			`display-message -p -t `+target+` "`+paneStateFormat+`"`)
		cancelCall()
		if stateErr == nil {
			applyPaneState(&msg, state)
		}
		if !w.push(stop, msg) {
			return "", false
		}
		return pane, true
	}
	previous, ok := capture()
	if !ok {
		w.clearIfCurrent(id, stop)
		return
	}
	lastCapture := time.Now()
	nextCapture := lastCapture.Add(captureGap)
	var inputUntil time.Time
	timer := time.NewTimer(0)
	defer timer.Stop()
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
		case <-input:
			inputUntil = time.Now().Add(frameBudget)
			continue
		}
		captureAt := nextCapture
		// Echo may arrive after a streaming paint or a cursor-only update.
		if time.Now().Before(inputUntil) {
			captureAt = lastCapture.Add(captureGap)
		}
		for wait := time.Until(captureAt); wait > 0; wait = time.Until(captureAt) {
			timer.Reset(wait)
			select {
			case <-stop:
				return
			case <-input:
				inputUntil = time.Now().Add(frameBudget)
				captureAt = lastCapture.Add(captureGap)
			case <-timer.C:
			}
			timer.Stop()
		}
		for {
			select {
			case <-control.Events():
				continue
			default:
			}
			break
		}
		pane, ok := capture()
		if !ok {
			w.clearIfCurrent(id, stop)
			return
		}
		// A write that leaves the text unchanged, like the cursor move some
		// TUIs send ahead of an echo, must not hold back that echo.
		gap := captureGap
		if pane != previous {
			gap = frameBudget
		}
		previous = pane
		lastCapture = time.Now()
		nextCapture = lastCapture.Add(gap)
	}
}

// poll serves a session whose multiplexer has no usable control client,
// with forked looks standing in for pushed frames. Each counted write that
// lands is looked for a display frame later, and again each frame until
// the pane changes or the frame budget passes; otherwise a changing pane
// is looked at on the live tick cadence and a still one after
// pollQuiet.
func (w *Watcher) poll(ctx context.Context, id string, stop <-chan struct{}, input chan struct{}) {
	w.mu.Lock()
	current := w.stop == stop
	if current {
		w.polled = true
	}
	w.mu.Unlock()
	if !current {
		return
	}
	previous, ok := w.look(ctx, id, stop, "", true)
	if !ok {
		w.clearIfCurrent(id, stop)
		return
	}
	var echoUntil time.Time
	due := time.Now().Add(pollQuiet)
	timer := time.NewTimer(pollQuiet)
	defer timer.Stop()
	for {
		select {
		case <-stop:
			return
		case <-input:
			now := time.Now()
			echoUntil = now.Add(frameBudget)
			// Only ever sooner: writes landing back to back must not keep
			// pushing the look out until the typing stops.
			if soon := now.Add(captureGap); soon.Before(due) {
				due = soon
				timer.Reset(captureGap)
			}
			continue
		case <-timer.C:
		}
		echo := time.Now().Before(echoUntil)
		pane, ok := w.look(ctx, id, stop, previous, echo)
		if !ok {
			w.clearIfCurrent(id, stop)
			return
		}
		wait := pollQuiet
		switch {
		case pane != previous:
			wait = w.liveTick
			echoUntil = time.Time{}
		case echo:
			wait = captureGap
		}
		previous = pane
		due = time.Now().Add(wait)
		timer.Reset(wait)
	}
}

// look captures the pane and pushes it with its cursor and mouse state. A
// pane unchanged since previous costs the capture alone and pushes
// nothing, unless force asks for the state anyway: a write can move the
// caret without changing a character.
func (w *Watcher) look(ctx context.Context, id string, stop <-chan struct{}, previous string, force bool) (string, bool) {
	w.mu.Lock()
	landed := w.landed
	w.mu.Unlock()
	callCtx, cancelCall := context.WithTimeout(ctx, controlCallTimeout)
	pane, err := w.driver.CapturePaneContext(callCtx, id)
	cancelCall()
	if err != nil {
		w.report(stop, fmt.Errorf("preview of %s: %w", id, err))
		return "", false
	}
	if pane == previous && !force {
		return pane, true
	}
	msg := Frame{SessionID: id, Preview: pane, Writes: landed}
	callCtx, cancelCall = context.WithTimeout(ctx, controlCallTimeout)
	state, err := w.driver.DisplayContext(callCtx, id, paneStateFormat)
	cancelCall()
	if err == nil {
		applyPaneState(&msg, state)
	}
	return pane, w.push(stop, msg)
}

// push hands a frame to the UI loop unless the watcher was stopped: the
// send could block on the UI loop for a frame, and its result is stale.
func (w *Watcher) push(stop <-chan struct{}, msg Frame) bool {
	select {
	case <-stop:
		return false
	default:
	}
	w.sink.Frame(msg)
	return true
}

// report surfaces a client the watcher lost, so the preview dropping to
// the poll cadence shows its reason instead of reading as lag. A stopped
// watcher stays quiet: its send could block on the UI loop for a frame,
// and the loss was asked for.
func (w *Watcher) report(stop <-chan struct{}, err error) {
	select {
	case <-stop:
		return
	default:
	}
	w.sink.Lost(err)
}

// MatchExecShape gives the control-pipe capture the same shape the exec
// CapturePane path produces. The control client joins the reply's lines
// with newlines and never appends a terminator, so the terminator is added
// here and nothing is ever trimmed: a trailing newline in the join IS a
// blank pane row, and trimming it drops the very row the caret parks on
// when a tool rests its cursor at the pane's bottom edge.
func MatchExecShape(pane string) string {
	return pane + "\n"
}

// applyPaneState ignores malformed replies because tmux can substitute its
// default status text when it does not receive the format expression.
func applyPaneState(msg *Frame, reply string) {
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
	msg.PaneStateOK = true
	msg.CursorX, msg.CursorY = x, y
	msg.CursorOK = !tmux.ReportsCursorFlag || strings.TrimSpace(parts[2]) == "1"
	msg.Mouse = strings.Contains(parts[3], "1")
	if historySize > 0 {
		msg.History = historySize
	}
	msg.Motion = strings.TrimSpace(parts[5]) == "1"
	msg.SGR = strings.TrimSpace(parts[6]) == "1"
	msg.Alt = strings.TrimSpace(parts[7]) == "1"
}
