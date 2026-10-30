package geometry

// Drag is the gesture a motion under a press makes: it moves the divider to
// the column and marks the drag as moved, which is what a release commits.
func (g Gesture) Drag(x, width int) Gesture {
	if width <= 0 {
		return g
	}
	g.Moved = true
	g.Ratio = RatioAt(x, width)
	return g
}

// Released ends a drag on column x. A drag that never moved keeps the ratio
// it started with, so a plain click on the seam moves nothing.
func (g Gesture) Released(x, width int) Gesture {
	if g.Moved && width > 0 {
		g.Ratio = RatioAt(x, width)
	}
	g.Dragging = false
	return g
}

func (g Gesture) Ended(commit bool) Gesture {
	if !commit {
		g.Ratio = g.RatioBefore
	}
	g.ResizeMode, g.Dragging, g.Moved = false, false, false
	return g
}

// Pressed starts a drag on the divider without the keyboard. A gesture the
// resize key already armed keeps the ratio it started from.
func (g Gesture) Pressed() Gesture {
	if !g.ResizeMode {
		g.RatioBefore = g.Ratio
	}
	g.Dragging, g.Moved = true, false
	return g
}

// Nudged moves the divider by delta columns while the resize keys are on.
func (g Gesture) Nudged(delta, width int) Gesture {
	if width <= 0 || delta == 0 {
		return g
	}
	left, _ := Widths(g.Ratio, width)
	g.Ratio = RatioAt(left+delta, width)
	return g
}

// Divider is the column of the sessions/sidebar junction: the first column of
// the right panel.
func (g Gesture) Divider(width int) int {
	left, _ := Widths(g.Ratio, width)
	return left
}

// FocusPaneRows is the rows of pane content a focused view paints: the whole
// body when the session is open full screen, the preview panel's rows in the
// split.
func FocusPaneRows(l Layout, ratio float64) int {
	if l.FullLayout {
		return l.BodyHeight()
	}
	return l.PreviewHeight(ratio)
}
