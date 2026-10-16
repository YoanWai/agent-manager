package ui

import (
	"errors"
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
	// writes is how many queued writes had landed when the capture was
	// taken.
	writes uint64
}

// focusFrameBudget spaces captures that change the preview, so a stream
// cannot repaint the outer terminal faster than it keeps up with.
const focusFrameBudget = 80 * time.Millisecond

// focusCaptureGap spaces every other capture by one 60Hz display frame.
const focusCaptureGap = 16 * time.Millisecond

// focusPollQuiet is how long a polled watcher leaves a pane that showed
// nothing new before it looks again. Every look forks the multiplexer,
// about 25 ms of CPU on psmux, so a changing pane is looked at on the live
// tick cadence rather than the frame budget, and only typing sooner.
const focusPollQuiet = 500 * time.Millisecond

// paneStateFormat asks for the cursor cell and whether it shows, mouse
// ownership, history depth and the alternate screen, in the order
// applyPaneState reads them.
const paneStateFormat = "#{cursor_x},#{cursor_y},#{cursor_flag},#{mouse_any_flag}#{mouse_button_flag}#{mouse_standard_flag},#{history_size},#{mouse_all_flag},#{mouse_sgr_flag},#{alternate_on}"

// focusWatch keeps one tmux control-mode client on the selected session.
// tmux pushes an event the moment the pane paints and the capture rides
// the same pipe, so the focused preview updates event-driven, with no
// polling interval and no process forks. Where the multiplexer has no
// usable control client the watcher polls forked captures instead, and
// forked writes to the pane go through its queue.
type focusWatch struct {
	driver *tmux.Driver
	send   func(tea.Msg)

	mu      sync.Mutex
	id      string
	stop    chan struct{}
	input   chan struct{}
	control *tmux.Control
	// polled marks a watcher serving id with forked captures, for a
	// multiplexer whose control client is not used.
	polled bool
	// failedID/failedAt back off reopening a session whose client just
	// died. Selection sync retries every poll pass, and without the pause
	// a dead session's row costs one tmux fork per pass, forever.
	failedID string
	failedAt time.Time
	// writes are forked writes to a pane, run one at a time in the order
	// they were queued; writing is whether a goroutine is draining them.
	// queued and landed count them in and out.
	writes         []focusWrite
	writing        bool
	queued, landed uint64
}

// focusWrite is one queued write to session id's pane. Its message, an
// error or what the write read, goes to the UI loop.
type focusWrite struct {
	id  string
	run tea.Cmd
}

// focusRetryBackoff is how long a failed session sits before the watcher
// tries its control client again.
const focusRetryBackoff = 15 * time.Second

func newFocusWatch(driver *tmux.Driver, send func(tea.Msg)) *focusWatch {
	return &focusWatch{driver: driver, send: send}
}

// setFocus points the watcher at a session, replacing any previous one.
// An empty id stops watching. Repeated calls with the current id are
// no-ops while its watcher is alive, so this is safe to call from every
// selection sync.
func (w *focusWatch) setFocus(id string) {
	w.mu.Lock()
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
	stop := make(chan struct{})
	input := make(chan struct{}, 1)
	w.stop = stop
	w.input = input
	w.mu.Unlock()
	go w.watch(id, stop, input)
}

// inputSent tells the watcher a write just reached the pane, so it looks
// for the echo. A polled watcher hears it from its queue once each write
// has landed instead: told at send time, it would only take a capture
// before the write arrives.
func (w *focusWatch) inputSent(id string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.polled {
		w.landedLocked(id)
	}
}

// landedLocked wakes the watcher on id to look for a write's echo.
func (w *focusWatch) landedLocked(id string) {
	if w.id != id {
		return
	}
	select {
	case w.input <- struct{}{}:
	default:
	}
}

// queue runs a forked write to pane id off the update loop, after every
// write queued before it, and wakes the watcher once it has landed. It
// takes the write while the watcher polls id, or while earlier writes are
// still draining so that a write run directly cannot overtake them, and
// refuses it otherwise, leaving the caller to write directly.
func (w *focusWatch) queue(id string, write tea.Cmd) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.writing && !(w.polled && w.id == id) {
		return false
	}
	w.writes = append(w.writes, focusWrite{id: id, run: write})
	w.queued++
	if !w.writing {
		w.writing = true
		go w.drainWrites()
	}
	return true
}

