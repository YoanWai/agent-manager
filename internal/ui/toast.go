package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

const (
	// toastMaxWidth is the widest a notice card gets before it wraps, and
	// toastMargin is the gap it keeps from the frame's right edge.
	toastMaxWidth = 52
	toastMinWidth = 20
	toastMargin   = 2
	toastChromeX  = 4
)

// statusToast is the transient message rendered as a card that floats over
// the frame's top right, so a notice never costs the body a row.
func (m *Model) statusToast() []string {
	text := m.statusLine()
	if text == "" {
		return nil
	}
	// The card lives in the content column: it shrinks to its text, and never
	// reaches back over the seam into the sessions rail.
	leftWidth, _ := m.splitWidths()
	width := ansi.StringWidth(text) + toastChromeX
	for _, limit := range []int{toastMaxWidth, m.width - leftWidth - 2 - toastMargin} {
		if width > limit {
			width = limit
		}
	}
	if width < toastMinWidth {
		return nil
	}
	inner := width - toastChromeX
	border := cardBorderStyle()
	edge := border.Render("│")
	rows := []string{paint(border.Render("╭"+strings.Repeat("─", width-2)+"╮"), width, blockHex())}
	for _, line := range strings.Split(ansi.Wordwrap(text, inner, ""), "\n") {
		rows = append(rows, paint(edge+" "+padRight(line, inner)+" "+edge, width, blockHex()))
	}
	return append(rows, paint(border.Render("╰"+strings.Repeat("─", width-2)+"╯"), width, blockHex()))
}

// overlayTopRight splices a pre-painted box over the frame's rows, anchored
// under the header band and inset from the right edge. Rows past the frame's
// bottom are dropped rather than extending it, so the layout never moves.
func (m *Model) overlayTopRight(frame string, box []string, top int) string {
	if len(box) == 0 {
		return frame
	}
	left := m.width - maxLineWidth(box) - toastMargin
	if left < 0 {
		left = 0
	}
	lines := strings.Split(frame, "\n")
	for i, patch := range box {
		row := top + i
		if row < 0 || row >= len(lines) {
			continue
		}
		lines[row] = spliceAtColumn(lines[row], patch, left)
	}
	return strings.Join(lines, "\n")
}

// spliceAtColumn overpaints a run of cells inside a styled row, keeping the
// escape state on both sides of the patch intact.
func spliceAtColumn(row, patch string, left int) string {
	head := ansi.Truncate(row, left, "")
	if pad := left - ansi.StringWidth(head); pad > 0 {
		head += strings.Repeat(" ", pad)
	}
	tail := ansi.TruncateLeft(row, left+ansi.StringWidth(patch), "")
	return head + patch + tail
}
