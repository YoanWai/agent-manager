package ui

import (
	"github.com/YoanWai/agent-manager/internal/tmux"
	focuswatch "github.com/YoanWai/agent-manager/internal/ui/focus/watch"
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

// newFocusWatch opens the root's one pane watcher, whose frames and lost
// clients arrive through send as messages. The root holds it and closes it
// on quit.
func newFocusWatch(driver *tmux.Driver, send func(tea.Msg)) *focuswatch.Watcher {
	return focuswatch.New(driver, previewIntervalLive, focuswatch.Sink{
		Frame: func(frame focuswatch.Frame) { send(focusPreviewFromFrame(frame)) },
		Lost:  func(err error) { send(errMsg{err}) },
	})
}

func focusPreviewFromFrame(frame focuswatch.Frame) focusPreviewMsg {
	return focusPreviewMsg{
		sessID:      frame.SessionID,
		preview:     frame.Preview,
		cursorX:     frame.CursorX,
		cursorY:     frame.CursorY,
		cursorOK:    frame.CursorOK,
		paneStateOK: frame.PaneStateOK,
		paneMouse:   frame.Mouse,
		paneMotion:  frame.Motion,
		paneSGR:     frame.SGR,
		paneAlt:     frame.Alt,
		historySize: frame.History,
		writes:      frame.Writes,
	}
}