func (w *focusWatch) drainWrites() {
	w.mu.Lock()
	for len(w.writes) > 0 {
		write := w.writes[0]
		w.writes[0] = focusWrite{}
		w.writes = w.writes[1:]
		w.mu.Unlock()
		if msg := write.run(); msg != nil {
			w.send(msg)
		}
		w.mu.Lock()
		w.landed++
		w.landedLocked(write.id)
	}
	w.writing = false
	w.mu.Unlock()
}

// caughtUp reports whether a frame taken once writes of the queued writes
// had landed shows every write queued so far. One that does not predates
// keys still on their way, and its caret is not where typing lands.
func (w *focusWatch) caughtUp(writes uint64) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.queued == writes
}

// Close stops the current watcher; its control client detaches on its
// own once any in-flight send drains.
func (w *focusWatch) Close() {
	w.mu.Lock()
	w.stopLocked()
	w.id = ""
	w.mu.Unlock()
}

// watching is the session the watcher is currently pointed at, live client
// or not.
func (w *focusWatch) watching() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.id
}

// serving reports whether the watcher is pushing previews for this
// session, over a live control client or by polling. While it is, its
// pushes are the only truth about the pane: the poll and tick captures run
// on their own slower cadence and would otherwise paint a stale frame over
// a fresh one.
func (w *focusWatch) serving(id string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.id == id && (w.control != nil || w.polled)
}

// attempt forwards one tmux command over the control pipe without waiting
// for the reply. True once a client accepted the write: the pane owns the
// command from that moment, even if the acknowledgement is lost, so a
// caller that resent on a slow reply would type the keystroke twice.
// False only when nothing went out - no client, or a failed write - which
// is the one case a forked fallback is safe.
func (w *focusWatch) attempt(command string) bool {
	return w.attemptBlocks(command, 1)
}

// attemptBlocks is attempt for a command tmux answers with several reply
// blocks, which the client has to be told to expect.
func (w *focusWatch) attemptBlocks(command string, blocks int) bool {
	w.mu.Lock()
	control := w.control
	w.mu.Unlock()
	if control == nil {
		return false
	}
	return control.SendBlocks(command, blocks) == nil
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
	if w.stop != nil {
		close(w.stop)
		w.stop = nil
	}
	w.input = nil
	// The stopped watcher's client is not ours to report or use anymore.
	// Left in place until its goroutine unwound, serving() would claim a
	// session nothing streams yet and freeze its preview on the old frame.
	w.control = nil
	w.polled = false
}

