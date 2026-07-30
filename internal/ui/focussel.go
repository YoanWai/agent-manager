package ui

import (
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// multiClickWindow is how close two presses must be, in time and place, to
// count as a double or triple click. Terminals report each press
// separately, so the run is reconstructed here.
const multiClickWindow = 400 * time.Millisecond

// selection granularity, widening with each click in a run.
const (
	selectChar = iota
	selectWord
	selectLine
)

// paneBox is where the focused pane's captured rows were last painted, in
// terminal cells. Recorded during render so hit-testing can never drift
// from what the user sees.
type paneBox struct {
	x, y, width, height int
	ok                  bool
}

// paneCursor is where the focused session's own cursor sits, in pane
// cells, as tmux last reported it.
type paneCursor struct {
	x, y int
	ok   bool
}

// cursorCell maps the pane cursor onto the rows the preview is painting.
// The cursor's row counts from the top of the capture, and a capture
// taller than the panel is shown from its bottom, so the row shifts by
// exactly the lines the panel dropped.
func (m *Model) cursorCell(paneLines int) (row, col int, ok bool) {
	cursor := m.paneCursor
	// The caret belongs to the live bottom, and only to the lit half of
	// the blink.
	if !cursor.ok || m.mode != modeFocus || paneLines <= 0 || !m.cursorOn || m.scrolledBack() {
		return 0, 0, false
	}
	captured := len(strings.Split(strings.TrimSuffix(m.preview, "\n"), "\n"))
	offset := 0
	if captured > paneLines {
		offset = captured - paneLines
	}
	row = cursor.y - offset
	if row < 0 || row >= paneLines {
		return 0, 0, false
	}
	return row, cursor.x, true
}

// focusSelection is a text selection drawn over the focused pane. anchor
// and head are pane-relative cell coordinates; the pane's own text is the
// source, so what gets copied is what tmux captured, not what the screen
// happens to render.
type focusSelection struct {
	active     bool
	dragging   bool
	granule    int
	anchorRow  int
	anchorCol  int
	headRow    int
	headCol    int
	lastClick  time.Time
	lastRow    int
	lastCol    int
	clickCount int
}

// paneOriginX is the first terminal column of the content panel's pane
// area: past the rail's edge cell, the rail, the seam and the bleed.
func (m *Model) paneOriginX() int { return m.paneColumnX }

// paneCell converts a terminal cell to a pane-relative coordinate. ok is
// false for anything outside the painted pane.
func (m *Model) paneCell(x, y int) (row, col int, ok bool) {
	box := m.paneBox
	if !box.ok || box.width <= 0 || box.height <= 0 {
		return 0, 0, false
	}
	if x < box.x || x >= box.x+box.width || y < box.y || y >= box.y+box.height {
		return 0, 0, false
	}
	return y - box.y, x - box.x, true
}

// paneTextLines is the focused pane's captured rows as plain text, in the
// same slice the renderer paints, so selection indices line up with what
// is on screen.
func (m *Model) paneTextLines() []string {
	rows := paneExact(m.preview, m.paneBox.height)
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = ansi.Strip(previewDangerSeqs.ReplaceAllString(row, ""))
	}
	return out
}

// handleFocusMouse owns the mouse while a session has focus: presses,
// drags and releases build a selection over the pane, and everything
// outside it is swallowed so a stray click cannot retarget the keyboard.
func (m *Model) handleFocusMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if tea.MouseEvent(msg).IsWheel() {
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			return m, m.wheelFocus(true, msg.X, msg.Y)
		case tea.MouseButtonWheelDown:
			return m, m.wheelFocus(false, msg.X, msg.Y)
		}
		return m, nil
	}
	switch msg.Action {
	case tea.MouseActionPress:
		if msg.Button != tea.MouseButtonLeft {
			return m, nil
		}
		row, col, ok := m.paneCell(msg.X, msg.Y)
		if !ok {
			m.sel = focusSelection{}
			return m, nil
		}
		m.startSelection(row, col)
		return m, nil

	case tea.MouseActionMotion:
		if !m.sel.dragging {
			return m, nil
		}
		row, col, ok := m.paneCell(msg.X, msg.Y)
		if !ok {
			return m, nil
		}
		m.sel.headRow, m.sel.headCol = row, col
		return m, nil

	case tea.MouseActionRelease:
		if !m.sel.dragging {
			return m, nil
		}
		m.sel.dragging = false
		return m, m.copySelectionCmd()
	}
	return m, nil
}

