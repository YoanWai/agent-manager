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
// cells, as tmux last reported it. positionOK stays true when the
// application hides that cursor but tmux still reports its coordinates.
type paneCursor struct {
	x, y       int
	ok         bool
	positionOK bool
}

// cursorCell maps the pane cursor onto the rows the preview is painting.
// The cursor's row counts from the top of the capture, and a capture
// taller than the panel is shown from its bottom, so the row shifts by
// exactly the lines the panel dropped.
func (m *Model) cursorCell(paneLines int) (row, col int, ok bool) {
	cursor := m.pane.cursor
	// The caret belongs to the live bottom, and only to the lit half of
	// the blink.
	if !cursor.ok || m.mode != modeFocus || paneLines <= 0 || !m.cursorOn || m.scrolledBack() {
		return 0, 0, false
	}
	row = cursor.y - m.paneRowOffset(paneLines)
	if row < 0 || row >= paneLines {
		return 0, 0, false
	}
	return row, cursor.x, true
}

// paneRowOffset is how many captured rows the panel dropped off the top,
// which is exactly what separates a painted row from the pane's own row.
// It reads the same crop window the renderer paints, so caret and mouse
// coordinates can never drift from what the user sees.
func (m *Model) paneRowOffset(paneLines int) int {
	_, start := paneWindow(m.preview, paneLines, m.paneCaretRow())
	return start
}

// paneCaretRow is the capture row the live caret sits on, or -1 when no
// caret is in play. The crop keeps this row painted whatever the blank
// rows around it look like: it is where typing lands. A tool that paints
// its own composer cursor (command-code) is the exception: it rests the
// terminal caret on a blank row below its content where typing never
// lands, and extending the crop to it would only drag those blank rows
// into view above it. For every other tool a caret on a blank row IS the
// typing point - a shell waiting below its output - and stays pinned.
func (m *Model) paneCaretRow() int {
	if m.mode != modeFocus || !m.pane.cursor.ok || m.scrolledBack() {
		return -1
	}
	caret := m.pane.cursor.y
	if sess, ok := m.selected(); ok && m.engine != nil && m.engine.ParksItsCaret(sess.Tool) {
		rows := strings.Split(strings.TrimSuffix(m.preview, "\n"), "\n")
		if caret < len(rows) && caretParkedBelowContent(rows, caret, m.pane.cursor.x) {
			return -1
		}
	}
	return caret
}

// caretParkedBelowContent reports whether the caret rests at column zero
// on a blank row with nothing but blank rows beneath the pane's content,
// which is the parking spot of a tool that paints its own composer cursor.
func caretParkedBelowContent(rows []string, caret, column int) bool {
	if column != 0 {
		return false
	}
	for y := caret; y < len(rows); y++ {
		if strings.TrimSpace(ansi.Strip(rows[y])) != "" {
			return false
		}
	}
	for y := caret - 1; y >= 0; y-- {
		if strings.TrimSpace(ansi.Strip(rows[y])) != "" {
			return true
		}
	}
	return false
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
func (m *Model) paneOriginX() int { return m.pane.columnX }

// paneCell converts a terminal cell to a pane-relative coordinate. ok is
// false for anything outside the painted pane.
func (m *Model) paneCell(x, y int) (row, col int, ok bool) {
	box := m.pane.box
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
	rows := paneExact(m.preview, m.pane.box.height, m.pane.box.width, m.paneCaretRow())
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = ansi.Strip(previewDangerSeqs.ReplaceAllString(row, ""))
	}
	return out
}

// pendingClick holds a press in a mouse-tracking pane until the gesture
// declares itself: motion turns it into a selection drag, release on the
// same cell proves it a click and forwards it to the pane's application.
type pendingClick struct {
	active   bool
	button   int
	row, col int
}