// clearIfCurrent lets a dead watcher release its claim so a later
// setFocus with the same id opens a fresh client (session restarted,
// server came back).
func (w *focusWatch) clearIfCurrent(id string, stop chan struct{}) {
	w.mu.Lock()
	if w.id == id && w.stop == stop {
		w.stop = nil
		w.id = ""
		w.polled = false
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

func (w *focusWatch) watch(id string, stop, input chan struct{}) {
	if !w.driver.Exists(id) {
		w.clearIfCurrent(id, stop)
		return
	}
	control, err := w.driver.OpenControl(id)
	if errors.Is(err, tmux.ErrControlUnavailable) {
		w.poll(id, stop, input)
		return
	}
	if err != nil {
		// No control client, no pushed previews; the settle/tick capture
		// path still serves this session.
		w.clearIfCurrent(id, stop)
		return
	}
	defer control.Close()
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
		pane, err := control.Command("capture-pane -p -e -t " + target)
		if err != nil {
			w.report(stop, fmt.Errorf("preview client for %s: %w", id, err))
			return "", false
		}
		msg := focusPreviewMsg{sessID: id, preview: matchExecShape(pane)}
		// The capture carries no cursor, and a terminal without a visible
		// cursor gives no sense of where typing lands. Mouse ownership and
		// history depth ride along so the wheel can route from cached state
		// instead of a round trip on the UI loop.
		// The format must be quoted: tmux's parser reads a bare { as the
		// start of a command block and swallows the argument, which comes
		// back as its default status message instead of coordinates.
		if state, err := control.Command(`display-message -p -t ` + target + ` "` + paneStateFormat + `"`); err == nil {
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
	nextCapture := lastCapture.Add(focusCaptureGap)
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
			inputUntil = time.Now().Add(focusFrameBudget)
			continue
		}
		captureAt := nextCapture
		// Echo may arrive after a streaming paint or a cursor-only update.
		if time.Now().Before(inputUntil) {
			captureAt = lastCapture.Add(focusCaptureGap)
		}
		for wait := time.Until(captureAt); wait > 0; wait = time.Until(captureAt) {
			timer.Reset(wait)
			select {
			case <-stop:
				return
			case <-input:
				inputUntil = time.Now().Add(focusFrameBudget)
				captureAt = lastCapture.Add(focusCaptureGap)
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
		gap := focusCaptureGap
		if pane != previous {
			gap = focusFrameBudget
		}
		previous = pane
		lastCapture = time.Now()
		nextCapture = lastCapture.Add(gap)
	}
}

// poll serves a session whose multiplexer has no usable control client,
// with forked looks standing in for pushed frames. Each queued write that
// lands is looked for a display frame later, and again each frame until
// the pane changes or the frame budget passes; otherwise a changing pane
// is looked at on the live tick cadence and a still one after
// focusPollQuiet.
func (w *focusWatch) poll(id string, stop, input chan struct{}) {
	w.mu.Lock()
	current := w.stop == stop
	if current {
		w.polled = true
	}
	w.mu.Unlock()
	if !current {
		return
	}
	previous, ok := w.look(id, stop, "", true)
	if !ok {
		w.clearIfCurrent(id, stop)
		return
	}
	var echoUntil time.Time
	due := time.Now().Add(focusPollQuiet)
	timer := time.NewTimer(focusPollQuiet)
	defer timer.Stop()
	for {
		select {
		case <-stop:
			return
		case <-input:
			now := time.Now()
			echoUntil = now.Add(focusFrameBudget)
			// Only ever sooner: writes landing back to back must not keep
			// pushing the look out until the typing stops.
			if soon := now.Add(focusCaptureGap); soon.Before(due) {
				due = soon
				timer.Reset(focusCaptureGap)
			}
			continue
		case <-timer.C:
		}
		echo := time.Now().Before(echoUntil)
		pane, ok := w.look(id, stop, previous, echo)
		if !ok {
			w.clearIfCurrent(id, stop)
			return
		}
		wait := focusPollQuiet
		switch {
		case pane != previous:
			wait = previewIntervalLive
			echoUntil = time.Time{}
		case echo:
			wait = focusCaptureGap
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
func (w *focusWatch) look(id string, stop chan struct{}, previous string, force bool) (string, bool) {
	w.mu.Lock()
	landed := w.landed
	w.mu.Unlock()
	pane, err := w.driver.CapturePane(id)
	if err != nil {
		w.report(stop, fmt.Errorf("preview of %s: %w", id, err))
		return "", false
	}
	if pane == previous && !force {
		return pane, true
	}
	msg := focusPreviewMsg{sessID: id, preview: pane, writes: landed}
	if state, err := w.driver.Display(id, paneStateFormat); err == nil {
		applyPaneState(&msg, state)
	}
	return pane, w.push(stop, msg)
}

// push hands a frame to the UI loop unless the watcher was stopped: the
// send could block on the UI loop for a frame, and its result is stale.
func (w *focusWatch) push(stop chan struct{}, msg focusPreviewMsg) bool {
	select {
	case <-stop:
		return false
	default:
	}
	w.send(msg)
	return true
}

// report surfaces a client the watcher lost, so the preview dropping to
// the poll cadence shows its reason instead of reading as lag. A stopped
// watcher stays quiet: its send could block on the UI loop for a frame,
// and the loss was asked for.
func (w *focusWatch) report(stop chan struct{}, err error) {
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
	msg.cursorOK = !tmux.ReportsCursorFlag || strings.TrimSpace(parts[2]) == "1"
	msg.paneMouse = strings.Contains(parts[3], "1")
	if historySize > 0 {
		msg.historySize = historySize
	}
	msg.paneMotion = strings.TrimSpace(parts[5]) == "1"
	msg.paneSGR = strings.TrimSpace(parts[6]) == "1"
	msg.paneAlt = strings.TrimSpace(parts[7]) == "1"
}
