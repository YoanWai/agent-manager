package ui

import (
	"github.com/charmbracelet/lipgloss"
)

// splitWidths is the body's horizontal split: the sessions panel takes
// splitRatio of the terminal (default 30%), floored so both sides stay
// usable when the window is wide enough.
func (m *Model) splitWidths() (int, int) {
	if m.width <= 0 {
		return 0, 0
	}
	ratio := m.split.ratio
	if ratio <= 0 || ratio >= 1 {
		ratio = defaultSplitRatio
	}
	leftWidth := int(float64(m.width)*ratio + 0.5)
	leftWidth = clampSplitLeft(leftWidth, m.width)
	return leftWidth, m.width - leftWidth
}

// previewPaneWidth is the sidebar's inner content width: the columns the
// pane preview can actually show. Sessions size to it so captured lines
// fit the panel instead of getting clipped on the right.
func (m *Model) previewPaneWidth() int {
	_, rightWidth := m.splitWidths()
	// The seam and bleed columns between the rail and the content are not
	// the pane's. The rest is: captured output spans the column, so a
	// session is sized to the full width its preview paints into.
	w := rightWidth - 2
	if w < 1 {
		return 1
	}
	return w
}

// paneTargetSize is the tmux window size sessions are pinned to: the
// preview panel's box in the split, the whole terminal body in the full
// screen layout, which paints captures across the full width.
func (m *Model) paneTargetSize() (int, int) {
	if m.prefs.fullLayout {
		return max(m.width, 1), m.restingBodyHeight()
	}
	return m.previewPaneWidth(), m.previewPaneHeight()
}

// previewPaneHeight is the rows of session pane content the Preview
// section can show with nothing transient over it, which is what tmux is
// pinned to: the painted view crops a taller pane, where resizing it for
// a passing overlay would cost an agent a full transcript redraw.
func (m *Model) previewPaneHeight() int {
	// Our own blocks wrap inside the column's gutters, so their heights
	// are measured at that width rather than at the preview's full span.
	inner := m.previewPaneWidth() - 2*contentGutter
	if inner < 1 {
		inner = 1
	}
	if m.height < 1 {
		return 1
	}
	avail := m.restingBodyHeight()
	if avail < 1 {
		return 1
	}
	// Mirrors contentLines: the detail head, the seam, then the pane
	// filling everything below.
	rest := avail - lipgloss.Height(m.viewDetail(inner)) - 1
	if rest < 3 {
		// Preview section is hidden; keep a tiny pane for create/attach paths.
		return 3
	}
	return rest
}

// fullFocus reports whether the focused session owns the whole terminal
// body, which is how the full screen layout opens a session.
func (m *Model) fullFocus() bool {
	return m.prefs.fullLayout && m.mode == modeFocus
}

// focusPaneRows is the rows of pane content the focused view paints: the
// whole body when the session is open full screen, the preview panel's
// rows in the split.
func (m *Model) focusPaneRows() int {
	if m.fullFocus() {
		return m.listBodyHeight()
	}
	return m.previewPaneHeight()
}
