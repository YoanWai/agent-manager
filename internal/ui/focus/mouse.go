package focus

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

const multiClickWindow = 400 * time.Millisecond

const (
	selectChar = iota
	selectWord
	selectLine
)

const (
	wheelUpButton   = 64
	wheelDownButton = 65
	motionButton    = 35
	leftButton      = 0
	middleButton    = 1
	rightButton     = 2
	motionBit       = 32
	x10Limit        = 223
)

var paneLinkPattern = regexp.MustCompile(`https?://\S+`)

type selection struct {
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

type pendingClick struct {
	active   bool
	button   int
	row, col int
}

type forwardedGesture struct {
	active   bool
	button   int
	row, col int
}

type MouseContext struct {
	SessionID    string
	Rows         int
	Now          time.Time
	RuntimeReady bool
}

type CopyRequest struct {
	Text       string
	Generation uint64
}

// MouseResult contains semantic outcomes for one focused-pane gesture. Root
// adapters own each effect named here.
type MouseResult struct {
	Leave      bool
	OpenURL    string
	SendReport string
	// Input marks a report that is the user's own input to the pane, so its
	// echo is captured promptly; motion and the wheel are not.
	Input  bool
	Copy   *CopyRequest
	Region *RegionRequest
}

func (m *Model) Mouse(msg tea.MouseMsg, ctx MouseContext) MouseResult {
	if tea.MouseEvent(msg).IsWheel() {
		if !ctx.RuntimeReady {
			return MouseResult{}
		}
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			return m.wheel(true, msg.X, msg.Y, ctx)
		case tea.MouseButtonWheelDown:
			return m.wheel(false, msg.X, msg.Y, ctx)
		}
		return MouseResult{}
	}
	if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonBackward {
		return MouseResult{Leave: true, SendReport: m.Leave()}
	}
	if msg.Action == tea.MouseActionPress && msg.Alt && m.pane.mouse {
		if row, col, inside := m.paneCell(msg.X, msg.Y); inside {
			m.clearSelection()
			m.pending = pendingClick{}
			m.forward = forwardedGesture{active: true, button: mouseButton(msg.Button), row: row, col: col}
		}
	}
	if m.forward.active {
		return m.forwardMouse(msg)
	}

	now := ctx.Now
	if now.IsZero() {
		now = time.Now()
	}
	switch msg.Action {
	case tea.MouseActionPress:
		if msg.Button != tea.MouseButtonLeft {
			return MouseResult{}
		}
		row, col, ok := m.paneCell(msg.X, msg.Y)
		if !ok {
			m.clearSelection()
			return MouseResult{}
		}
		if m.pane.mouse && !m.clickRunContinues(row, col, now) {
			m.deferClick(mouseButton(msg.Button), row, col, now)
			return MouseResult{}
		}
		m.startSelection(row, col, now)

	case tea.MouseActionMotion:
		if m.pending.active {
			if row, col, ok := m.paneCell(msg.X, msg.Y); ok && row == m.pending.row && col == m.pending.col {
				return MouseResult{}
			}
			m.beginSelection(m.pending.row, m.pending.col)
			m.pending = pendingClick{}
		}
		if !m.selection.dragging {
			return MouseResult{}
		}
		if row, col, ok := m.paneCell(msg.X, msg.Y); ok {
			m.selection.headRow, m.selection.headCol = row, col
		}

	case tea.MouseActionRelease:
		if m.pending.active {
			pending := m.pending
			m.pending = pendingClick{}
			row, col, ok := m.paneCell(msg.X, msg.Y)
			if ok && row == pending.row && col == pending.col {
				if url := m.linkAt(pending.row, pending.col); url != "" {
					return MouseResult{OpenURL: url}
				}
				return MouseResult{SendReport: m.clickReport(pending.button, pending.row, pending.col)}
			}
			m.beginSelection(pending.row, pending.col)
			m.selection.dragging = false
			if ok {
				m.selection.headRow, m.selection.headCol = row, col
			}
			return MouseResult{Copy: m.copyRequest()}
		}
		if !m.selection.dragging {
			return MouseResult{}
		}
		m.selection.dragging = false
		if m.selection.clickCount == 1 &&
			m.selection.anchorRow == m.selection.headRow &&
			m.selection.anchorCol == m.selection.headCol {
			if url := m.linkAt(m.selection.anchorRow, m.selection.anchorCol); url != "" {
				m.clearSelection()
				return MouseResult{OpenURL: url}
			}
		}
		return MouseResult{Copy: m.copyRequest()}
	}
	return MouseResult{}
}

