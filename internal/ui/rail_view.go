package ui

import (
	"fmt"
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"strings"
)

// railGutter is the left inset every rail line shares, and contentGutter
// the content column's. Consistent insets are what make an unbordered
// layout read as columns.
const (
	railGutter    = 2
	contentGutter = 2
	// railInset is the pad inside the rail's own column, one short of
	// railGutter because the edge column already occupies the first cell.
	railInset = railGutter - 1
)

const shellGlyph = "❯"

// viewListFrame is the sessions rail beside the session content, both
// painted surfaces rather than drawn panels.
func (m *Model) viewListFrame() string {
	if m.fullFocus() {
		return m.viewFullFocusFrame()
	}
	if m.fullRows() {
		return m.viewFullListFrame()
	}
	leftWidth, rightWidth := m.splitWidths()
	footer := m.viewFooter()
	bodyHeight := m.listBodyHeight()

	// The header is one full-width band over both columns, closed by a
	// rule; the seam between the rail and the content tees into that rule
	// and runs down to meet the footer's.
	contentWidth := rightWidth - 1

	frame := []string{}
	for _, line := range m.viewHeaderRows() {
		frame = append(frame, paint(line, m.width, backdropHex()))
	}
	// The rail's fill runs from the edge column through the seam column,
	// then bleeds half a cell further right, and half a cell above and
	// below its body rows — soft edges drawn with half blocks, the finest
	// step a character cell allows. The first column is drawn as foreground
	// blocks so the window margin beside it keeps the terminal's own
	// background and the fill's corners land exactly on the cell grid.
	bleedWidth := contentWidth - 1
	railWidth := leftWidth - 1
	railRows := m.railLines(railWidth, bodyHeight)
	m.recordRailHits(railRows)
	contentRows := m.contentLines(bleedWidth, bodyHeight)
	seam := make([]string, bodyHeight)
	edge := make([]string, bodyHeight)
	for i := range seam {
		seam[i] = m.seamCell(i < len(railRows) && railRows[i].rule)
		tone := panelHex()
		if i < len(railRows) && railRows[i].tone != "" {
			tone = railRows[i].tone
		}
		edge[i] = railEdgeCell(tone)
	}
	frame = append(frame, m.railTopRow(leftWidth+1, m.width))
	frame = append(frame, joinColumns(
		edge,
		paintContent(railRows, railWidth, bodyHeight, panelHex()),
		seam,
		m.bleedColumn(bodyHeight),
		paintContent(contentRows, bleedWidth, bodyHeight, backdropHex()),
	)...)
	bottom := m.boundedRuleRow(leftWidth+1, m.width, "▄")
	if m.mode == modeFocus && m.focusPane.FrameBox().Valid {
		bottom = m.focusBottomRule(leftWidth+1, m.width)
	}
	frame = append(frame, bottom)
	for _, line := range splitLines(footer) {
		frame = append(frame, paint(line, m.width, backdropHex()))
	}
	return m.overlayTopRight(strings.Join(frame, "\n"), m.statusToast(), m.listChromeRows()+1)
}

// fullRows reports whether the list is on the full screen layout, whose
// rows and frame both differ from the split's. Focused, the list is not
// on screen at all: the session owns the body through viewFullFocusFrame.
func (m *Model) fullRows() bool {
	return m.prefs.fullLayout && m.mode != modeFocus
}

// viewFullListFrame is the full screen layout: the rail owns the whole
// width, so there is no seam, no bleed and no content column beside it.
// The detail head and the preview belong to the split alone; the fill's
// soft top and bottom edges run to the terminal's right edge instead of
// to a seam.
func (m *Model) viewFullListFrame() string {
	footer := m.viewFooter()
	bodyHeight := m.listBodyHeight()
	railWidth := m.width - 1

	frame := []string{}
	for _, line := range m.viewHeaderRows() {
		frame = append(frame, paint(line, m.width, backdropHex()))
	}
	quickRows := m.fullQuickLines(railWidth, bodyHeight)
	railRows := m.railLines(railWidth, bodyHeight-len(quickRows))
	railRows = append(railRows, quickRows...)
	m.recordRailHits(railRows)
	edge := make([]string, bodyHeight)
	for i := range edge {
		tone := panelHex()
		if i < len(railRows) && railRows[i].tone != "" {
			tone = railRows[i].tone
		}
		edge[i] = railEdgeCell(tone)
	}
	frame = append(frame, m.railTopRow(railWidth, m.width))
	frame = append(frame, joinColumns(
		edge,
		paintContent(railRows, railWidth, bodyHeight, panelHex()),
	)...)
	frame = append(frame, m.boundedRuleRow(railWidth, m.width, "▄"))
	for _, line := range splitLines(footer) {
		frame = append(frame, paint(line, m.width, backdropHex()))
	}
	return m.overlayTopRight(strings.Join(frame, "\n"), m.statusToast(), m.listChromeRows()+1)
}

