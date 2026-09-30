package ui

// focusRuntimeState owns resources and caches that require concrete runtime
// dependencies. Interaction policy and displayed geometry live in focus.Model.
type focusRuntimeState struct {
	watch *focusWatch
	// imeCursor is shared with the terminal output writer.
	imeCursor *cursorAnchor
	// geom is the last width x height told to tmux per session id.
	geom map[string][2]int
	// pids distinguishes a replacement pane from the process previously sized.
	pids map[string]int
	// published is the last box written for paneless launch paths.
	published [2]int
}