func (m *Model) wheel(up bool, x, y int, ctx MouseContext) MouseResult {
	if m.pane.mouse {
		row, col, inside := m.paneCell(x, y)
		if !inside {
			return MouseResult{}
		}
		button := wheelDownButton
		if up {
			button = wheelUpButton
		}
		report, ok := m.mouseReport(button, false, col, row+m.frame.rowOffset)
		if !ok {
			return MouseResult{}
		}
		return MouseResult{SendReport: report}
	}
	delta := 1
	if up {
		delta = -1
	}
	return MouseResult{Region: m.Scroll(delta, ctx.SessionID, ctx.Rows)}
}

func (m *Model) forwardMouse(msg tea.MouseMsg) MouseResult {
	row, col, ok := m.forwardedCell(msg)
	if !ok {
		return MouseResult{}
	}
	button := mouseButton(msg.Button)
	if msg.Action == tea.MouseActionRelease && msg.Button == tea.MouseButtonNone {
		button = m.forward.button
	}
	if msg.Action == tea.MouseActionMotion {
		button |= motionBit
	}
	report, ok := m.mouseReport(button, msg.Action == tea.MouseActionRelease, col, row+m.frame.rowOffset)
	if msg.Action == tea.MouseActionRelease {
		m.forward = forwardedGesture{}
	}
	if !ok {
		return MouseResult{}
	}
	return MouseResult{SendReport: report, Input: msg.Action != tea.MouseActionMotion}
}

func (m *Model) forwardedCell(msg tea.MouseMsg) (row, col int, ok bool) {
	if row, col, inside := m.paneCell(msg.X, msg.Y); inside {
		m.forward.row, m.forward.col = row, col
		return row, col, true
	}
	if msg.Action == tea.MouseActionRelease && m.forward.active {
		return m.forward.row, m.forward.col, true
	}
	return 0, 0, false
}

func (m *Model) endForwardedGesture() string {
	if !m.forward.active {
		return ""
	}
	report, _ := m.mouseReport(m.forward.button, true, m.forward.col, m.forward.row+m.frame.rowOffset)
	m.forward = forwardedGesture{}
	return report
}

func (m Model) paneCell(x, y int) (row, col int, ok bool) {
	box := m.frame.box
	if !box.Valid || box.Width <= 0 || box.Height <= 0 ||
		x < box.X || x >= box.X+box.Width || y < box.Y || y >= box.Y+box.Height {
		return 0, 0, false
	}
	return y - box.Y, x - box.X, true
}

func (m *Model) deferClick(button, row, col int, now time.Time) {
	m.clearSelection()
	m.pending = pendingClick{active: true, button: button, row: row, col: col}
	m.selection.lastClick, m.selection.lastRow, m.selection.lastCol = now, row, col
	m.selection.clickCount = 1
}

func (m Model) clickRunContinues(row, col int, now time.Time) bool {
	return row == m.selection.lastRow && col == m.selection.lastCol &&
		now.Sub(m.selection.lastClick) < multiClickWindow
}

func (m *Model) startSelection(row, col int, now time.Time) {
	if m.clickRunContinues(row, col, now) {
		m.selection.clickCount++
	} else {
		m.selection.clickCount = 1
	}
	m.selection.lastClick, m.selection.lastRow, m.selection.lastCol = now, row, col
	m.beginSelection(row, col)
}

func (m *Model) beginSelection(row, col int) {
	m.copied = 0
	m.copyGen++
	m.selection.active = true
	m.selection.dragging = true
	m.selection.anchorRow, m.selection.anchorCol = row, col
	m.selection.headRow, m.selection.headCol = row, col
	switch {
	case m.selection.clickCount >= 3:
		m.selection.granule = selectLine
	case m.selection.clickCount == 2:
		m.selection.granule = selectWord
	default:
		m.selection.granule = selectChar
	}
	m.expandSelection()
}

func (m *Model) expandSelection() {
	if m.selection.anchorRow >= len(m.frame.lines) {
		return
	}
	line := m.frame.lines[m.selection.anchorRow]
	switch m.selection.granule {
	case selectLine:
		m.selection.anchorCol = 0
		m.selection.headCol = ansi.StringWidth(line)
	case selectWord:
		m.selection.anchorCol, m.selection.headCol = wordBounds(line, m.selection.anchorCol)
	}
}