func (m *Model) highlightQuery(name string, base lipgloss.Style) string {
	query := []rune(strings.TrimSpace(m.rail.search))
	runes := []rune(name)
	if len(query) == 0 {
		return base.Render(name)
	}
	for start := 0; start+len(query) <= len(runes); start++ {
		end := start + len(query)
		if !strings.EqualFold(string(runes[start:end]), string(query)) {
			continue
		}
		return base.Render(string(runes[:start])) +
			searchMatchStyle.Render(string(runes[start:end])) +
			base.Render(string(runes[end:]))
	}
	return base.Render(name)
}

// searchFieldLine is the live filter at the head of the rail: the typed
// query with a caret, and the key that closes it when there is room. With
// the field closed and a query still applied it drops the caret and offers
// to clear instead, so the rail always accounts for the entries it is
// holding back.
func (m *Model) searchFieldLine(width int) string {
	indent := strings.Repeat(" ", railInset)
	glyph := keyStyle.Render("⌕ ")
	caret := cursorAnchorMarker + lipgloss.NewStyle().Foreground(colorAccent).Render("▏")
	hint := keyCapQuiet("esc", "close")
	if !m.rail.searching {
		caret, hint = "", keyCapQuiet("esc", "clear")
	}
	chrome := railInset + ansi.StringWidth(glyph) + ansi.StringWidth(caret)

	if m.rail.search == "" {
		field := glyph + subtleStyle.Render("type to filter") + caret
		if gap := width - railInset - ansi.StringWidth(field) - ansi.StringWidth(hint) - 1; gap >= 2 {
			return indent + field + strings.Repeat(" ", gap) + hint
		}
		return indent + field
	}
	// A query longer than the rail keeps its end: that is where the caret is
	// and where the next keystroke lands.
	room := width - chrome - ansi.StringWidth(hint) - 2
	if room < 8 {
		hint, room = "", width-chrome
	}
	query := m.rail.search
	if ansi.StringWidth(query) > room {
		query = "…" + string([]rune(query)[len([]rune(query))-max(room-1, 1):])
	}
	field := glyph + valueStyle.Render(query) + caret
	if hint == "" {
		return indent + field
	}
	gap := width - railInset - ansi.StringWidth(field) - ansi.StringWidth(hint) - 1
	return indent + field + strings.Repeat(" ", max(gap, 1)) + hint
}

// railLines is the sessions rail: the entry list on top, the machine
// meters and the messages card docked at the bottom behind their seam.
func (m *Model) railLines(width, height int) []contentLine {
	var rows []contentLine
	// chrome lays lines that carry no row, so a click landing on one of
	// them picks nothing, unlike a line entryLines painted.
	chrome := func(lines ...contentLine) {
		rows = append(rows, lines...)
	}
	meters := m.railFootLines(width)
	listHeight := height
	if len(meters) > 0 {
		listHeight -= len(meters) + 1
	}
	if listHeight < 3 {
		listHeight, meters = height, nil
	}
	// A banner costs the list the rows it paints plus its padding, so each one
	// is only laid while entries still have room under it: a rail that is all
	// banner says nothing about the fleet.
	const railBannerRows, railListMin = 3, 3
	room := func(cost int) bool { return listHeight-len(rows)-cost >= railListMin }
	// Search heads the list it filters, so the query sits over the entries it
	// is narrowing. It is also the field being typed into, so a rail too tight
	// for the padded block keeps the bare field rather than dropping it.
	if m.rail.searching || m.rail.search != "" {
		field := contentLine{text: m.searchFieldLine(width)}
		if m.rail.searching {
			field.tone = searchFieldHex()
			field.text = paint(field.text, width, field.tone)
		}
		switch {
		case room(railBannerRows):
			chrome(contentLine{}, field, contentLine{})
		case room(1):
			chrome(field)
		}
	}
	// The list starts straight under the pane's top edge; the empty state
	// centers itself in the full list area instead. Every filter the rail is
	// under gets a badge here, since a narrowed list cannot show what it is
	// leaving out. A rail too tight for the padded block keeps the bare
	// badges, the way the search field does.
	if badges := m.filterBadgeLines(); len(badges) > 0 {
		lines := make([]contentLine, 0, len(badges))
		for _, badge := range badges {
			lines = append(lines, contentLine{text: badge})
		}
		switch {
		case room(len(lines) + 2):
			chrome(contentLine{})
			chrome(lines...)
			chrome(contentLine{})
		case room(len(lines)):
			chrome(lines...)
		}
	}
	rows = append(rows, m.entryLines(m.rail.rows, 0, width, max(listHeight-len(rows), 0))...)
	for len(rows) < listHeight {
		chrome(contentLine{})
	}
	rows = rows[:listHeight]
	if len(meters) > 0 {
		chrome(contentLine{rule: true})
		for _, line := range meters {
			chrome(contentLine{text: line})
		}
	}
	m.placeNoticeHit(len(meters), listHeight+1)
	return rows
}