// startSelection opens a selection, widening the granularity when this
// press continues a click run at the same cell.
func (m *Model) startSelection(row, col int) {
	now := time.Now()
	sameSpot := row == m.sel.lastRow && col == m.sel.lastCol
	if sameSpot && now.Sub(m.sel.lastClick) < multiClickWindow {
		m.sel.clickCount++
	} else {
		m.sel.clickCount = 1
	}
	m.sel.lastClick, m.sel.lastRow, m.sel.lastCol = now, row, col

	m.copied = 0
	m.sel.active = true
	m.sel.dragging = true
	m.sel.anchorRow, m.sel.anchorCol = row, col
	m.sel.headRow, m.sel.headCol = row, col
	switch {
	case m.sel.clickCount >= 3:
		m.sel.granule = selectLine
	case m.sel.clickCount == 2:
		m.sel.granule = selectWord
	default:
		m.sel.granule = selectChar
	}
	m.expandSelection()
}

// expandSelection grows a word or line selection out from the clicked cell.
func (m *Model) expandSelection() {
	lines := m.paneTextLines()
	if m.sel.anchorRow >= len(lines) {
		return
	}
	line := []rune(lines[m.sel.anchorRow])
	switch m.sel.granule {
	case selectLine:
		m.sel.anchorCol = 0
		m.sel.headCol = len(line)
	case selectWord:
		start, end := wordBounds(line, m.sel.anchorCol)
		m.sel.anchorCol, m.sel.headCol = start, end
	}
}

// wordBounds is the run of word characters around col, or the single cell
// when it sits on a separator.
func wordBounds(line []rune, col int) (int, int) {
	if col >= len(line) {
		return col, col
	}
	if !isWordRune(line[col]) {
		return col, col + 1
	}
	start := col
	for start > 0 && isWordRune(line[start-1]) {
		start--
	}
	end := col
	for end < len(line) && isWordRune(line[end]) {
		end++
	}
	return start, end
}

// isWordRune keeps paths, flags and identifiers together on a double
// click: everything but whitespace and the punctuation that ends a token.
func isWordRune(r rune) bool {
	if r == ' ' || r == '\t' {
		return false
	}
	switch r {
	case '"', '\'', '`', '(', ')', '[', ']', '{', '}', ',', ';', ':', '|':
		return false
	}
	return true
}

// selectionRange normalizes anchor/head into forward order.
func (s focusSelection) selectionRange() (startRow, startCol, endRow, endCol int) {
	if s.anchorRow < s.headRow || (s.anchorRow == s.headRow && s.anchorCol <= s.headCol) {
		return s.anchorRow, s.anchorCol, s.headRow, s.headCol
	}
	return s.headRow, s.headCol, s.anchorRow, s.anchorCol
}

// selectionSpan is the selected column range on one pane row, or ok=false
// when the row carries no selection.
func (m *Model) selectionSpan(row, lineLen int) (start, end int, ok bool) {
	if !m.sel.active {
		return 0, 0, false
	}
	startRow, startCol, endRow, endCol := m.sel.selectionRange()
	if row < startRow || row > endRow {
		return 0, 0, false
	}
	start, end = 0, lineLen
	if row == startRow {
		start = startCol
	}
	if row == endRow {
		end = endCol
	}
	if start > lineLen {
		start = lineLen
	}
	if end > lineLen {
		end = lineLen
	}
	if end <= start {
		return 0, 0, false
	}
	return start, end, true
}

