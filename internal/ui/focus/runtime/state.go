package runtime

import (
	"github.com/YoanWai/agent-manager/internal/ui/focus"
	"github.com/YoanWai/agent-manager/internal/ui/focus/watch"
)

// State is what one run keeps for the focused pane: the watcher the manager
// opened on its tmux server, which outlives the surface that paints it.
type State struct {
	// Watch follows the selected session's pane, nil until the poller starts
	// one.
	Watch *watch.Watcher
}

// Watching is the session the watcher serves, empty when it serves none.
func (s *State) Watching() string {
	if s.Watch == nil {
		return ""
	}
	return s.Watch.Watching()
}

// Ready reports whether a watcher exists to route a guarded report through.
func (s *State) Ready() bool { return s.Watch != nil }

// SetFocus points the watcher at a session.
func (s *State) SetFocus(id string) {
	if s.Watch != nil {
		s.Watch.SetFocus(id)
	}
}

// ClearFocus stops the watcher's client, leaving the next selection to open
// one.
func (s *State) ClearFocus() {
	if s.Watch != nil {
		s.Watch.SetFocus("")
	}
}

// Settle stops the client when it serves a session other than id: opening a
// control client costs a process and a tmux attach, so a held j through twenty
// rows would pay that twenty times, and the client opens once the cursor comes
// to rest.
func (s *State) Settle(id string) {
	if s.Watch != nil && s.Watch.Watching() != id {
		s.Watch.SetFocus("")
	}
}

// Retry asks the watcher to reopen a client it backed off from.
func (s *State) Retry() {
	if s.Watch != nil {
		s.Watch.RetryNow()
	}
}

// Unwatch stops the watcher before a session is killed on purpose: the client
// going away then is the plan, not a loss to report.
func (s *State) Unwatch(id string) {
	if s.Watch != nil {
		s.Watch.Unwatch(id)
	}
}

// Serving reports whether the watcher pushes frames for id.
func (s *State) Serving(id string) bool {
	return s.Watch != nil && s.Watch.Serving(id)
}

// CountWrite marks one write to a polled pane, reporting whether the watcher is
// counting and so owes an echo.
func (s *State) CountWrite(id string) bool {
	return s.Watch != nil && s.Watch.CountWrite(id)
}

// CaughtUp reports whether a frame showing writes had landed after everything
// queued to the pane.
func (s *State) CaughtUp(writes uint64) bool {
	return s.Watch == nil || s.Watch.CaughtUp(writes)
}

// KeepsPaneFacts reports whether entering a session keeps the pane facts
// already on screen: the watcher serves it and the pane shows it.
func (s *State) KeepsPaneFacts(pane *focus.Model, sessionID string) bool {
	return s.Serving(sessionID) && pane.Pane().SessionID == sessionID
}
