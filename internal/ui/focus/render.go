package focus

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var dangerousPaneSequences = regexp.MustCompile(
	`\x1b\[[0-9;]*[KJLMSTr]|\x1b[DEM]`,
)

const capturedCursorMarker = "\x1b]1337;agent-manager-ime-cursor\x07"

// FrameContext is the copied presentation state needed to prepare one pane
// frame. OriginY is the first row available to the pane, before top padding.
type FrameContext struct {
	Preview        string
	Width          int
	Height         int
	OriginX        int
	OriginY        int
	CaretRow       int
	CursorStyle    lipgloss.Style
	SelectionStyle lipgloss.Style
}

type FrameContent struct {
	Rows       []string
	TopPadding int
	HasPane    bool
}

// PrepareFrame records the exact pane frame that mouse and IME policy
// will read until the next paint.
func (m *Model) PrepareFrame(ctx FrameContext) FrameContent {
	pane, rowOffset := paneExact(ctx.Preview, ctx.Height, ctx.Width, ctx.CaretRow)
	if len(pane) == 0 {
		m.frame = displayedFrame{}
		return FrameContent{}
	}
	topPadding := ctx.Height - len(pane)
	lines := make([]string, len(pane))
	for i, raw := range pane {
		lines[i] = plainPaneLine(raw)
	}
	m.frame = displayedFrame{
		box: Box{
			X:      ctx.OriginX,
			Y:      ctx.OriginY + topPadding,
			Width:  ctx.Width,
			Height: len(pane),
			Valid:  true,
		},
		lines:     lines,
		rowOffset: rowOffset,
	}

	rendered := make([]string, len(pane))
	for i, raw := range pane {
		rendered[i] = m.renderPaneRow(i, raw, ctx.Width, ctx.CursorStyle, ctx.SelectionStyle)
	}
	return FrameContent{Rows: rendered, TopPadding: topPadding, HasPane: true}
}

func (m Model) CursorAnchor(sessionID string) (col, row int, ok bool) {
	box := m.frame.box
	if m.scroll != 0 || !box.Valid || !m.pane.cursor.Visible || m.pane.sessionID != sessionID {
		return 0, 0, false
	}
	row = m.pane.cursor.Y - m.frame.rowOffset
	if row < 0 || row >= box.Height || box.Width <= 0 {
		return 0, 0, false
	}
	col = min(max(m.pane.cursor.X, 0), box.Width-1)
	return box.X + col + 1, box.Y + row + 1, true
}

func (m Model) cursorCell() (row, col int, ok bool) {
	if !m.pane.cursor.Visible || !m.cursorOn || m.scroll != 0 || !m.frame.box.Valid {
		return 0, 0, false
	}
	row = m.pane.cursor.Y - m.frame.rowOffset
	if row < 0 || row >= m.frame.box.Height {
		return 0, 0, false
	}
	return row, m.pane.cursor.X, true
}

func (m Model) renderPaneRow(row int, raw string, width int, cursorStyle, selectionStyle lipgloss.Style) string {
	clean := safePaneLine(raw)
	line := ansi.Strip(clean)
	if start, end, ok := m.selectionSpan(row, ansi.StringWidth(line)); ok {
		startByte, endByte := graphemeRangeAtColumns(line, start, end)
		selected := line[startByte:endByte]
		before := ansi.Truncate(clean, ansi.StringWidth(line[:startByte]), "")
		after := ansi.TruncateLeft(clean, ansi.StringWidth(line[:endByte]), "")
		return fitPaneLine(before+selectionStyle.Render(selected)+after, width)
	}
	return m.withCursor(row, raw, width, cursorStyle)
}

func (m Model) withCursor(row int, raw string, width int, style lipgloss.Style) string {
	cursorRow, cursorCol, ok := m.cursorCell()
	if !ok || cursorRow != row {
		return fitPaneLine(raw, width)
	}
	clean := safePaneLine(raw)
	lineWidth := ansi.StringWidth(clean)
	if cursorCol >= lineWidth {
		pad := strings.Repeat(" ", cursorCol-lineWidth)
		return fitPaneLine(clean+pad+style.Render(" "), width)
	}
	head := ansi.Truncate(clean, cursorCol, "")
	tail := ansi.TruncateLeft(clean, cursorCol+1, "")
	cell := graphemeAtCell(ansi.Strip(clean), cursorCol)
	return fitPaneLine(head+style.Render(cell)+tail, width)
}

func graphemeAtCell(line string, column int) string {
	for _, span := range graphemeSpans(line) {
		if column < span.endCell {
			return span.text
		}
	}
	return " "
}

func plainPaneLine(line string) string {
	return ansi.Strip(safePaneLine(line))
}

func fitPaneLine(line string, width int) string {
	line = safePaneLine(line)
	line = strings.Map(func(r rune) rune {
		if r < 0x20 && r != 0x1b && r != '\t' {
			return -1
		}
		return r
	}, line)
	w := ansi.StringWidth(line)
	if w > width {
		line = ansi.Truncate(line, width, "")
		w = ansi.StringWidth(line)
	}
	if strings.ContainsRune(line, 0x1b) {
		line += "\x1b[0m"
	}
	if w < width {
		line += strings.Repeat(" ", width-w)
	}
	return line
}

func safePaneLine(line string) string {
	line = strings.ReplaceAll(line, capturedCursorMarker, "")
	return dangerousPaneSequences.ReplaceAllString(line, "")
}

func paneExact(pane string, rows, width, caretRow int) ([]string, int) {
	lines, start := paneWindow(pane, rows, caretRow)
	for i, line := range lines {
		lines[i] = expandPaneTabs(line, width)
	}
	return lines, start
}

func paneWindow(pane string, rows, caretRow int) ([]string, int) {
	if rows <= 0 || pane == "" {
		return nil, 0
	}
	lines := strings.Split(strings.TrimSuffix(pane, "\n"), "\n")
	end := len(lines)
	for end > 0 && strings.TrimSpace(ansi.Strip(lines[end-1])) == "" {
		end--
	}
	if end == 0 {
		end = min(rows, len(lines))
	}
	if caretRow+1 > end {
		end = caretRow + 1
	}
	end = min(end, len(lines))
	start := max(end-rows, 0)
	return lines[start:end], start
}

func expandPaneTabs(line string, width int) string {
	if !strings.ContainsRune(line, '\t') {
		return line
	}
	const tabStop = 8
	var out strings.Builder
	column := 0
	for i, segment := range strings.Split(line, "\t") {
		if i > 0 {
			pad := max(min(tabStop-column%tabStop, width-1-column), 0)
			out.WriteString(strings.Repeat(" ", pad))
			column += pad
		}
		out.WriteString(segment)
		column += ansi.StringWidth(segment)
	}
	return out.String()
}
