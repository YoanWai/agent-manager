package geometry

import "testing"

// A drag follows the pointer and marks itself moved, which is what a release
// commits; a release after no motion puts nothing back.
func TestGestureDragAndRelease(t *testing.T) {
	armed := Gesture{Ratio: 0.3, RatioBefore: 0.3}

	dragged := armed.Pressed()
	if !dragged.Dragging || dragged.Moved {
		t.Fatalf("a press left dragging=%t moved=%t", dragged.Dragging, dragged.Moved)
	}
	dragged = dragged.Drag(50, 100)
	if dragged.Ratio != 0.5 || !dragged.Moved {
		t.Fatalf("drag on column 50 of 100 gave ratio=%v moved=%t", dragged.Ratio, dragged.Moved)
	}
	released := dragged.Released(60, 100)
	if released.Dragging || released.Ratio != 0.6 {
		t.Fatalf("release gave dragging=%t ratio=%v", released.Dragging, released.Ratio)
	}
	if end := released.Ended(true); end.ResizeMode || end.Dragging || end.Moved || end.Ratio != 0.6 {
		t.Fatalf("commit left %+v", end)
	}
	if cancel := released.Ended(false); cancel.Ratio != 0.3 {
		t.Fatalf("cancel put back %v, want the ratio it started from", cancel.Ratio)
	}
}

// A press on the seam that never moved keeps the ratio it found.
func TestGestureClickOnTheSeamMovesNothing(t *testing.T) {
	gesture := Gesture{Ratio: 0.3, RatioBefore: 0.3}.Pressed().Released(70, 100)
	if gesture.Ratio != 0.3 {
		t.Fatalf("a click moved the divider to %v", gesture.Ratio)
	}
}

// The resize keys nudge the divider by whole columns, floored so both panels
// stay usable.
func TestGestureNudgeClamps(t *testing.T) {
	nudged := Gesture{Ratio: 0.3}.Nudged(-10, 100)
	if left, _ := Widths(nudged.Ratio, 100); left != MinSplitSide {
		t.Fatalf("nudging past the floor left %d columns", left)
	}
	same := Gesture{Ratio: 0.3}.Nudged(0, 100)
	if same.Ratio != 0.3 {
		t.Fatalf("a zero nudge moved the divider to %v", same.Ratio)
	}
	narrow := Gesture{Ratio: 0.3}.Nudged(1, 0)
	if narrow.Ratio != 0.3 {
		t.Fatalf("a nudge with no width moved the divider to %v", narrow.Ratio)
	}
}
