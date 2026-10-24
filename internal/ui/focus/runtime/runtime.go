// Package runtime is the Focus runtime behind the focused surface: the pane
// watcher the manager opens on its tmux server, the sizes it pins each
// session's window to so a capture fills the preview box, the writes it
// forwards to the agent's pane, and the reads behind entering and leaving a
// session. Package focus paints what this State holds; the root routes the
// watcher's frames and applies what the runtime answers.
package runtime

import (
	"time"

	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/YoanWai/agent-manager/internal/ui/focus"
	"github.com/YoanWai/agent-manager/internal/ui/focus/watch"
)

// liveTick is how soon a polled watcher looks again at a pane that just
// changed: the poller's live preview cadence.
const liveTick = 300 * time.Millisecond

// Sink receives what the watcher pushes: pane frames, and the loss of a
// client it was serving.
type Sink struct {
	Frame func(watch.Frame)
	Lost  func(error)
}

// NewWatcher opens a pane watcher on driver that pushes to sink.
func NewWatcher(driver *tmux.Driver, sink Sink) *watch.Watcher {
	return watch.New(driver, liveTick, watch.Sink{Frame: sink.Frame, Lost: sink.Lost})
}

// FrameMsg carries a pane snapshot pushed by the watcher, with the pane's
// cursor cell so the focused view can show where typing will land. Process
// stats stay on the poller cadence.
type FrameMsg struct {
	SessionID   string
	Preview     string
	CursorX     int
	CursorY     int
	CursorOK    bool
	PaneStateOK bool
	// Mouse and History let the wheel route without asking tmux mid-Update:
	// whether the pane's application owns the mouse, and how far back its
	// history goes. Motion narrows that to the apps that asked for every
	// pointer move, not just clicks and drags, and SGR to those that asked
	// for the modern report encoding.
	Mouse   bool
	Motion  bool
	SGR     bool
	Alt     bool
	History int
	// Writes is how many queued writes had landed when the capture was
	// taken.
	Writes uint64
}

// ScrollMsg carries one scrollback viewport for the focused pane: the rows it
// was read for, the viewport itself, and whether the read succeeded.
type ScrollMsg struct {
	SessionID string
	Offset    int
	Rows      int
	Preview   string
	OK        bool
}

// Scroll reads one viewport off the event loop, from the watcher's history
// when it serves the pane.
func Scroll(watch *watch.Watcher, sessionID string, offset, rows int) ScrollMsg {
	msg := ScrollMsg{SessionID: sessionID, Offset: offset, Rows: rows}
	if watch == nil {
		return msg
	}
	out, ok := watch.History(sessionID, offset+rows)
	msg.Preview, msg.OK = focus.BottomWindow(out, rows, offset), ok
	return msg
}

// FrameMessage is a watcher frame as the root routes it.
func FrameMessage(frame watch.Frame) FrameMsg {
	return FrameMsg{
		SessionID:   frame.SessionID,
		Preview:     frame.Preview,
		CursorX:     frame.CursorX,
		CursorY:     frame.CursorY,
		CursorOK:    frame.CursorOK,
		PaneStateOK: frame.PaneStateOK,
		Mouse:       frame.Mouse,
		Motion:      frame.Motion,
		SGR:         frame.SGR,
		Alt:         frame.Alt,
		History:     frame.History,
		Writes:      frame.Writes,
	}
}
