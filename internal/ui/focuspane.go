package ui

type focusPaneState struct {
	focus *focusWatch
	// sel is the focused-pane selection, written during paint so clicks
	// resolve against the current frame. copied is the size of the last
	// clipboard write, shown once in the status line and cleared on the
	// next selection. copyGen rises whenever the selection behind a write
	// stops being the one on screen, so a write that lands late is dropped
	// instead of re-arming the count under nothing.
	copied  int
	copyGen int
	sel     focusSelection
	// forwardingMouse holds an Alt-initiated in-pane click lifecycle until
	// its release. The button and last in-pane cell keep an X10 release
	// paired with its press when it reports MouseButtonNone outside the pane.
	forwardingMouse  bool
	forwardingButton int
	forwardingRow    int
	forwardingCol    int
	// pending is a press in a mouse-tracking pane awaiting its verdict:
	// selection drag or forwarded click.
	pending pendingClick
	pane    paneMirror
	// cursorOn is the caret's blink phase while focused.
	cursorOn bool
	// imeCursor is shared with the terminal output writer so the host input
	// method can follow whichever software caret the UI rendered.
	imeCursor *cursorAnchor
	// focusScroll is how many lines the focused pane is scrolled back into
	// its history; zero is live at the bottom.
	focusScroll int
	// focusFetchInFlight guards the scroll-region pipeline: one capture
	// rides the control pipe at a time, and a wheel that moved the target
	// meanwhile is served by the reply's own follow-up fetch. Without it a
	// fast wheel queues a full history capture per notch plus a catch-up
	// per stale reply, and the pipe answers them for half a minute.
	focusFetchInFlight bool
	// watchedGen is previewGen as of the last poll pass, so a selection
	// that has not moved since can be recognised as at rest.
	watchedGen        uint64
	previewBodyOffset int
	// previewGen increments on every cursor move. In-flight captures and
	// settle timers with an older gen are dropped so key-repeat cannot
	// queue a second of tmux work after the user stops.
	previewGen uint64
}