// selectionText is the selected pane text, newline-joined, with each row's
// trailing pad dropped the way a terminal's own copy does.
func (m *Model) selectionText() string {
	if !m.sel.active {
		return ""
	}
	lines := m.paneTextLines()
	startRow, _, endRow, _ := m.sel.selectionRange()
	var out []string
	for row := startRow; row <= endRow && row < len(lines); row++ {
		line := []rune(lines[row])
		start, end, ok := m.selectionSpan(row, len(line))
		if !ok {
			out = append(out, "")
			continue
		}
		out = append(out, strings.TrimRight(string(line[start:end]), " "))
	}
	return strings.Join(out, "\n")
}

// copySelectionCmd puts the selection on the system clipboard. The host
// terminal cannot do this itself while focus mode owns the mouse.
func (m *Model) copySelectionCmd() tea.Cmd {
	text := m.selectionText()
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return func() tea.Msg {
		if err := clipboard.WriteText(text); err != nil {
			return errMsg{err}
		}
		return focusCopiedMsg{chars: len([]rune(text))}
	}
}

// focusCopiedMsg reports a finished clipboard write so the status line can
// confirm it.
type focusCopiedMsg struct{ chars int }

// renderPaneRow draws one captured pane row, overlaying the selection when
// it covers part of it. A selected row is painted from its plain text: the
// agent's own colors would fight the highlight, and the selection is
// transient.
func (m *Model) renderPaneRow(row int, raw string, width int) string {
	line := []rune(ansi.Strip(previewDangerSeqs.ReplaceAllString(raw, "")))
	start, end, ok := m.selectionSpan(row, len(line))
	if !ok {
		return m.withCursor(row, raw, line, width)
	}
	before := string(line[:start])
	selected := string(line[start:end])
	after := string(line[end:])
	painted := before + selectionStyle().Render(selected) + after
	return previewLine(painted, width)
}

// withCursor draws the focused session's own cursor as a lit cell, since a
// captured pane carries no cursor of its own and the terminal's real one
// sits wherever our frame ended. The row keeps every colour the agent
// drew: only the caret cell is overpainted, spliced in by display column
// so the surrounding escape state survives on both sides of it.
func (m *Model) withCursor(row int, raw string, line []rune, width int) string {
	cursorRow, cursorCol, ok := m.cursorCell(m.paneBox.height)
	if !ok || cursorRow != row {
		return previewLine(raw, width)
	}
	clean := previewDangerSeqs.ReplaceAllString(raw, "")
	lineWidth := ansi.StringWidth(clean)
	if cursorCol >= lineWidth {
		// The caret sits on padding the row does not have, which is where
		// a prompt leaves it; pad up to it and light the cell there.
		pad := strings.Repeat(" ", cursorCol-lineWidth)
		return previewLine(clean+pad+cursorStyle().Render(" "), width)
	}
	head := ansi.Truncate(clean, cursorCol, "")
	tail := ansi.TruncateLeft(clean, cursorCol+1, "")
	index := runeAtColumn(line, cursorCol)
	cell := " "
	if index < len(line) {
		cell = string(line[index])
	}
	return previewLine(head+cursorStyle().Render(cell)+tail, width)
}

// runeAtColumn finds which rune sits at a display column, since tmux
// reports the cursor in cells and a wide rune covers two of them. Past the
// line's end it returns len(line).
func runeAtColumn(line []rune, column int) int {
	cell := 0
	for i, r := range line {
		next := cell + ansi.StringWidth(string(r))
		if column < next {
			return i
		}
		cell = next
	}
	return len(line)
}

// cursorStyle is the focused pane's cursor block: the accent behind the
// character it sits on, which reads as a cursor in either theme.
func cursorStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(colorBg).Background(colorAccent)
}

// selectionStyle is the highlight: the accent behind the terminal's own
// background color, so selected text stays legible in either theme.
func selectionStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(colorBg).Background(colorAccent2)
}
