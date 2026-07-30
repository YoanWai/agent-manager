package ui

import (
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
	sessID   string
	preview  string
	cursorX  int
	cursorY  int
	cursorOK bool
}

// focusDebounce is how long the watcher lets a paint burst settle before
// capturing, so a stream of tmux output events becomes a few captures.
const focusDebounce = 25 * time.Millisecond

// focusWatch keeps one tmux control-mode client on the selected session.
// tmux pushes an event the moment the pane paints and the capture rides
// the same pipe, so the focused preview updates event-driven, with no
// polling interval and no process forks.
type focusWatch struct {
	driver *tmux.Driver
	send   func(tea.Msg)

	mu      sync.Mutex
	id      string
	stop    chan struct{}
	control *tmux.Control
	// failedID/failedAt back off reopening a session whose client just
	// died. Selection sync retries every poll pass, and without the pause
	// a dead session's row costs one tmux fork per pass, forever.
	failedID string
	failedAt time.Time
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
	w.stop = stop
	w.mu.Unlock()
	go w.watch(id, stop)
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

// serving reports whether a live control client is pushing previews for
// this session. While it is, its pushes are the only truth about the pane:
// the poll and tick captures run on their own slower cadence and would
// otherwise paint a stale frame over a fresh one.
func (w *focusWatch) serving(id string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.id == id && w.control != nil
}

// command runs one tmux command over the current watcher's control pipe.
// False when no client is up (watcher still opening, or dead); the caller
// falls back to a forked tmux invocation.
func (w *focusWatch) command(command string) bool {
	_, ok := w.query(command)
	return ok
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
}

// clearIfCurrent lets a dead watcher release its claim so a later
// setFocus with the same id opens a fresh client (session restarted,
// server came back).
func (w *focusWatch) clearIfCurrent(id string, stop chan struct{}) {
	w.mu.Lock()
	if w.id == id && w.stop == stop {
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

func (w *focusWatch) watch(id string, stop chan struct{}) {
	control, err := w.driver.OpenControl(id)
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
	target := tmux.SessionName(id)
	capture := func() bool {
		pane, err := control.Command("capture-pane -p -e -t " + target)
		if err != nil {
			return false
		}
		msg := focusPreviewMsg{sessID: id, preview: matchExecShape(pane)}
		// The capture carries no cursor, and a terminal without a visible
		// cursor gives no sense of where typing lands.
		// The format must be quoted: tmux's parser reads a bare { as the
		// start of a command block and swallows the argument, which comes
		// back as its default status message instead of coordinates.
		if where, err := control.Command(
			`display-message -p -t ` + target + ` "#{cursor_x},#{cursor_y}"`); err == nil {
			msg.cursorX, msg.cursorY, msg.cursorOK = parseCursor(where)
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
			return
		case <-control.Events():
		}
		// Let the paint burst settle, then fold everything queued since
		// into this one capture.
		time.Sleep(focusDebounce)
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

// matchExecShape gives the control-pipe capture the same shape the exec
// CapturePane path produces. Only the terminating newline comes off:
// trailing blank rows are pane content, and dropping them shifts every
// line up, so the pushed and polled captures would disagree about where
// the agent's output sits.
func matchExecShape(pane string) string {
	return strings.TrimSuffix(pane, "\n") + "\n"
}

// parseCursor reads "x,y" from display-message.
func parseCursor(reply string) (x, y int, ok bool) {
	parts := strings.Split(strings.TrimSpace(reply), ",")
	if len(parts) != 2 {
		return 0, 0, false
	}
	values := make([]int, 2)
	for i, part := range parts {
		value, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return 0, 0, false
		}
		values[i] = value
	}
	return values[0], values[1], true
}