// handleFocusMouse owns the mouse while a session has focus. In a pane
// whose application tracks the mouse, a click passes through to that app
// while a drag still selects for copy; the press is held back until one of
// the two is proven. Alt forces a whole gesture through, drags included.
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
	if msg.Action == tea.MouseActionPress && msg.Alt && m.pane.mouse {
		if row, col, inside := m.paneCell(msg.X, msg.Y); inside {
			m.clearSelection()
			m.pending = pendingClick{}
			m.forwardingMouse = true
			m.forwardingButton = mouseButton(msg.Button)
			m.forwardingRow, m.forwardingCol = row, col
		}
	}
	if m.forwardingMouse {
		model, cmd := m.forwardFocusMouse(msg)
		if msg.Action == tea.MouseActionRelease {
			m.clearForwardingMouse()
		}
		return model, cmd
	}
	switch msg.Action {
	case tea.MouseActionPress:
		if msg.Button != tea.MouseButtonLeft {
			return m, nil
		}
		row, col, ok := m.paneCell(msg.X, msg.Y)
		if !ok {
			m.clearSelection()
			return m, nil
		}
		// A repeated press at the same cell is the user asking for a word or
		// line: keep the click run on the selection path. The first press of
		// the run has already reached the app; a lone click is harmless there.
		if m.pane.mouse && !m.clickRunContinues(row, col) {
			m.deferClick(mouseButton(msg.Button), row, col)
			return m, nil
		}
		m.startSelection(row, col)
		return m, nil

	case tea.MouseActionMotion:
		if m.pending.active {
			if row, col, ok := m.paneCell(msg.X, msg.Y); ok && row == m.pending.row && col == m.pending.col {
				return m, nil
			}
			m.beginSelection(m.pending.row, m.pending.col)
			m.pending = pendingClick{}
		}
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
		if m.pending.active {
			pending := m.pending
			m.pending = pendingClick{}
			row, col, ok := m.paneCell(msg.X, msg.Y)
			if ok && row == pending.row && col == pending.col {
				// A click on a link opens it here: the terminal's own
				// opener cannot reach through the mouse claim, and the
				// application under the pane has no opener of its own.
				if url := m.linkAt(pending.row, pending.col); url != "" {
					return m, openLinkCmd(url)
				}
				m.forwardClick(pending.button, pending.row, pending.col)
				return m, nil
			}
			// A release away from the press with no motion between them is
			// a drag from a terminal that reports no motion: select it.
			m.beginSelection(pending.row, pending.col)
			m.sel.dragging = false
			if ok {
				m.sel.headRow, m.sel.headCol = row, col
			}
			return m, m.copySelectionCmd()
		}
		if !m.sel.dragging {
			return m, nil
		}
		m.sel.dragging = false
		// A press that never moved is a click, not a copy: on a link it
		// opens the link, the same answer either kind of pane gives.
		if m.sel.clickCount == 1 && m.sel.anchorRow == m.sel.headRow && m.sel.anchorCol == m.sel.headCol {
			if url := m.linkAt(m.sel.anchorRow, m.sel.anchorCol); url != "" {
				m.clearSelection()
				return m, openLinkCmd(url)
			}
		}
		return m, m.copySelectionCmd()
	}
	return m, nil
}

// deferClick parks a press until motion or release resolves the gesture,
// recording it as the start of a possible double or triple click run. The
// press drops the earlier selection whichever way it resolves: a highlight
// left standing under a click elsewhere reads as still selected.
func (m *Model) deferClick(button, row, col int) {
	m.clearSelection()
	m.pending = pendingClick{active: true, button: button, row: row, col: col}
	m.sel.lastClick, m.sel.lastRow, m.sel.lastCol = time.Now(), row, col
	m.sel.clickCount = 1
}

// clearSelection drops the highlight along with the copy confirmation that
// belongs to it.
func (m *Model) clearSelection() {
	m.sel = focusSelection{}
	m.copied = 0
	m.copyGen++
}

// clickRunContinues reports whether a press at this cell extends the click
// run the previous press opened.
func (m *Model) clickRunContinues(row, col int) bool {
	return row == m.sel.lastRow && col == m.sel.lastCol &&
		time.Since(m.sel.lastClick) < multiClickWindow
}

// forwardClick sends a full press-release pair to the focused pane's
// application: the press was held back until release proved the gesture a
// click rather than a selection drag.
func (m *Model) forwardClick(button, row, col int) {
	paneRow := row + m.paneRowOffset(m.pane.box.height)
	press, ok := m.mouseReport(button, false, col, paneRow)
	if !ok {
		return
	}
	release, ok := m.mouseReport(button, true, col, paneRow)
	if !ok {
		return
	}
	m.sendFocusReport(press + release)
}

func (m *Model) clearForwardingMouse() {
	m.forwardingMouse = false
	m.forwardingButton = leftButton
	m.forwardingRow, m.forwardingCol = 0, 0
}

// startSelection opens a selection, widening the granularity when this
// press continues a click run at the same cell.
func (m *Model) startSelection(row, col int) {
	if m.clickRunContinues(row, col) {
		m.sel.clickCount++
	} else {
		m.sel.clickCount = 1
	}
	m.sel.lastClick, m.sel.lastRow, m.sel.lastCol = time.Now(), row, col
	m.beginSelection(row, col)
}

// beginSelection anchors a selection at the cell using the current click
// count, without treating the call as a fresh press: a deferred press that
// motion turned into a drag anchors here without widening the run.
func (m *Model) beginSelection(row, col int) {
	m.copied = 0
	m.copyGen++
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
	line := lines[m.sel.anchorRow]
	switch m.sel.granule {
	case selectLine:
		m.sel.anchorCol = 0
		m.sel.headCol = ansi.StringWidth(line)
	case selectWord:
		start, end := wordBounds(line, m.sel.anchorCol)
		m.sel.anchorCol, m.sel.headCol = start, end
	}
}

// graphemeSpan maps one displayed grapheme to its string bytes and cells.
// Mouse coordinates are cells, but strings must be sliced at grapheme edges.
type graphemeSpan struct {
	start, end         int
	startCell, endCell int
	text               string
}

