package geometry

// Gutter is the blank column a preview reserves on each side of its text.
const Gutter = 2

// Layout is the terminal box and what its chrome costs, as the body's height,
// the preview's box and a pane's pin read them.
type Layout struct {
	Width, Height int
	// FullLayout paints the list full screen instead of the split.
	FullLayout bool
	// FullFocus is a session owning the whole body, which takes the split's
	// preview out of the picture.
	FullFocus bool
	// HeaderRows is the band above the body.
	HeaderRows int
	// FooterRows is the painted footer's height; QuickFooterRows is the
	// one the quick bar paints, read only while QuickBar says a bar is up.
	FooterRows, QuickFooterRows int
	QuickBar                    bool
	// Detail is the height of the detail head painted over a column inner
	// cells wide, which the preview gives way to.
	Detail func(inner int) int
}

// ChromeRows is the rows above the body: the header band and the rule that
// closes it, plus the line a full screen session names itself on.
func (l Layout) ChromeRows() int {
	if l.FullFocus {
		return l.HeaderRows + 3
	}
	return l.HeaderRows + 1
}

// BodyHeight is the vertical budget for the sessions and sidebar panels.
func (l Layout) BodyHeight() int {
	return l.bodyRows(l.FooterRows)
}

// RestingBodyHeight is the same budget with the footer the screen actually
// paints: a growing footer crops previews instead of resizing running panes.
func (l Layout) RestingBodyHeight() int {
	if !l.QuickBar {
		return l.BodyHeight()
	}
	return l.bodyRows(l.QuickFooterRows)
}

func (l Layout) bodyRows(footer int) int {
	return max(l.Height-l.ChromeRows()-1-footer, 3)
}

// SplitWidths is the body's horizontal split: the sessions panel takes the
// ratio of the width, floored so both sides stay usable.
func (l Layout) SplitWidths(ratio float64) (int, int) {
	return Widths(ratio, l.Width)
}

// PreviewWidth is the sidebar's inner content width: the columns the pane
// preview can actually show.
func (l Layout) PreviewWidth(ratio float64) int {
	_, right := l.SplitWidths(ratio)
	return max(right-2, 1)
}

// PreviewHeight is the rows of session pane content the preview shows with
// nothing transient over it, which is what tmux is pinned to.
func (l Layout) PreviewHeight(ratio float64) int {
	inner := max(l.PreviewWidth(ratio)-2*Gutter, 1)
	if l.Height < 1 {
		return 1
	}
	avail := max(l.RestingBodyHeight(), 1)
	rest := avail - l.detailRows(inner) - 1
	if rest < 3 {
		// The preview section is hidden; keep a tiny pane for the create and
		// attach paths.
		return 3
	}
	return rest
}

// PaneSize is the tmux window size sessions are pinned to: the preview panel's
// box in the split, the whole terminal body in the full screen layout, which
// paints captures across the full width.
func (l Layout) PaneSize(ratio float64) (int, int) {
	if l.FullLayout {
		return max(l.Width, 1), l.RestingBodyHeight()
	}
	return l.PreviewWidth(ratio), l.PreviewHeight(ratio)
}

// BodyRange is the inclusive-start, exclusive-end row range of the main
// sessions and sidebar body.
func (l Layout) BodyRange() (start, end int) {
	start = l.ChromeRows()
	return start, start + l.BodyHeight()
}

func (l Layout) detailRows(inner int) int {
	if l.Detail == nil {
		return 0
	}
	return l.Detail(inner)
}
