package ui

import (
	"github.com/YoanWai/agent-manager/internal/tmux"
	focusruntime "github.com/YoanWai/agent-manager/internal/ui/focus/runtime"
	focuswatch "github.com/YoanWai/agent-manager/internal/ui/focus/watch"
	tea "github.com/charmbracelet/bubbletea"
)

// focusPreviewMsg carries a pane snapshot pushed by the focus watcher, with
// the pane's cursor cell so the focused view can show where typing will land.
type focusPreviewMsg = focusruntime.FrameMsg

// focusScrollMsg carries one scrollback viewport for the focused pane.
type focusScrollMsg = focusruntime.ScrollMsg

// newFocusWatch opens the root's one pane watcher, whose frames and lost
// clients arrive through send as messages.
func newFocusWatch(driver *tmux.Driver, send func(tea.Msg)) *focuswatch.Watcher {
	return focusruntime.NewWatcher(driver, focusruntime.Sink{
		Frame: func(frame focuswatch.Frame) { send(focusPreviewFromFrame(frame)) },
		Lost:  func(err error) { send(errMsg{err}) },
	})
}

func focusPreviewFromFrame(frame focuswatch.Frame) focusPreviewMsg {
	return focusruntime.FrameMessage(frame)
}
