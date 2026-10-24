package geometry

import "testing"

func testLayout() Layout {
	return Layout{
		Width: 100, Height: 40,
		HeaderRows: 2,
		FooterRows: 1,
		Detail:     func(int) int { return 3 },
	}
}

// The body's height is the terminal minus the chrome the frame paints.
func TestLayoutBodyHeight(t *testing.T) {
	l := testLayout()
	if got := l.ChromeRows(); got != 3 {
		t.Fatalf("ChromeRows = %d, want the header band and its rule", got)
	}
	if got := l.BodyHeight(); got != 40-3-1-1 {
		t.Fatalf("BodyHeight = %d", got)
	}
	if start, end := l.BodyRange(); start != 3 || end != 3+l.BodyHeight() {
		t.Fatalf("BodyRange = %d..%d", start, end)
	}
}

// A full screen session names itself on a line of its own, held off by a rule
// each, so the body starts lower.
func TestLayoutFullFocusTakesAnExtraLine(t *testing.T) {
	l := testLayout()
	l.FullFocus = true
	if got := l.ChromeRows(); got != 5 {
		t.Fatalf("ChromeRows with a full screen session = %d", got)
	}
}

// The quick bar paints a taller footer, so the body gives way rather than the
// preview resizing every running pane.
func TestLayoutQuickBarShortensTheRestingBody(t *testing.T) {
	l := testLayout()
	plain := l.RestingBodyHeight()
	l.QuickBar = true
	l.QuickFooterRows = 3
	if got := l.RestingBodyHeight(); got >= plain {
		t.Fatalf("the quick bar's footer did not shorten the body: %d >= %d", got, plain)
	}
}

// The preview keeps at least three rows under the detail head, and the pane is
// pinned to the preview's box.
func TestLayoutPaneSizeFollowsThePreview(t *testing.T) {
	l := testLayout()
	width, height := l.PaneSize(DefaultSplitRatio)
	if width != l.PreviewWidth(DefaultSplitRatio) || height != l.PreviewHeight(DefaultSplitRatio) {
		t.Fatalf("PaneSize = %d,%d", width, height)
	}
	tiny := Layout{Width: 20, Height: 4, Detail: func(int) int { return 100 }}
	if got := tiny.PreviewHeight(DefaultSplitRatio); got != 3 {
		t.Fatalf("a hidden preview section gave %d rows, want the floor of 3", got)
	}
	full := testLayout()
	full.FullLayout = true
	if w, h := full.PaneSize(DefaultSplitRatio); w != 100 || h != full.RestingBodyHeight() {
		t.Fatalf("full layout pane = %d,%d, want the whole body", w, h)
	}
}
