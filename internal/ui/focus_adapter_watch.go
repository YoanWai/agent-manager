package ui

import (
	"github.com/YoanWai/agent-manager/internal/tmux"
	focusruntime "github.com/YoanWai/agent-manager/internal/ui/focus/runtime"
	focuswatch "github.com/YoanWai/agent-manager/internal/ui/focus/watch"
	tea "github.com/charmbracelet/bubbletea"
)

// newFocusWatch opens the root's one pane watcher, whose frames and lost
// clients arrive through send as messages.
func newFocusWatch(driver *tmux.Driver, send func(tea.Msg)) *focuswatch.Watcher {
	return focusruntime.NewWatcher(driver, focusruntime.Sink{
		Frame: func(frame focuswatch.Frame) { send(focusruntime.FrameMessage(frame)) },
		Lost:  func(err error) { send(errMsg{err}) },
	})
}