func graphemeSpans(line string) []graphemeSpan {
	var spans []graphemeSpan
	for offset, cell := 0, 0; offset < len(line); {
		text, width := ansi.FirstGraphemeCluster(line[offset:], ansi.GraphemeWidth)
		if text == "" {
			break
		}
		spans = append(spans, graphemeSpan{
			start: offset, end: offset + len(text), startCell: cell, endCell: cell + width, text: text,
		})
		offset += len(text)
		cell += width
	}
	return spans
}

func graphemeAtColumn(spans []graphemeSpan, col int) int {
	for i, span := range spans {
		if col < span.endCell {
			return i
		}
	}
	return len(spans)
}

// wordBounds is the run of word graphemes around col, or the clicked
// grapheme when it sits on a separator.
func wordBounds(line string, col int) (int, int) {
	spans := graphemeSpans(line)
	index := graphemeAtColumn(spans, col)
	if index >= len(spans) {
		return col, col
	}
	if !isWordGrapheme(spans[index].text) {
		return spans[index].startCell, spans[index].endCell
	}
	start := index
	for start > 0 && isWordGrapheme(spans[start-1].text) {
		start--
	}
	end := index
	for end < len(spans) && isWordGrapheme(spans[end].text) {
		end++
	}
	return spans[start].startCell, spans[end-1].endCell
}

func isWordGrapheme(text string) bool {
	for _, r := range text {
		if !isWordRune(r) {
			return false
		}
	}
	return text != ""
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
func (m *Model) selectionSpan(row, lineWidth int) (start, end int, ok bool) {
	if !m.sel.active {
		return 0, 0, false
	}
	startRow, startCol, endRow, endCol := m.sel.selectionRange()
	if row < startRow || row > endRow {
		return 0, 0, false
	}
	start, end = 0, lineWidth
	if row == startRow {
		start = startCol
	}
	if row == endRow {
		end = endCol
	}
	if start > lineWidth {
		start = lineWidth
	}
	if end > lineWidth {
		end = lineWidth
	}
	if end <= start {
		return 0, 0, false
	}
	return start, end, true
}

// graphemeRangeAtColumns translates a terminal-cell span to byte offsets
// without splitting wide or multi-rune graphemes. A drag endpoint inside a
// wide glyph expands to that glyph's far edge, matching terminal selection.
func graphemeRangeAtColumns(line string, start, end int) (int, int) {
	startByte, endByte := len(line), len(line)
	for _, span := range graphemeSpans(line) {
		if startByte == len(line) && start < span.endCell {
			startByte = span.start
		}
		if endByte == len(line) && end <= span.endCell {
			endByte = span.end
		}
	}
	return startByte, endByte
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
		line := lines[row]
		start, end, ok := m.selectionSpan(row, ansi.StringWidth(line))
		if !ok {
			out = append(out, "")
			continue
		}
		startByte, endByte := graphemeRangeAtColumns(line, start, end)
		out = append(out, strings.TrimRight(line[startByte:endByte], " "))
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
	gen := m.copyGen
	return func() tea.Msg {
		if err := clipboard.WriteText(text); err != nil {
			return errMsg{err}
		}
		return focusCopiedMsg{chars: len([]rune(text)), gen: gen}
	}
}

// focusCopiedMsg reports a finished clipboard write so the status line can
// confirm it.
type focusCopiedMsg struct {
	chars int
	gen   int
}

// renderPaneRow draws one captured pane row, overlaying the selection when
// it covers part of it. The agent's own styling survives on both sides of
// the highlight; only the selected span is repainted. The splice points are
// the selection's grapheme-snapped columns, so a wide grapheme under an
// edge stays on exactly one side.
func (m *Model) renderPaneRow(row int, raw string, width int) string {
	clean := previewDangerSeqs.ReplaceAllString(raw, "")
	line := ansi.Strip(clean)
	if !m.sel.active {
		return m.withCursor(row, raw, []rune(line), width)
	}
	start, end, ok := m.selectionSpan(row, ansi.StringWidth(line))
	if !ok {
		return m.withCursor(row, raw, []rune(line), width)
	}
	startByte, endByte := graphemeRangeAtColumns(line, start, end)
	selected := line[startByte:endByte]
	before := ansi.Truncate(clean, ansi.StringWidth(line[:startByte]), "")
	after := ansi.TruncateLeft(clean, ansi.StringWidth(line[:endByte]), "")
	painted := before + selectionStyle().Render(selected) + after
	return previewLine(painted, width)
}

// withCursor draws the focused session's own cursor as a lit cell, since a
// captured pane carries no cursor of its own and the terminal's real one
// sits wherever our frame ended. The row keeps every colour the agent
// drew: only the caret cell is overpainted, spliced in by display column
// so the surrounding escape state survives on both sides of it.
func (m *Model) withCursor(row int, raw string, line []rune, width int) string {
	cursorRow, cursorCol, ok := m.cursorCell(m.pane.box.height)
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