func (s selection) selectionRange() (startRow, startCol, endRow, endCol int) {
	if s.anchorRow < s.headRow || (s.anchorRow == s.headRow && s.anchorCol <= s.headCol) {
		return s.anchorRow, s.anchorCol, s.headRow, s.headCol
	}
	return s.headRow, s.headCol, s.anchorRow, s.anchorCol
}

func (m Model) selectionSpan(row, lineWidth int) (start, end int, ok bool) {
	if !m.selection.active {
		return 0, 0, false
	}
	startRow, startCol, endRow, endCol := m.selection.selectionRange()
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
	start = min(start, lineWidth)
	end = min(end, lineWidth)
	if end <= start {
		return 0, 0, false
	}
	return start, end, true
}

func (m Model) selectionText() string {
	if !m.selection.active {
		return ""
	}
	startRow, _, endRow, _ := m.selection.selectionRange()
	var out []string
	for row := startRow; row <= endRow && row < len(m.frame.lines); row++ {
		line := m.frame.lines[row]
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

func (m Model) copyRequest() *CopyRequest {
	text := m.selectionText()
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return &CopyRequest{Text: text, Generation: m.copyGen}
}

func (m Model) linkAt(row, col int) string {
	if row < 0 || row >= len(m.frame.lines) {
		return ""
	}
	line := m.frame.lines[row]
	clicked, _ := graphemeRangeAtColumns(line, col, col+1)
	for _, span := range paneLinkPattern.FindAllStringIndex(line, -1) {
		if clicked < span[0] || clicked >= span[1] {
			continue
		}
		url := line[span[0]:span[1]]
		current, end := line, span[1]
		for next := row + 1; end == len(current) &&
			ansi.StringWidth(current) >= m.frame.box.Width && next < len(m.frame.lines); next++ {
			rest := m.frame.lines[next]
			if rest == "" || strings.HasPrefix(rest, " ") {
				break
			}
			fields := strings.Fields(rest)
			url += fields[0]
			if len(fields) > 1 || ansi.StringWidth(rest) < m.frame.box.Width {
				break
			}
			current, end = rest, len(rest)
		}
		return trimLinkPunctuation(url)
	}
	return ""
}

func trimLinkPunctuation(url string) string {
	url = strings.TrimRight(url, `.,;:!?'"`)
	for _, pair := range [][2]string{{"(", ")"}, {"[", "]"}, {"<", ">"}} {
		for strings.HasSuffix(url, pair[1]) && strings.Count(url, pair[1]) > strings.Count(url, pair[0]) {
			url = strings.TrimSuffix(url, pair[1])
			url = strings.TrimRight(url, `.,;:!?'"`)
		}
	}
	return url
}

func (m Model) clickReport(button, row, col int) string {
	paneRow := row + m.frame.rowOffset
	press, ok := m.mouseReport(button, false, col, paneRow)
	if !ok {
		return ""
	}
	release, ok := m.mouseReport(button, true, col, paneRow)
	if !ok {
		return ""
	}
	return press + release
}

func (m Model) mouseReport(button int, release bool, col, row int) (string, bool) {
	if m.pane.sgr {
		terminator := "M"
		if release {
			terminator = "m"
		}
		report := fmt.Sprintf("\x1b[<%d;%d;%d%s", button, col+1, row+1, terminator)
		if m.pane.motion && !release {
			report = sgrMouse(motionButton, col, row) + report
		}
		return report, true
	}
	if release {
		button = 3
	}
	report, ok := x10Mouse(button, col, row)
	if !ok {
		return "", false
	}
	if m.pane.motion && !release {
		move, moveOK := x10Mouse(motionButton, col, row)
		if !moveOK {
			return "", false
		}
		report = move + report
	}
	return report, true
}

func sgrMouse(button, col, row int) string {
	return fmt.Sprintf("\x1b[<%d;%d;%dM", button, col+1, row+1)
}

func x10Mouse(button, col, row int) (string, bool) {
	if col >= x10Limit || row >= x10Limit {
		return "", false
	}
	return string([]byte{0x1b, '[', 'M', byte(32 + button), byte(33 + col), byte(33 + row)}), true
}

func mouseButton(button tea.MouseButton) int {
	switch button {
	case tea.MouseButtonMiddle:
		return middleButton
	case tea.MouseButtonRight:
		return rightButton
	default:
		return leftButton
	}
}

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
