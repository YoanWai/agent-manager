package ui

import (
	"github.com/charmbracelet/lipgloss"
)

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
		return max(m.layout.width, 1), m.restingBodyHeight()
	}
	return m.previewPaneWidth(), m.previewPaneHeight()
}

// listChromeRows is the number of rows above the sessions/content body:
// the full-width header band and the rule that closes it. Shared by View
// and bodyYRange so hit-testing cannot drift from paint.
func (m *Model) listChromeRows() int {
	// A session open full screen names itself on a line of its own, held
	// off the band above it and the pane below it by a rule each.
	if m.fullFocus() {
		return m.headerRows() + 3
	}
	return m.headerRows() + 1
}

// listBodyHeight is the vertical budget for the sessions/sidebar panels.
// Matches View: height - (header, seam, footer). Notices float over the body
// instead of taking a row, so the budget is the same with one up.
func (m *Model) listBodyHeight() int {
	bodyHeight := m.layout.height - m.listChromeRows() - 1 - lipgloss.Height(m.viewFooter())
	if bodyHeight < 3 {
		bodyHeight = 3
	}
	return bodyHeight
}

// A growing footer crops previews instead of resizing running panes.
func (m *Model) restingBodyHeight() int {
	if m.quick() == nil {
		return m.listBodyHeight()
	}
	return max(m.layout.height-m.listChromeRows()-1-lipgloss.Height(m.listFooter()), 3)
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
	if m.layout.height < 1 {
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
	return m.prefs.fullLayout && m.focused()
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

// bodyYRange is the inclusive-start exclusive-end row range of the main
// sessions/sidebar body, matching the layout in View.
func (m *Model) bodyYRange() (start, end int) {
	return m.listChromeRows(), m.listChromeRows() + m.listBodyHeight()
}