// placeNoticeHit pins the card or badge's columns to the screen rows the
// foot took this frame: one edge cell sits left of the rail's content, and
// the foot starts under the rule that closes the list.
func (m *Model) placeNoticeHit(footLines, footIndex int) {
	if footLines == 0 || !m.notices.noticeHit.ok {
		m.notices.noticeHit = noticeHit{}
		return
	}
	y0, _ := m.bodyYRange()
	m.notices.noticeHit.x0++
	m.notices.noticeHit.x1++
	m.notices.noticeHit.y0 = y0 + footIndex
	m.notices.noticeHit.y1 = m.notices.noticeHit.y0 + footLines
}

// recordRailHits reads the row each rail line carries into m.railHits, so
// a click resolves against the frame the rail actually painted instead of
// re-deriving the layout in the mouse handler, where it would drift the
// first time the layout moved (#110). Called once per frame, off the final
// line slice, so whatever the frame prepended, truncated or padded is
// already accounted for.
func (m *Model) recordRailHits(lines []contentLine) {
	m.rail.railHits = m.rail.railHits[:0]
	for _, line := range lines {
		m.rail.railHits = append(m.rail.railHits, line.row-1)
	}
}

// filterBadgeLines is one badge per narrowing the rail is under, each next
// to the key that lifts it. Ordered widest to narrowest: the archive is a
// different fleet, the status filter hides sessions, hiding empty groups
// only hides scaffolding.
func (m *Model) filterBadgeLines() []string {
	var lines []string
	badge := func(label, key, action string) {
		lines = append(lines, strings.Repeat(" ", railInset)+scopeBadgeStyle.Render(label)+
			subtleStyle.Render("  ")+keyCap(key, action))
	}
	if m.rail.showArchived {
		badge("ARCHIVED", "t", "back to active")
	}
	if m.rail.statusFilter.active() {
		badge(strings.ToUpper(m.rail.statusFilter.label()), "w", "show all")
	}
	if m.rail.hideEmptyGroups && !m.rail.showArchived {
		badge("HIDE EMPTY", "e", "show empty")
	}
	return lines
}

