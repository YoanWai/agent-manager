package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/clipboard"
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/launch"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/sysstat"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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
	m.pane.columnX = leftWidth + 2
	railRows := m.railLines(railWidth, bodyHeight)
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
	if m.mode == modeFocus && m.pane.box.ok {
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
	return m.fullLayout && m.mode != modeFocus
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

// viewFullFocusFrame is a session opened from the full screen list: the
// captured pane owns the whole terminal body and the list waits behind
// it. The focus rules cap and close the pane the way they do in the
// split, stretched to the terminal's edges.
func (m *Model) viewFullFocusFrame() string {
	footer := m.viewFooter()
	bodyHeight := m.listBodyHeight()
	m.pane.columnX = 0
	frame := []string{}
	for _, line := range m.viewHeaderRows() {
		frame = append(frame, paint(line, m.width, backdropHex()))
	}
	frame = append(frame, paint(hrule(m.width), m.width, backdropHex()))
	frame = append(frame, paint(m.focusFactsLine(m.width), m.width, backdropHex()))
	frame = append(frame, paint(m.focusEdge(m.width), m.width, backdropHex()))
	m.previewBodyOffset = 0
	paneRows := m.previewLines(m.width, bodyHeight, strings.Repeat(" ", contentGutter))
	frame = append(frame, paintContent(paneRows, m.width, bodyHeight, backdropHex())...)
	frame = append(frame, paint(m.focusEdge(m.width), m.width, backdropHex()))
	for _, line := range splitLines(footer) {
		frame = append(frame, paint(line, m.width, backdropHex()))
	}
	return m.overlayTopRight(strings.Join(frame, "\n"), m.statusToast(), m.listChromeRows()+1)
}

// fullQuickLines docks the open quick bar at the full screen frame's
// foot, its tail kept when the frame runs out of rows, which is where
// the caret is. Empty while the bar is closed.
func (m *Model) fullQuickLines(width, height int) []contentLine {
	if !m.quick.active {
		return nil
	}
	gutter := strings.Repeat(" ", contentGutter)
	inner := width - 2*contentGutter
	if inner < 1 {
		inner = 1
	}
	inset := func(block []string) []contentLine {
		out := make([]contentLine, len(block))
		for i, line := range block {
			out[i] = contentLine{text: gutter + line}
		}
		return out
	}
	lines := append([]contentLine{{rule: true}}, inset(splitLines(m.viewQuickBar(inner)))...)
	if len(lines) > height {
		lines = lines[len(lines)-height:]
	}
	return lines
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
	if !m.searching {
		caret, hint = "", keyCapQuiet("esc", "clear")
	}
	chrome := railInset + ansi.StringWidth(glyph) + ansi.StringWidth(caret)

	if m.search == "" {
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
	query := m.search
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
	meters := m.railFootLines(width)
	listHeight := height
	if len(meters) > 0 {
		listHeight -= len(meters) + 1
	}
	if listHeight < 3 {
		listHeight, meters = height, nil
	}
	var rows []contentLine
	// A banner costs the list the rows it paints plus its padding, so each one
	// is only laid while entries still have room under it: a rail that is all
	// banner says nothing about the fleet.
	const railBannerRows, railListMin = 3, 3
	room := func(cost int) bool { return listHeight-len(rows)-cost >= railListMin }
	// Search heads the list it filters, so the query sits over the entries it
	// is narrowing. It is also the field being typed into, so a rail too tight
	// for the padded block keeps the bare field rather than dropping it.
	if m.searching || m.search != "" {
		field := contentLine{text: m.searchFieldLine(width)}
		switch {
		case room(railBannerRows):
			rows = append(rows, contentLine{}, field, contentLine{})
		case room(1):
			rows = append(rows, field)
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
			rows = append(rows, contentLine{})
			rows = append(rows, lines...)
			rows = append(rows, contentLine{})
		case room(len(lines)):
			rows = append(rows, lines...)
		}
	}
	rows = append(rows, m.entryLines(m.rows, 0, width, max(listHeight-len(rows), 0))...)
	for len(rows) < listHeight {
		rows = append(rows, contentLine{})
	}
	rows = rows[:listHeight]
	if len(meters) > 0 {
		rows = append(rows, contentLine{rule: true})
		for _, line := range meters {
			rows = append(rows, contentLine{text: line})
		}
	}
	return rows
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
	if m.showArchived {
		badge("ARCHIVED", "t", "back to active")
	}
	if m.statusFilter.active() {
		badge(strings.ToUpper(m.statusFilter.label()), "w", "show all")
	}
	if m.hideEmptyGroups {
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
	// Root alone is still an empty list: it says what the rail holds, not
	// what to do about it being empty.
	if rest := rowsBelowRoot(rows); len(rest) == 0 {
		var lines []contentLine
		for i, entry := range rows {
			for _, line := range splitLines(m.renderTreeRow(entry, m.cursor == offset+i, width, offset+i, panelHex())) {
				lines = append(lines, contentLine{text: line})
			}
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
	start, end := railWindow(heights, m.cursor-offset, height, m.railTop)
	m.railTop = start

	var lines []contentLine
	for i := start; i < end; i++ {
		selected := offset+i == m.cursor
		entry := rows[i]
		tone := panelHex()
		if selected || m.renamingRow(entry) {
			tone = selectedHex()
		}
		for _, line := range splitLines(m.renderTreeRow(entry, selected, width, offset+i, tone)) {
			lines = append(lines, contentLine{text: line, tone: tone})
		}
	}
	// The window already held a row back for each counter, so the checks
	// below only catch an entry that painted taller than entryHeight said.
	spare := height - len(lines)
	if start > 0 && spare > 0 {
		lines = append([]contentLine{{text: subtleStyle.Render(strings.Repeat(" ", railInset) + fmt.Sprintf("↑ %d more", start))}}, lines...)
		spare--
	}
	if end < len(rows) && spare > 0 {
		lines = append(lines, contentLine{text: subtleStyle.Render(strings.Repeat(" ", railInset) + fmt.Sprintf("↓ %d more", len(rows)-end))})
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
	if m.comfortableRows {
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
	if m.showArchived {
		title = "nothing archived"
		hint = m.listHint(keybind.Archived, "back to active")
	}
	if m.statusFilter.active() {
		title = "nothing needs " + m.statusFilter.label()
		hint = m.listHint(keybind.Filter, "show all")
	}
	if search := strings.TrimSpace(m.search); search != "" {
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

// treeGuidesAt is the ancestry trail left of a nested entry: a branch
// connector — ├─ mid-list, ╰─ for the last child — behind one guide per
// ancestor, so a group's children hang off its branch the way the legacy
// tree drew them. A slot goes quiet once its level has no further
// siblings below, which is what closes a branch off.
func (m *Model) treeGuidesAt(index int) string {
	if index < 0 || index >= len(m.rows) {
		return ""
	}
	depth := m.rows[index].depth
	if depth <= 0 {
		return ""
	}
	var guides strings.Builder
	for slot := 1; slot <= depth; slot++ {
		continues := m.slotContinues(index, slot)
		switch {
		case slot < depth && continues:
			guides.WriteString("│  ")
		case slot < depth:
			guides.WriteString("   ")
		case continues:
			guides.WriteString("├─ ")
		default:
			guides.WriteString("╰─ ")
		}
	}
	return subtleStyle.Render(guides.String())
}

// treeGuideTrail is the same ancestry read one line lower: every branch
// the entry's own row connected to carries straight down past its second
// line, so a two-line entry cannot leave a gap in the tree.
func (m *Model) treeGuideTrail(index int) string {
	if index < 0 || index >= len(m.rows) {
		return ""
	}
	depth := m.rows[index].depth
	if depth <= 0 {
		return ""
	}
	var guides strings.Builder
	for slot := 1; slot <= depth; slot++ {
		if m.slotContinues(index, slot) {
			guides.WriteString("│  ")
			continue
		}
		guides.WriteString("   ")
	}
	return subtleStyle.Render(guides.String())
}

// slotContinues reports whether the level named by slot has another entry
// below index. A slot goes quiet once its level has no further siblings,
// which is what closes a branch off.
func (m *Model) slotContinues(index, slot int) bool {
	for j := index + 1; j < len(m.rows); j++ {
		if m.rows[j].depth < slot {
			return false
		}
		if m.rows[j].depth == slot {
			return true
		}
	}
	return false
}

// renderTreeRow paints one entry: a status dot, the name, and what the
// entry is doing set against the row's far edge. The selected entry lifts
// onto its own band instead of wearing a marker.
func (m *Model) renderTreeRow(entry treeRow, selected bool, width, index int, bg string) string {
	pad := strings.Repeat(" ", railInset)
	guides := m.treeGuidesAt(index)
	trail := m.treeGuideTrail(index)

	if m.renamingRow(entry) {
		line := pad + guides + m.renameRowInput(entry, width-railGutter-ansi.StringWidth(guides))
		row := paint(line, width, selectedHex())
		for held := m.entryHeight(entry); held > 1; held-- {
			row += "\n" + paint(pad+trail, width, selectedHex())
		}
		return row
	}

	if entry.isGroup {
		return m.renderGroupEntry(entry, selected, width, pad, guides, trail, bg)
	}
	return m.renderSessionEntry(entry, selected, width, pad, guides, trail, bg)
}

// A shell takes a caret rather than an idle dot it would never leave, but
// a pane that has gone still has to say so.
func (m *Model) sessionGlyph(sess store.Session) string {
	if sess.Status == status.Starting {
		return lipgloss.NewStyle().Foreground(statusColor(status.Starting)).
			Render(startupFrames[m.startupPhase%len(startupFrames)])
	}
	resting := sess.Status != status.Dead && sess.Status != status.Errored
	if resting && m.isShell(sess.Tool) {
		return subtleStyle.Render(shellGlyph)
	}
	return lipgloss.NewStyle().Foreground(statusColor(sess.Status)).Render(statusGlyph(sess.Status))
}

// namePlaceholder stands in for the name a spawn generated while the agent
// it asked to name itself has not answered, so the row settles on one name
// instead of flashing a throwaway one first.
const namePlaceholder = "…"

// placeholderPromptWidth caps the prompt a waiting row borrows. It is what
// the narrowest rail affords beside a starting row's state, tool and age, so
// the row keeps the shape every other row has instead of pushing a column
// off its own end.
const placeholderPromptWidth = 12

// renameGrace caps the wait for that answer. It spans the whole way there,
// the boot, the directive reaching the agent, and the command it runs, so it
// is generous; past it the session keeps the name it was given.
const renameGrace = time.Minute

// awaitedRename is what a spawn launched with: the name generated for it,
// which it falls back to, and the prompt it was given, which says which task
// the row is while it has no name of its own.
type awaitedRename struct {
	generated string
	prompt    string
}

// awaitingRename drops the record it reads as soon as the wait is over, so
// a session settling on its name needs nothing to sweep the map after it.
func (m *Model) awaitingRename(sess store.Session) bool {
	awaited, ok := m.awaitedRenames[sess.ID]
	if !ok {
		return false
	}
	if sess.Name == awaited.generated && sess.Status != status.Dead &&
		time.Since(sess.CreatedAt) < renameGrace {
		return true
	}
	delete(m.awaitedRenames, sess.ID)
	return false
}

// displayName is what every reading of a session prints, so the rail row and
// the columns beside it never disagree about who an agent is.
func (m *Model) displayName(sess store.Session) string {
	if !m.awaitingRename(sess) {
		return sess.Name
	}
	// The stored LaunchPrompt is the decorated one, carrying the rename
	// directive the agent was sent; what the row wants is what was typed.
	if preview := promptPreview(m.awaitedRenames[sess.ID].prompt); preview != "" {
		return preview
	}
	return namePlaceholder
}

// promptPreview flattens a prompt into the one short line a row can wear as
// a name, so five agents spawned in a burst say which is which right away.
// A pasted image reaches the agent as the path it was written to, which
// would name every image-first spawn the same thing, so the pictures drop
// out of the preview and the words stay.
func promptPreview(prompt string) string {
	words := make([]string, 0, len(strings.Fields(prompt)))
	for _, word := range strings.Fields(prompt) {
		if clipboard.IsPastePath(word) {
			continue
		}
		words = append(words, word)
	}
	return ansi.Truncate(strings.Join(words, " "), placeholderPromptWidth, "…")
}

func (m *Model) renderSessionEntry(entry treeRow, selected bool, width int, pad, guides, trail, bg string) string {
	sess := entry.sess
	// An archived session's pane was killed on its way in; a status frozen
	// by an older build (a "working" from before the kill recorded dead)
	// must not read as alive from inside the archive.
	if sess.Archived {
		sess.Status = status.Dead
	}
	dot := m.sessionGlyph(sess)
	nameStyle := valueStyle
	if selected {
		nameStyle = lipgloss.NewStyle().Foreground(colorBright).Bold(true)
	}
	head := pad + guides + dot + " " + nameStyle.Render(m.displayName(sess))
	focused := selected && m.mode == modeFocus
	if focused {
		head += " " + focusBadgeStyle.Render(" FOCUS ")
	}
	if queued := m.queuedMessages[sess.ID]; queued > 0 {
		head += " " + inboxBadge(queued)
	}

	metaStyle := subtleStyle
	if selected {
		metaStyle = mutedStyle
	}
	// A session names its state in words as well as in its dot; a group,
	// whose row rolls several states together, is left to its dots.
	meta := lipgloss.NewStyle().Foreground(statusColor(sess.Status)).Render(statusLabel(sess.Status)) +
		metaStyle.Render(" · "+sess.Tool)
	// The id only fits the roomy layout; the compact meta is already crowded.
	if sess.AgentSessionID != "" && m.comfortableRows {
		meta += metaStyle.Render(" · " + sess.AgentSessionID)
	}
	meta += metaStyle.Render(" · " + relSince(lastActivity(sess)) + m.elsewhereNote(sess))

	if m.comfortableRows {
		return m.tallRow(sess, head, meta, metaIndent(pad, trail), selected, width, bg)
	}
	return m.compactRow(sess, head, meta, selected, width, bg)
}

// rowPromptFloor is the narrowest slot worth printing a prompt into: any
// tighter and the row shows an ellipsis where a task should be.
const rowPromptFloor = 8

// compactRow is the one-line session entry: the state picks the value
// riding between the name and the meta, the way Claude's agent view
// tells a fleet apart at a glance.
func (m *Model) compactRow(sess store.Session, head, meta string, selected bool, width int, bg string) string {
	quiet := subtleStyle
	if selected {
		quiet = mutedStyle
	}
	const gap = 2
	room := width - railGutter - ansi.StringWidth(head) - ansi.StringWidth(meta) - 2*gap
	if room >= rowPromptFloor {
		if cell := m.compactCell(sess, quiet, room); cell != "" {
			head += strings.Repeat(" ", gap) + cell
		}
	}
	return paint(rowColumns(head, meta, width-railGutter), width, bg)
}

// compactCell is what the one-line row quotes: the agent's last message
// whenever it has said anything — the question it waits on, its
// progress, its result — and the task it was given only while it has
// not. A session with neither says nothing rather than holding a dash
// mid-row.
func (m *Model) compactCell(sess store.Session, quiet lipgloss.Style, room int) string {
	if m.paneLines[sess.ID] != "" || sess.Status == status.Working {
		return m.replyCell(sess, quiet, room)
	}
	if prompt := oneLine(m.rowPrompt(sess)); prompt != "" {
		return rowGlyphStyle(current.Accent).Render("❯ ") +
			rowPromptStyle().Render(ansi.Truncate(prompt, max(room-2, 1), "…"))
	}
	return ""
}

// tallRow is the comfortable session entry: the name and meta alone on
// top, your last prompt under it, the agent's last reply under that. A
// shell keeps the name and its last output and skips the prompt line in
// between, having no one on the other side of it to quote.
func (m *Model) tallRow(sess store.Session, head, meta, indent string, selected bool, width int, bg string) string {
	quiet := subtleStyle
	if selected {
		quiet = mutedStyle
	}
	top := rowColumns(head, meta, width-railGutter)
	room := width - railGutter - ansi.StringWidth(indent) - 2
	lines := []string{paint(top, width, bg)}
	if !m.isShell(sess.Tool) {
		promptLine := indent + quiet.Render("-")
		if prompt := oneLine(m.rowPrompt(sess)); prompt != "" && room >= rowPromptFloor {
			promptLine = indent + rowGlyphStyle(current.Accent).Render("❯ ") +
				rowPromptStyle().Render(ansi.Truncate(prompt, room, "…"))
		}
		lines = append(lines, paint(promptLine, width, bg))
	}
	lines = append(lines, paint(indent+m.replyCell(sess, quiet, room+2), width, bg))
	return strings.Join(lines, "\n")
}

func rowGlyphStyle(hex string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(hex))
}

// rowReplyStyle washes the reply in its state's color at half strength;
// waiting stays at full strength because it needs the user.
func rowReplyStyle(state string) lipgloss.Style {
	if state == status.Waiting {
		return lipgloss.NewStyle().Foreground(statusColor(state))
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(mix(string(statusColor(state)), current.Subtle, 0.5)))
}

// rowPromptStyle leans the prompt toward the accent: visibly not chrome,
// visibly not the reply under it.
func rowPromptStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(mix(current.Accent, current.Dim, 0.5)))
}

// rowPrompt is the prompt a full screen row carries beside the name: the
// last one the session's own transcript echoes, whoever typed it and from
// wherever; else the last one delivered through the manager, else the one
// the session launched with, stripped of the notes launch prepends.
func (m *Model) rowPrompt(sess store.Session) string {
	if prompt := m.panePrompts[sess.ID]; prompt != "" {
		return prompt
	}
	if sess.LastPrompt != "" {
		return sess.LastPrompt
	}
	return typedPrompt(sess.LaunchPrompt)
}

// typedPrompt is a delivered prompt with the launch notes peeled off: the
// rename directives and the coordination note are the manager's words, not
// a task, and a note delivered on its own leaves nothing typed at all.
func typedPrompt(text string) string {
	if text == launch.DeferredRenameDirective || text == launch.CoordinationNote {
		return ""
	}
	text = strings.TrimPrefix(text, launch.CoordinationNote+"\n\n")
	text = strings.TrimPrefix(text, launch.RenameDirective+"\n\n")
	text = strings.TrimPrefix(text, launch.RenameAvailableNote+"\n\n")
	return text
}

// replyCell quotes the start of the agent's last message behind a static
// ↳, with the text washed in the state's hue so states read apart at a
// glance — the name's own dot already says the state, so the glyph does
// not repeat it; waiting keeps full strength because it needs the user.
// A working session with nothing quotable yet animates a loader, and a
// silent one holds the cell with a dim dash.
func (m *Model) replyCell(sess store.Session, quiet lipgloss.Style, room int) string {
	line := m.paneLines[sess.ID]
	if sess.Status == status.Working && line == "" {
		frame := startupFrames[m.startupPhase%len(startupFrames)]
		return lipgloss.NewStyle().Foreground(statusColor(status.Working)).Render(frame + " working")
	}
	if line == "" {
		return quiet.Render("-")
	}
	line = ansi.Truncate(line, max(room-2, 1), "…")
	return subtleStyle.Render("↳ ") + rowReplyStyle(sess.Status).Render(line)
}

// metaIndent lines a second row line up under the name on the first, past
// the entry's guides and the glyph column ahead of it.
func metaIndent(pad, trail string) string {
	return pad + trail + "  "
}

func (m *Model) renderGroupEntry(entry treeRow, selected bool, width int, pad, guides, trail, bg string) string {
	marker := "▾"
	if m.collapsed[entry.group] {
		marker = "▸"
	}
	nameStyle := lipgloss.NewStyle().Foreground(colorAccent2).Bold(true)
	if selected {
		nameStyle = nameStyle.Foreground(colorBright)
	}
	name := baseName(entry.group)
	if entry.isRoot() {
		// Nothing nests under root, so the marker is a blank that holds the column.
		marker, name = " ", "root"
		if !selected {
			nameStyle = nameStyle.Foreground(lipgloss.Color(mix(current.Accent2, current.Subtle, 0.5)))
		}
	}
	head := pad + guides + subtleStyle.Render(marker) + " " + nameStyle.Render(name)

	// What the group is doing rides on the same line as its name, so a
	// folded group still reports its subtree without being opened. It is
	// written in dots rather than words: the counts state the size too.
	meta := m.groupStatusGlyphs(entry.group)
	if meta == "" {
		meta = subtleStyle.Render("no agents yet")
	}

	return paint(rowColumns(head, meta, width-railGutter), width, bg)
}

// computerLines is the machine block docked at the rail's foot: a label
// and one thin meter per resource.
func (m *Model) computerLines(width int) []string {
	snap := m.snap
	pad := strings.Repeat(" ", railInset)
	barWidth := width - 22
	if barWidth < 4 {
		barWidth = 4
	}
	if barWidth > 10 {
		barWidth = 10
	}

	meter := func(label string, percent float64, ok bool, extra string) string {
		if !ok {
			return pad + labelStyle.Width(5).Render(label) + subtleStyle.Render("n/a")
		}
		line := pad + labelStyle.Width(5).Render(label) + gauge(percent, barWidth) +
			valueStyle.Render(fmt.Sprintf(" %3.0f%%", percent))
		if extra != "" {
			line += subtleStyle.Render(" " + extra)
		}
		return line
	}

	lines := []string{pad + subtleStyle.Render("computer")}
	lines = append(lines,
		meter("cpu", snap.CPUPercent, snap.CPUOK, ""),
		meter("mem", snap.MemPercent, snap.MemOK, humanBytes(snap.MemUsed)+"/"+humanBytes(snap.MemTotal)),
	)
	if snap.SwapOK && snap.SwapTotal > 0 {
		// Percent is used/total of the current swap allocation (macOS
		// grows the file under pressure; Linux uses the fixed swap size).
		lines = append(lines, meter("swap", snap.SwapPercent, true,
			humanBytes(snap.SwapUsed)+"/"+humanBytes(snap.SwapTotal)))
	}
	if snap.DiskOK {
		lines = append(lines, meter("disk", snap.DiskPercent, true,
			humanBytes(snap.DiskFree)+" free"))
	} else {
		lines = append(lines, meter("disk", 0, false, ""))
	}
	if temps := tempReadings(snap); temps != "" {
		lines = append(lines, pad+labelStyle.Width(5).Render("temp")+temps)
	}
	if m.net.rates {
		lines = append(lines, pad+labelStyle.Width(5).Render("net")+
			valueStyle.Render("↓ "+humanBytes(m.net.down)+"/s")+
			subtleStyle.Render("  ↑ "+humanBytes(m.net.up)+"/s"))
	}
	return append(lines, "")
}

func tempReadings(snap sysstat.Snapshot) string {
	var parts []string
	if snap.CPUTempOK {
		parts = append(parts, valueStyle.Render(fmt.Sprintf("cpu %.0f°C", snap.CPUTemp)))
	}
	if snap.GPUTempOK {
		parts = append(parts, valueStyle.Render(fmt.Sprintf("gpu %.0f°C", snap.GPUTemp)))
	}
	if snap.SoCTempOK {
		parts = append(parts, valueStyle.Render(fmt.Sprintf("soc %.0f°C", snap.SoCTemp)))
	}
	return strings.Join(parts, subtleStyle.Render("  "))
}

// contentLines is the right column: what the cursor is on, then its live
// pane, with the quick prompt docked at the foot when it is open. width is
// the whole column; our own blocks sit inside its gutters, while the
// captured pane spans it edge to edge.
func (m *Model) contentLines(width, height int) []contentLine {
	gutter := strings.Repeat(" ", contentGutter)
	inner := width - 2*contentGutter
	ours := func(lines []string) []contentLine {
		out := make([]contentLine, len(lines))
		for i, line := range lines {
			out[i] = contentLine{text: gutter + line}
		}
		return out
	}

	var bar []contentLine
	if m.quick.active {
		bar = append([]contentLine{{}}, ours(splitLines(m.viewQuickBar(inner)))...)
	}
	body := ours(splitLines(m.viewDetail(inner)))
	rest := height - len(body) - len(bar) - 1
	if rest >= 3 {
		if group, ok := m.selectedGroup(); ok {
			body = append(body, contentLine{rule: true})
			body = append(body, ours(splitLines(m.viewGroupAgents(group, inner, rest)))...)
		} else {
			separator := contentLine{rule: true}
			if m.mode == modeFocus {
				separator = contentLine{text: focusTopRule(width, m.keys), raw: true}
			}
			body = append(body, separator)
			m.previewBodyOffset = len(body)
			body = append(body, m.previewLines(width, rest, gutter)...)
		}
	}
	for len(body)+len(bar) < height {
		body = append(body, contentLine{})
	}
	return append(body[:max(height-len(bar), 0)], bar...)
}

// focusFactsLine says which session a full screen frame is showing, where
// nothing else on it does: the state dot and the name on the left, then
// where it runs and what it costs against the right edge, with the whole
// width to itself and a rule under it holding it off the pane. The keys
// the split's rule names are in the footer, so they stay there.
func (m *Model) focusFactsLine(width int) string {
	sess, ok := m.selected()
	if !ok {
		return ""
	}
	sep := subtleStyle.Render(" · ")
	left := " " + m.sessionGlyph(sess) + " " + valueStyle.Render(m.displayName(sess)) +
		sep + valueStyle.Render(sess.Tool) +
		sep + lipgloss.NewStyle().Foreground(statusColor(sess.Status)).Render(statusLabel(sess.Status)) +
		sep + subtleStyle.Render(relSince(lastActivity(sess)))
	// The facts give way one at a time as the terminal narrows, the least
	// telling first, so a tight line still carries what it has room for
	// rather than dropping the lot.
	facts := []focusFact{{text: valueStyle.Render(truncateTail(shortHome(sess.Cwd), focusFactsDirCap)), spare: 3}}
	if sess.WorktreeBranch != "" {
		facts = append(facts, focusFact{text: subtleStyle.Render("⑂ ") + valueStyle.Render(sess.WorktreeBranch), spare: 2})
	}
	if m.procFor == sess.ID && m.proc.OK {
		facts = append(facts,
			focusFact{text: labelStyle.Render("cpu ") + valueStyle.Render(fmt.Sprintf("%.1f%%", m.proc.CPUPercent)), spare: 1},
			focusFact{text: labelStyle.Render("ram ") + valueStyle.Render(humanBytes(m.proc.RSS)), spare: 1})
	}
	facts = append(facts, focusFact{text: labelStyle.Render("started ") + valueStyle.Render(relSince(sess.CreatedAt)), spare: 4})
	if queued := m.queuedMessages[sess.ID]; queued > 0 {
		facts = append(facts, focusFact{text: valueStyle.Render(fmt.Sprintf("%d queued", queued)), spare: 0})
	}

	right := joinFacts(facts, sep)
	for len(facts) > 0 && ansi.StringWidth(left)+focusFactsGap+ansi.StringWidth(right) > width {
		facts = dropSparest(facts)
		right = joinFacts(facts, sep)
	}
	if ansi.StringWidth(left) > width {
		return ansi.Truncate(left, max(width, 0), "…")
	}
	return rowColumns(left, right, width)
}

// focusFact is one reading on the full screen focus line, spare ranking
// how readily it gives up its room: the higher, the sooner it goes.
type focusFact struct {
	text  string
	spare int
}

func joinFacts(facts []focusFact, sep string) string {
	if len(facts) == 0 {
		return ""
	}
	parts := make([]string, len(facts))
	for i, fact := range facts {
		parts[i] = fact.text
	}
	return strings.Join(parts, sep) + " "
}

func dropSparest(facts []focusFact) []focusFact {
	sparest := 0
	for i, fact := range facts {
		if fact.spare >= facts[sparest].spare {
			sparest = i
		}
	}
	return append(facts[:sparest], facts[sparest+1:]...)
}

// focusEdge is the hairline holding the full screen pane off what sits
// above and below it, in the pane's own tone once it has a box to trace.
func (m *Model) focusEdge(width int) string {
	if m.pane.box.ok {
		return focusEdgeStyle.Render(strings.Repeat("─", max(width, 0)))
	}
	return hrule(width)
}

// focusFactsDirCap keeps a deep path from crowding the readings beside it,
// and focusFactsGap is the least space kept between the name and them.
const (
	focusFactsDirCap = 40
	focusFactsGap    = 2
)

// focusTopRule is the hairline that caps the focused pane in the split,
// where the detail head above it already names the session, so the rule
// spends its title on the keys instead.
func (m *Model) listHint(action, label string) string {
	glyph := m.listGlyph(action)
	if glyph == "" {
		return ""
	}
	return keyCap(glyph, label)
}

func focusTopRule(width int, keys keybind.Table) string {
	hints := []string{"focused", keys.Binding(keybind.Detach).Label() + " back"}
	if label := keys.Binding(keybind.Review).Label(); label != "" {
		hints = append(hints, label+" review")
	}
	if label := keys.Binding(keybind.Editor).Label(); label != "" {
		hints = append(hints, label+" editor")
	}
	title := " " + strings.Join(hints, " · ") + " "
	rule := annotationStyle.Render(title)
	rest := width - lipgloss.Width(title)
	if rest > 0 {
		rule += focusEdgeStyle.Render(strings.Repeat("─", rest))
	}
	return rule
}

var startupFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const startupRingPoints = 12

func (m *Model) startupLoader(width, height int) []string {
	sess, ok := m.selected()
	if !ok || m.mode == modeFocus || sess.Status != status.Starting || paneBooted(m.preview) {
		return nil
	}
	return ringLoader(width, height, "starting up", m.startupPhase)
}

func ringLoader(width, height int, label string, phase int) []string {
	accent := lipgloss.NewStyle().Foreground(statusColor(status.Starting)).Bold(true)
	glow := lipgloss.NewStyle().Foreground(statusColor(status.Starting))
	phase = phase % startupRingPoints
	dot := func(position int) string {
		switch position {
		case phase:
			return accent.Render("●")
		case (phase + startupRingPoints - 1) % startupRingPoints:
			return glow.Render("•")
		default:
			return subtleStyle.Render("·")
		}
	}

	block := []string{
		centerLine(dot(11)+"   "+dot(0)+"   "+dot(1), width),
		centerLine(dot(10)+"       "+dot(2), width),
		centerLine(dot(9)+"       "+dot(3), width),
		centerLine(dot(8)+"       "+dot(4), width),
		centerLine(dot(7)+"   "+dot(6)+"   "+dot(5), width),
		centerLine(valueStyle.Bold(true).Render(label), width),
	}
	if height <= len(block) {
		return block[:height]
	}
	lines := make([]string, height)
	copy(lines[(height-len(block))/2:], block)
	return lines
}

// previewLines is the captured pane, filling every row under the detail
// separator. The captured rows are marked raw and drawn without the
// column's gutters: painting our backdrop behind an agent's own CLI colors
// would replace the background it drew itself, and insetting its output
// would put a margin around a terminal that has its own.
func (m *Model) previewLines(width, height int, gutter string) []contentLine {
	var lines []contentLine
	loader := m.startupLoader(width, height)
	pane := paneExact(m.preview, height, width, m.paneCaretRow())
	if len(pane) == 0 {
		// No rows painted means nothing to hit-test: a box left over from
		// the previous session would catch clicks on empty space.
		m.pane.box = paneBox{}
		if loader != nil {
			for _, line := range loader {
				lines = append(lines, contentLine{text: previewLine(line, width), raw: true})
			}
			return lines
		}
		return append(lines, contentLine{text: gutter + mutedStyle.Render("(no output yet)")})
	}
	// Record where these rows land so mouse hit-testing reads the same
	// geometry the paint used.
	m.pane.box = paneBox{
		x:      m.paneOriginX(),
		y:      m.listChromeRows() + m.previewBodyOffset,
		width:  width,
		height: len(pane),
		ok:     true,
	}
	for i, line := range pane {
		if i < len(loader) && loader[i] != "" {
			lines = append(lines, contentLine{text: previewLine(loader[i], width), raw: true})
			continue
		}
		lines = append(lines, contentLine{text: m.renderPaneRow(i, line, width), raw: true})
	}
	// Rows past the capture stay raw too: a painted tail under unpainted
	// output would read as a box drawn around the agent's last line.
	for len(lines) < height {
		lines = append(lines, contentLine{raw: true})
	}
	return lines
}

// detailLabelWidth is the column every fact label in the content head is
// padded to, so the values under it line up as one column.
const detailLabelWidth = 7

// factRow is one line of a detail head: a quiet label, its value, and a
// reading set against the right edge. The value is rendered against the
// columns it actually gets, and the reading is dropped rather than pushed
// off the edge when the column is too narrow to hold both.
func factRow(label string, value func(room int) string, right string, width int) string {
	room := width - detailLabelWidth - ansi.StringWidth(right) - 2
	if room < 12 {
		right, room = "", width-detailLabelWidth
	}
	return rowColumns(labelStyle.Render(padRight(label, detailLabelWidth))+value(max(room, 1)), right, width)
}

// plainValue is a factRow value that is already short enough to stand as it
// is, for facts whose text does not vary with the terminal.
func plainValue(value string) func(int) string {
	return func(int) string { return value }
}

// trimmedValue is a factRow value cut to the columns it gets, for readings
// that grow with the fleet rather than with the terminal.
func trimmedValue(value string) func(int) string {
	return func(room int) string { return ansi.Truncate(value, room, "…") }
}

// fitColumns lays the richest pair of readings that fits: rights are tried
// from richest to plainest, and for each, the lefts in turn. Nothing fitting
// means the plainest left is trimmed to what the column has.
func fitColumns(lefts, rights []string, width int) string {
	for _, right := range rights {
		for _, left := range lefts {
			if ansi.StringWidth(left)+ansi.StringWidth(right)+2 <= width {
				return rowColumns(left, right, width)
			}
		}
	}
	// Nothing fits whole, so the plainest left is trimmed to keep the richest
	// reading that still leaves it something readable.
	last := lefts[len(lefts)-1]
	for _, right := range rights {
		if room := width - ansi.StringWidth(right) - 2; room >= 8 {
			return rowColumns(ansi.Truncate(last, room, "…"), right, width)
		}
	}
	return rowColumns(ansi.Truncate(last, max(width, 1), "…"), "", width)
}

// viewDetail heads the content column: the selected session's name, its
// state, and the facts that place it (group, directory, age, usage).
func (m *Model) viewDetail(width int) string {
	sess, ok := m.selected()
	if !ok {
		if group, isGroup := m.selectedGroup(); isGroup {
			return m.viewGroupDetail(group, width)
		}
		return mutedStyle.Render("Select a session to inspect it.")
	}
	tool := sess.Tool
	if m.mode == modeRename && !m.rename.isGroup && m.rename.sessID == sess.ID {
		if picked := m.renameTool(); picked != "" {
			tool = picked
		}
	}

	state := lipgloss.NewStyle().Foreground(statusColor(sess.Status)).
		Render(statusGlyph(sess.Status)+" "+statusLabel(sess.Status)) +
		subtleStyle.Render(" · "+relSince(lastActivity(sess))+m.elsewhereNote(sess))
	// The branch a worktree session lives on is the fact that tells it apart
	// from its siblings, so it rides beside the tool while the row has room,
	// and the tool chip goes before the name does.
	name := lipgloss.NewStyle().Foreground(colorBright).Bold(true).Render(m.displayName(sess))
	// Ahead of the name because fitColumns trims the head from its tail: the
	// rail still shows the name, while a trimmed badge leaves no trace that
	// anything is waiting.
	if queued := m.queuedMessages[sess.ID]; queued > 0 {
		name = inboxBadge(queued) + " " + name
	}
	withTool := name + "  " + chipStyle.Render(tool)
	heads := []string{withTool, name}
	if sess.WorktreeBranch != "" {
		heads = append([]string{withTool + " " + chipStyle.Render("⑂ "+sess.WorktreeBranch)}, heads...)
	}

	usage := ""
	if m.procFor == sess.ID && m.proc.OK {
		usage = labelStyle.Render("cpu ") + valueStyle.Render(fmt.Sprintf("%.1f%%", m.proc.CPUPercent)) +
			subtleStyle.Render(" · ") + labelStyle.Render("ram ") +
			valueStyle.Render(fmt.Sprintf("%.1f%%", m.proc.RamPercent)) +
			subtleStyle.Render(" · ") + valueStyle.Render(humanBytes(m.proc.RSS))
	}
	started := subtleStyle.Render("started " + relSince(sess.CreatedAt))
	group := lipgloss.NewStyle().Foreground(colorAccent2).Render(displayGroup(sess.Group))
	dir := func(room int) string { return mutedStyle.Render(truncateTail(sess.Cwd, room)) }
	return fitColumns(heads, []string{state}, width) + "\n" +
		factRow("group", plainValue(group), started, width) + "\n" +
		factRow("dir", dir, usage, width)
}

// viewGroupDetail heads the content column for a group: its name, how many
// agents sit under it, where they start, and what they are all doing.
func (m *Model) viewGroupDetail(group string, width int) string {
	count := m.groupSessionCount(group)
	countLabel := fmt.Sprintf("%d agents", count)
	if count == 1 {
		countLabel = "1 agent"
	}
	title := lipgloss.NewStyle().Foreground(colorAccent2).Bold(true).Render(displayGroup(group))
	head := fitColumns([]string{title + "  " + chipStyle.Render(countLabel), title}, []string{""}, width)

	if m.renamingGroup(group) {
		pathLabel := labelStyle
		if m.rename.focus == 1 {
			pathLabel = lipgloss.NewStyle().Foreground(colorAccent)
		}
		worktreeLabel := labelStyle
		if m.rename.focus == 2 {
			worktreeLabel = lipgloss.NewStyle().Foreground(colorAccent)
		}
		if fieldWidth := width - 12; fieldWidth >= 10 {
			m.rename.dir.Width = fieldWidth
		}
		out := head + "\n" + pathLabel.Width(10).Render("path") + textInputView(m.rename.dir)
		if m.rename.focus == 1 && m.pathSugg.active() {
			out += "\n" + m.viewPathSuggestions()
		}
		out += "\n" + worktreeLabel.Width(10).Render("worktree") +
			subtleStyle.Render("◂ ") + valueStyle.Render(groupWorktreeOptions[m.rename.worktreeIndex]) + subtleStyle.Render(" ▸")
		return out
	}

	path := m.groupPaths[group]
	source := ""
	if path == "" {
		path = m.groupDefaultDir(group)
		source = subtleStyle.Render("inherited")
	}
	dir := func(room int) string { return mutedStyle.Render(truncateTail(path, room)) }
	lines := []string{head, factRow("dir", dir, source, width)}
	if breakdown := m.groupStatusBreakdown(group); breakdown != "" {
		// The key that spawns into a group is the one people miss, since the
		// same key answers a session, so the group says what it does here.
		// The breakdown is the reading, so a column too tight for both keeps it.
		hint := m.listHint(keybind.Prompt, "new agent")
		if detailLabelWidth+ansi.StringWidth(breakdown)+ansi.StringWidth(hint)+2 > width {
			hint = ""
		}
		lines = append(lines, factRow("state", trimmedValue(breakdown), hint, width))
	}
	return strings.Join(lines, "\n")
}

// rosterToolColumn is the column a roster's tool names start at, so the
// roster reads as a table rather than as ragged pairs, and rosterNameMin
// the width under which a name column stops being worth reading.
const (
	rosterToolColumn = 14
	rosterNameMin    = 8
)

// viewGroupAgents lists a group's sessions where a session's pane preview
// would sit, so a group reads as a roster: one row per agent, its name, the
// CLI running it, and what it is doing.
func (m *Model) viewGroupAgents(group string, width, height int) string {
	total := m.groupSessionCount(group)
	if total == 0 {
		none := "(none yet)"
		if prompt := m.listGlyph(keybind.Prompt); prompt != "" {
			none = "(none yet, press " + prompt + " to spawn one)"
		}
		return subtleStyle.Render("agents") + "\n" + mutedStyle.Render(none)
	}

	type rosterRow struct{ name, tool, state string }
	var rows []rosterRow
	overflow := ""
	shown := 0
	for _, sess := range m.listedAgents() {
		if !inGroupSubtree(sess.Group, group) {
			continue
		}
		if shown >= height-2 && total > shown+1 {
			overflow = subtleStyle.Render(fmt.Sprintf("… %d more", total-shown))
			break
		}
		tint := lipgloss.NewStyle().Foreground(statusColor(sess.Status))
		rows = append(rows, rosterRow{
			name:  tint.Render(statusGlyph(sess.Status)) + " " + valueStyle.Render(m.displayName(sess)),
			tool:  subtleStyle.Render(sess.Tool),
			state: tint.Render(statusLabel(sess.Status)) + subtleStyle.Render(" · "+relSince(lastActivity(sess))),
		})
		shown++
	}

	// The name column is as wide as the longest name allows, bounded by what
	// the tool and state columns need, so tools land on one column and the
	// states share a right edge. A column too narrow for all three gives up
	// the tool first and the state second: a roster of names still answers
	// "who is in this group".
	nameWidth, toolWidth, stateWidth := rosterToolColumn, 0, 0
	for _, row := range rows {
		if w := ansi.StringWidth(row.name) + 2; w > nameWidth {
			nameWidth = w
		}
		if w := ansi.StringWidth(row.tool); w > toolWidth {
			toolWidth = w
		}
		if w := ansi.StringWidth(row.state); w > stateWidth {
			stateWidth = w
		}
	}
	showTool, showState := true, true
	if width-toolWidth-stateWidth-3 < rosterNameMin {
		showTool, toolWidth = false, 0
	}
	if width-stateWidth-2 < rosterNameMin {
		showState, stateWidth = false, 0
	}
	if room := width - toolWidth - stateWidth - 3; nameWidth > room {
		nameWidth = max(room, rosterNameMin)
	}

	head := padRight(subtleStyle.Render("agent"), nameWidth)
	if showTool {
		head += subtleStyle.Render("tool")
	}
	activity := ""
	if showState {
		activity = subtleStyle.Render("last activity")
	}
	lines := []string{rowColumns(head, activity, width)}
	for _, row := range rows {
		// A name trimmed to the column exactly would touch the tool beside
		// it, so the trim leaves the column's last cell as the gap.
		line := padRight(ansi.Truncate(row.name, max(nameWidth-1, 1), "…"), nameWidth)
		if showTool {
			line += row.tool
		}
		state := row.state
		if !showState {
			state = ""
		}
		lines = append(lines, rowColumns(line, state, width))
	}
	if overflow != "" {
		lines = append(lines, overflow)
	}
	return strings.Join(lines, "\n")
}

// lastActivity is when a session last changed state: the agent answering,
// finishing, erroring, or the moment a prompt set it working. It is what
// "how long since anything happened here" means to someone scanning the
// rail, where uptime says nothing about whether an agent is stuck.
func lastActivity(sess store.Session) time.Time {
	if sess.LastStatusAt.IsZero() {
		return sess.CreatedAt
	}
	return sess.LastStatusAt
}

// viewQuickBar is the docked prompt: enter answers the selected session, or
// spawns a fresh agent when a group is selected.
func (m *Model) viewQuickBar(width int) string {
	label := func(text string) string { return labelStyle.Render(padRight(text, detailLabelWidth)) }
	target := rowColumns(label("target")+mutedStyle.Render("no selection"), "", width)
	if entry, ok := m.selectedRow(); ok {
		if entry.isGroup {
			// Spawning: the tool and the worktree choice decide what gets
			// created, so they sit where the eye lands before typing.
			worktree := subtleStyle.Render("worktree off")
			switch {
			case !m.worktreeCapable(m.quickTargetDir()):
				worktree = subtleStyle.Render("worktree " + worktreeUnavailable)
			case m.quickWorktreeOn():
				worktree = lipgloss.NewStyle().Foreground(colorAccent2).Render("worktree on")
			}
			tool := chipStyle.Render(m.quickTool())
			target = fitColumns(
				[]string{label("new") + lipgloss.NewStyle().Foreground(colorAccent2).Render(displayGroup(entry.group))},
				[]string{tool + " " + worktree, tool, ""}, width)
		} else {
			sess := entry.sess
			state := lipgloss.NewStyle().Foreground(statusColor(sess.Status)).
				Render(statusGlyph(sess.Status) + " " + statusLabel(sess.Status))
			target = fitColumns(
				[]string{label("answer") + lipgloss.NewStyle().Foreground(colorBright).Bold(true).Render(m.displayName(sess))},
				[]string{state + " " + chipStyle.Render(sess.Tool), state, ""}, width)
		}
	}
	m.quick.input.SetWidth(width)
	m.quick.input.SetHeight(m.quickBarRows(width - 2))
	// Chips are tokens inside the typed text, so they wrap and reflow with
	// the words around them; painting happens on the rendered prompt.
	return target + "\n" + m.quick.renderChips(textAreaView(m.quick.input))
}

// viewHeaderRows is the full-width band over both columns: the wordmark
// on the left, and the richest reading of the fleet that fits set against
// the right edge (scope and rollup, then a compact rollup, then the scope
// alone).
func (m *Model) viewHeaderRows() []string {
	if m.headerRows() == 0 {
		return nil
	}
	left := m.viewBanner()[0]
	if m.update.latest != "" {
		left += subtleStyle.Render("  ") +
			lipgloss.NewStyle().Foreground(colorAccent).Render("↑ "+m.update.latest+" available")
	}
	sep := subtleStyle.Render("   ")
	scope := m.headerScope()
	agents := m.headerAgents()
	for _, right := range []string{
		joinHeaderPieces(sep, scope, m.viewStatusCounts(false), agents),
		joinHeaderPieces(sep, scope, m.viewStatusCounts(true), agents),
		joinHeaderPieces(sep, scope, m.viewStatusCounts(true), ""),
		scope,
		"",
	} {
		gap := m.width - railGutter - ansi.StringWidth(left) - ansi.StringWidth(right)
		if right == "" || gap < 2 {
			continue
		}
		return []string{left + strings.Repeat(" ", gap) + right + strings.Repeat(" ", railGutter)}
	}
	return []string{left}
}

// joinHeaderPieces joins the header's non-empty readings with a separator.
func joinHeaderPieces(sep string, pieces ...string) string {
	kept := pieces[:0:0]
	for _, piece := range pieces {
		if piece != "" {
			kept = append(kept, piece)
		}
	}
	return strings.Join(kept, sep)
}

// headerScope names what the list is showing. The count is of the same
// agents the rollup beside it breaks down, so the two lines always add up;
// counting painted rows instead would drop everything folded inside a
// collapsed group. Shells are left to their own block.
func (m *Model) headerScope() string {
	count := len(m.listedAgents())
	label := " agents"
	if count == 1 {
		label = " agent"
	}
	scope := subtleStyle.Render(" · active")
	if m.showArchived {
		scope = subtleStyle.Render(" · archived")
	}
	return valueStyle.Render(fmt.Sprintf("%d", count)) + subtleStyle.Render(label) + scope
}

// headerAgents is the fleet's process cost as shares of this machine,
// empty when nothing is running. RAM shows both percent and absolute size.
func (m *Model) headerAgents() string {
	if m.agents.count == 0 {
		return ""
	}
	title := lipgloss.NewStyle().Foreground(colorBright).Bold(true).Render("agents total usage:")
	return title + " " +
		labelStyle.Render("cpu ") + valueStyle.Render(fmt.Sprintf("%.0f%%", m.agents.cpu)) +
		subtleStyle.Render(" · ") + labelStyle.Render("ram ") +
		valueStyle.Render(fmt.Sprintf("%.0f%%", m.agents.ram)) +
		subtleStyle.Render(" · ") + valueStyle.Render(humanBytes(m.agents.rss))
}

// elsewhereNote marks a session this manager does not speak for: its pane
// runs on a tmux server this one does not talk to, or no server has claimed
// it and the store belongs to another manager. Its status is the last one
// that manager wrote, and nothing here refreshes or drives it.
func (m *Model) elsewhereNote(sess store.Session) string {
	if m.tmuxSocket == "" {
		return ""
	}
	if sess.TmuxSocket == "" && !m.leadingManager {
		return " · elsewhere"
	}
	if sess.TmuxSocket == "" || sess.TmuxSocket == m.tmuxSocket {
		return ""
	}
	return " · elsewhere"
}

// viewStatusCounts is the fleet-at-a-glance strip: a tinted dot and count
// per state present among the listed agents.
func (m *Model) viewStatusCounts(compact bool) string {
	counts := map[string]int{}
	for _, sess := range m.listedAgents() {
		counts[sess.Status]++
	}
	var parts []string
	for _, st := range []string{status.Waiting, status.Working, status.Finished, status.Idle, status.Errored, status.Dead} {
		if counts[st] == 0 {
			continue
		}
		dot := lipgloss.NewStyle().Foreground(statusColor(st)).Render(statusGlyph(st))
		label := fmt.Sprintf(" %d %s", counts[st], st)
		if compact {
			label = fmt.Sprintf(" %d", counts[st])
		}
		parts = append(parts, dot+subtleStyle.Render(label))
	}
	return strings.Join(parts, subtleStyle.Render("  "))
}
