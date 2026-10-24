package ui

import (
	"strings"

	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	uipreview "github.com/YoanWai/agent-manager/internal/ui/preview"
)

// The captured-pane row math lives in ui/preview; the root keeps the names
// the rest of the package already calls, so a pane row is measured one way.

// The IME marker is the root's, so the root strips it before the row is
// measured.
func previewLine(line string, width int) string {
	return uipreview.Line(strings.ReplaceAll(line, uiframe.CursorMarker, ""), width)
}

func expandPaneTabs(line string, width int) string { return uipreview.ExpandTabs(line, width) }

func paneWindow(pane string, n, caretRow int) (lines []string, start int) {
	return uipreview.Window(pane, n, caretRow)
}

func paneExact(pane string, n, width, caretRow int) []string {
	return uipreview.Exact(pane, n, width, caretRow)
}