// entryLines renders the visible slice of rows, which sit at offset in
// m.rows so the cursor and the tree guides still resolve against the whole
// list. Entries are two lines tall, so the window is measured in lines
// rather than rows. Each line carries the tone its entry painted, which the
// edge column matches.
func (m *Model) entryLines(rows []treeRow, offset, width, height int) []contentLine {
	m.rail.railWidth = width
	m.rail.handleX = map[string]int{}
	// Root alone is still an empty list: it says what the rail holds, not
	// what to do about it being empty.
	if rest := rowsBelowRoot(rows); len(rest) == 0 {
		m.rail.railTop, m.rail.railEnd = 0, len(rows)
		var lines []contentLine
		for i, entry := range rows {
			lines = append(lines, m.entryRowLines(entry, offset+i, width, panelHex())...)
		}
		for _, line := range m.emptyRailLines(width, height-len(lines)) {
			lines = append(lines, contentLine{text: line})
		}
		return lines
	}
	heights := make([]int, len(rows))
	for i := range heights {
		heights[i] = m.entryHeight(rows[i])
	}
	start, end := railWindow(heights, m.railAnchor()-offset, height, m.rail.railTop)
	m.rail.railTop, m.rail.railEnd = start, end

	var lines []contentLine
	for i := start; i < end; i++ {
		selected := offset+i == m.rail.cursor
		entry := rows[i]
		tone := panelHex()
		switch {
		case m.liftedRow(entry):
			tone = liftedHex()
		case m.dropRow(entry):
			tone = dropHex()
		case selected || m.renamingRow(entry):
			tone = selectedHex()
		}
		lines = append(lines, m.entryRowLines(entry, offset+i, width, tone)...)
	}
	// The window already held a row back for each counter, so the checks
	// below only catch an entry that painted taller than entryHeight said.
	// Each counter answers for the entry it is hiding, so clicking one
	// steps the window onto that entry instead of returning the same frame.
	spare := height - len(lines)
	if start > 0 && spare > 0 {
		counter := contentLine{
			text: subtleStyle.Render(strings.Repeat(" ", railInset) + fmt.Sprintf("↑ %d more", start)),
			row:  offset + start,
		}
		lines = append([]contentLine{counter}, lines...)
		spare--
	}
	if end < len(rows) && spare > 0 {
		lines = append(lines, contentLine{
			text: subtleStyle.Render(strings.Repeat(" ", railInset) + fmt.Sprintf("↓ %d more", len(rows)-end)),
			row:  offset + end + 1,
		})
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return lines
}

// entryHeight is how many lines an entry paints, in either layout: a
// compact session is one row wearing the reply inline, a comfortable one
// is three, name, the last prompt, the last reply, and a group is always
// one, having neither line to carry. A shell drops to two: nobody is
// prompting it, so the prompt line would only ever hold a dash.
func (m *Model) entryHeight(entry treeRow) int {
	if entry.isGroup {
		return 1
	}
	if m.prefs.comfortableRows {
		if m.isShell(entry.sess.Tool) {
			return 2
		}
		return 3
	}
	return 1
}

// railWindow is the slice of entries the rail paints, keeping the cursor's
// entry whole on screen while moving the top by as little as the step
// needs. top comes from the previous frame: a list of uneven rows has no
// stable window that a cursor position alone can name, so the one already
// on screen is the answer until the cursor walks off its edge.
func railWindow(heights []int, cursor, budget, top int) (int, int) {
	if len(heights) == 0 || budget <= 0 {
		return 0, 0
	}
	cursor = min(max(cursor, 0), len(heights)-1)
	total := 0
	for _, h := range heights {
		total += h
	}
	if total <= budget {
		return 0, len(heights)
	}
	top = min(max(top, 0), len(heights)-1)
	if cursor < top {
		top = cursor
	}
	for ; top <= cursor; top++ {
		if end := windowEnd(heights, top, budget); end > cursor {
			return top, end
		}
	}
	// Taller than the whole budget: paint it and let the caller crop.
	return cursor, cursor + 1
}

// windowEnd is where the entries starting at top stop fitting, counting
// the rows the "more" counters take at either edge.
func windowEnd(heights []int, top, budget int) int {
	room := budget
	if top > 0 {
		room--
	}
	end, used := top, 0
	for end < len(heights) {
		left := room
		if end+1 < len(heights) {
			left--
		}
		if used+heights[end] > left {
			break
		}
		used += heights[end]
		end++
	}
	return end
}

func (m *Model) emptyRailLines(width, height int) []string {
	title := "no sessions yet"
	hint := m.listHint(keybind.NewSession, "starts one")
	if m.rail.showArchived {
		title = "nothing archived"
		hint = m.listHint(keybind.Archived, "back to active")
	}
	if m.rail.statusFilter.active() {
		title = "nothing needs " + m.rail.statusFilter.label()
		hint = m.listHint(keybind.Filter, "show all")
	}
	if search := strings.TrimSpace(m.rail.search); search != "" {
		title = "no matches"
		hint = subtleStyle.Render("for \"" + search + "\"")
	}
	titleLine := centerLine(
		lipgloss.NewStyle().Bold(true).Foreground(colorBright).Render(title),
		width,
	)
	hintLine := centerLine(hint, width)
	block := []string{titleLine, "", hintLine}
	if height <= 0 {
		return block
	}
	if height < len(block) {
		return block[:height]
	}
	out := make([]string, height)
	start := (height - len(block)) / 2
	copy(out[start:], block)
	return out
}

// centerLine pads a styled string so its visible text sits in the middle
// of width columns.
func centerLine(s string, width int) string {
	if width <= 0 {
		return s
	}
	w := ansi.StringWidth(s)
	if w >= width {
		return ansi.Truncate(s, width, "…")
	}
	left := (width - w) / 2
	return strings.Repeat(" ", left) + s
}
