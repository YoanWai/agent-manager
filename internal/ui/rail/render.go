package rail

import (
	"fmt"
	"strings"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var startupFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type renderer struct {
	model    Model
	ctx      RenderContext
	palette  palette
	previous Frame
	frame    Frame
}

func (m Model) Render(ctx RenderContext, previous Frame) Frame {
	r := renderer{
		model:    m,
		ctx:      ctx,
		palette:  newPalette(ctx.Theme),
		previous: previous,
		frame: Frame{
			Width:   ctx.Width,
			Handles: map[string]int{},
			visible: map[string]bool{},
		},
	}
	r.frame.Lines = r.railLines()
	r.prepareMenu()
	return r.frame
}

func (r *renderer) railLines() []Line {
	var lines []Line
	listHeight := r.ctx.Height
	if len(r.ctx.Foot) > 0 {
		listHeight -= len(r.ctx.Foot) + 1
	}
	if listHeight < 3 {
		listHeight = r.ctx.Height
	}
	const bannerRows, listMinimum = 3, 3
	room := func(cost int) bool { return listHeight-len(lines)-cost >= listMinimum }
	if r.model.searching || r.model.search != "" {
		field := Line{Text: r.searchFieldLine()}
		if r.model.searching {
			field.Tone = r.palette.searchField()
			field.Text = paint(field.Text, r.ctx.Width, field.Tone)
		}
		switch {
		case room(bannerRows):
			lines = append(lines, Line{}, field, Line{})
		case room(1):
			lines = append(lines, field)
		}
	}
	if badges := r.filterBadgeLines(); len(badges) > 0 {
		badgeLines := make([]Line, len(badges))
		for i, badge := range badges {
			badgeLines[i].Text = badge
		}
		switch {
		case room(len(badgeLines) + 2):
			lines = append(lines, Line{})
			lines = append(lines, badgeLines...)
			lines = append(lines, Line{})
		case room(len(badgeLines)):
			lines = append(lines, badgeLines...)
		}
	}
	lines = append(lines, r.entryLines(max(listHeight-len(lines), 0))...)
	for len(lines) < listHeight {
		lines = append(lines, Line{})
	}
	lines = lines[:listHeight]
	if listHeight != r.ctx.Height && len(r.ctx.Foot) > 0 {
		lines = append(lines, Line{Rule: true})
		r.frame.FootStart = len(lines)
		r.frame.FootLines = len(r.ctx.Foot)
		lines = append(lines, append([]Line(nil), r.ctx.Foot...)...)
	}
	return lines
}

func (r renderer) searchFieldLine() string {
	indent := strings.Repeat(" ", railInset)
	glyph := r.palette.key.Render("⌕ ")
	caret := r.ctx.CursorMarker + lipgloss.NewStyle().Foreground(lipgloss.Color(r.ctx.Theme.Accent)).Render("▏")
	hint := r.palette.keyCap("esc", "close")
	if !r.model.searching {
		caret, hint = "", r.palette.keyCap("esc", "clear")
	}
	chrome := railInset + ansi.StringWidth(glyph) + ansi.StringWidth(caret)
	if r.model.search == "" {
		field := glyph + r.palette.subtle.Render("type to filter") + caret
		if gap := r.ctx.Width - railInset - ansi.StringWidth(field) - ansi.StringWidth(hint) - 1; gap >= 2 {
			return indent + field + strings.Repeat(" ", gap) + hint
		}
		return indent + field
	}
	room := r.ctx.Width - chrome - ansi.StringWidth(hint) - 2
	if room < 8 {
		hint, room = "", r.ctx.Width-chrome
	}
	query := r.model.search
	if ansi.StringWidth(query) > room {
		runes := []rune(query)
		query = "…" + string(runes[len(runes)-max(room-1, 1):])
	}
	field := glyph + r.palette.value.Render(query) + caret
	if hint == "" {
		return indent + field
	}
	gap := r.ctx.Width - railInset - ansi.StringWidth(field) - ansi.StringWidth(hint) - 1
	return indent + field + strings.Repeat(" ", max(gap, 1)) + hint
}

func (r renderer) filterBadgeLines() []string {
	var lines []string
	badge := func(label, key, action string) {
		lines = append(lines, strings.Repeat(" ", railInset)+r.palette.scopeBadge.Render(label)+
			r.palette.subtle.Render("  ")+r.palette.keyCap(key, action))
	}
	if r.model.showArchived {
		badge("ARCHIVED", "t", "back to active")
	}
	if r.model.filter != statusFilterAll {
		badge("ATTENTION", "w", "show all")
	}
	if r.model.hideEmptyGroups && !r.model.showArchived {
		badge("HIDE EMPTY", "e", "show empty")
	}
	return lines
}

func (r *renderer) entryLines(height int) []Line {
	rows := r.model.rows
	if len(rows) <= 1 && len(rows) > 0 && rows[0].isRoot() {
		r.frame.Window = Range{Start: 0, End: len(rows)}
		var lines []Line
		for i, row := range rows {
			lines = append(lines, r.entryRowLines(row, i, r.palette.panel())...)
		}
		for _, line := range r.emptyLines(height - len(lines)) {
			lines = append(lines, Line{Text: line})
		}
		return lines
	}
	heights := make([]int, len(rows))
	for i, row := range rows {
		heights[i] = r.entryHeight(row)
	}
	anchor := r.model.cursor
	if r.model.reorder.active && r.model.reorder.autoscroll.anchored {
		anchor = r.model.reorder.autoscroll.anchor
	}
	start, end := railWindow(heights, anchor, height, r.previous.Window.Start)
	r.frame.Window = Range{Start: start, End: end}
	for i := start; i < end; i++ {
		r.frame.visible[rows[i].key()] = true
	}
	var lines []Line
	for i := start; i < end; i++ {
		row := rows[i]
		tone := r.palette.panel()
		switch {
		case r.model.reorder.active && row.key() == r.model.reorder.key:
			tone = r.palette.lifted()
		case r.model.reorder.drop != nil && row.key() == r.model.reorder.drop.key:
			tone = r.palette.drop()
		case i == r.model.cursor || r.renaming(row):
			tone = r.palette.selected()
		}
		lines = append(lines, r.entryRowLines(row, i, tone)...)
	}
	spare := height - len(lines)
	if start > 0 && spare > 0 {
		lines = append([]Line{{
			Text:  r.palette.subtle.Render(strings.Repeat(" ", railInset) + fmt.Sprintf("↑ %d more", start)),
			Hit:   rows[start].selection(),
			HitOK: true,
		}}, lines...)
		spare--
	}
	if end < len(rows) && spare > 0 {
		lines = append(lines, Line{
			Text:  r.palette.subtle.Render(strings.Repeat(" ", railInset) + fmt.Sprintf("↓ %d more", len(rows)-end)),
			Hit:   rows[end].selection(),
			HitOK: true,
		})
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return lines
}

func (r renderer) entryHeight(row treeRow) int {
	if row.kind != SessionRow || !r.ctx.ComfortableRows {
		return 1
	}
	if row.sess.IsShell {
		return 2
	}
	return 3
}

func railWindow(heights []int, cursor, budget, top int) (int, int) {
	if len(heights) == 0 || budget <= 0 {
		return 0, 0
	}
	cursor = min(max(cursor, 0), len(heights)-1)
	total := 0
	for _, height := range heights {
		total += height
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
	return cursor, cursor + 1
}

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

func (r renderer) emptyLines(height int) []string {
	title := "no sessions yet"
	hint := r.listHint(keybind.NewSession, "starts one")
	if r.model.showArchived {
		title, hint = "nothing archived", r.listHint(keybind.Archived, "back to active")
	}
	if r.model.filter != statusFilterAll {
		title, hint = "nothing needs attention", r.listHint(keybind.Filter, "show all")
	}
	if query := strings.TrimSpace(r.model.search); query != "" {
		title, hint = "no matches", r.palette.subtle.Render("for \""+query+"\"")
	}
	block := []string{
		centerLine(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(r.ctx.Theme.Bright)).Render(title), r.ctx.Width),
		"",
		centerLine(hint, r.ctx.Width),
	}
	if height <= 0 {
		return block
	}
	if height < len(block) {
		return block[:height]
	}
	out := make([]string, height)
	copy(out[(height-len(block))/2:], block)
	return out
}

func (r renderer) listHint(action, label string) string {
	glyph := r.ctx.ListKeys.Binding(action).Glyph("/")
	if glyph == "" {
		return ""
	}
	return r.palette.keyCap(glyph, label)
}

func (r *renderer) entryRowLines(row treeRow, index int, tone string) []Line {
	selected := index == r.model.cursor
	button := !r.renaming(row) && !r.ctx.MouseDisabled
	width := r.ctx.Width
	if button {
		width -= menuButtonWidth
	}
	var lines []Line
	for lineIndex, text := range splitLines(r.renderTreeRow(row, selected, width, index, tone)) {
		if lineIndex == 0 {
			plain := ansi.Strip(text)
			if grip := strings.Index(plain, reorderGrip); grip >= 0 {
				r.frame.Handles[row.key()] = 1 + ansi.StringWidth(plain[:grip])
			}
		}
		if button && lineIndex == 0 {
			text += r.menuButton(selected, tone)
		} else if button {
			text += paint("", menuButtonWidth, tone)
		}
		lines = append(lines, Line{Text: text, Tone: tone, Hit: row.selection(), HitOK: true})
	}
	return lines
}

func (r renderer) menuButton(selected bool, background string) string {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(r.palette.restingMark()))
	if selected {
		style = r.palette.key
	}
	return paint(" "+style.Render(MenuGlyph), menuButtonWidth, background)
}

func (r renderer) renderTreeRow(row treeRow, selected bool, width, index int, background string) string {
	pad := strings.Repeat(" ", railInset)
	guides := r.treeGuidesAt(index)
	trail := r.treeGuideTrail(index)
	if r.renaming(row) {
		lead := r.palette.subtle.Render("▾")
		if row.kind == SessionRow {
			lead = r.sessionGlyph(row.sess)
		}
		text := pad + guides + lead + " " + r.ctx.Rename.View
		line := paint(text, width, r.palette.selected())
		for held := r.entryHeight(row); held > 1; held-- {
			line += "\n" + paint(pad+trail, width, r.palette.selected())
		}
		return line
	}
	switch row.kind {
	case ConnectionRow:
		return r.renderConnection(row, selected, width, pad, background)
	case GroupRow:
		return r.renderGroup(row, selected, width, pad, guides, background)
	}
	return r.renderSession(row, selected, width, pad, guides, trail, background)
}

func (r renderer) renaming(row treeRow) bool {
	return r.ctx.Rename.View != "" && r.ctx.Rename.Target == row.selection()
}

func (r renderer) treeGuidesAt(index int) string {
	if index < 0 || index >= len(r.model.rows) || r.model.rows[index].depth <= 0 {
		return ""
	}
	depth := r.model.rows[index].depth
	var guides strings.Builder
	for slot := 1; slot <= depth; slot++ {
		continues := r.slotContinues(index, slot)
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
	return r.palette.subtle.Render(guides.String())
}

func (r renderer) treeGuideTrail(index int) string {
	if index < 0 || index >= len(r.model.rows) || r.model.rows[index].depth <= 0 {
		return ""
	}
	var guides strings.Builder
	for slot := 1; slot <= r.model.rows[index].depth; slot++ {
		if r.slotContinues(index, slot) {
			guides.WriteString("│  ")
		} else {
			guides.WriteString("   ")
		}
	}
	return r.palette.subtle.Render(guides.String())
}

func (r renderer) slotContinues(index, slot int) bool {
	for next := index + 1; next < len(r.model.rows); next++ {
		if r.model.rows[next].depth < slot {
			return false
		}
		if r.model.rows[next].depth == slot {
			return true
		}
	}
	return false
}

func (r renderer) rowHandle(row treeRow, selected bool) string {
	if row.isRoot() || row.host != "" || r.renaming(row) || r.ctx.MouseDisabled {
		return ""
	}
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(r.palette.restingMark()))
	if selected {
		style = lipgloss.NewStyle().Foreground(lipgloss.Color(r.ctx.Theme.Dim))
	}
	if r.model.reorder.active && row.key() == r.model.reorder.key {
		style = lipgloss.NewStyle().Foreground(lipgloss.Color(r.ctx.Theme.Accent)).Bold(true)
	}
	return style.Render(reorderGrip) + " "
}

func (r renderer) sessionGlyph(session Session) string {
	if session.Status == "starting" {
		return lipgloss.NewStyle().Foreground(r.palette.statusColor(session.Status)).
			Render(startupFrames[r.ctx.StartupPhase%len(startupFrames)])
	}
	if session.IsShell && session.Status != "dead" && session.Status != "errored" {
		return r.palette.subtle.Render(shellGlyph)
	}
	return lipgloss.NewStyle().Foreground(r.palette.statusColor(session.Status)).Render(statusGlyph(session.Status))
}

func (r renderer) renderSession(row treeRow, selected bool, width int, pad, guides, trail, background string) string {
	session := row.sess
	if session.Archived {
		session.Status = "dead"
	}
	nameStyle := r.palette.value
	if selected {
		nameStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(r.ctx.Theme.Bright)).Bold(true)
	}
	lead := pad + guides + r.sessionGlyph(session) + " "
	handle := r.rowHandle(row, selected)
	focus := ""
	if selected && r.ctx.Focused {
		focus = " " + r.palette.focusBadge.Render(" FOCUS ")
	}
	badges := ""
	if session.Queued > 0 {
		badges = " " + r.palette.inbox(session.Queued)
	}
	if session.AfterTurn != "" {
		badges += " " + r.palette.inboxBadge.Render(session.AfterTurnGlyph)
	}
	// A connection's rows carry whatever its host sent.
	name := presentation.EscapeControlsInline(sessionName(session))
	room := width - railGutter - ansi.StringWidth(lead+handle+focus+badges)
	if room <= 0 {
		room += ansi.StringWidth(focus)
		focus = ""
	}
	if ansi.StringWidth(name) > room {
		name = ansi.Truncate(name, max(room, 0), "…")
	}
	head := lead + handle + r.palette.highlight(name, r.model.search, nameStyle) + focus + badges
	metaStyle := r.palette.subtle
	if selected {
		metaStyle = r.palette.muted
	}
	meta := lipgloss.NewStyle().Foreground(r.palette.statusColor(session.Status)).Render(presentation.EscapeControlsInline(statusLabel(session.Status))) +
		metaStyle.Render(" · "+presentation.EscapeControlsInline(session.Tool))
	if session.AgentSessionID != "" && r.ctx.ComfortableRows {
		meta += metaStyle.Render(" · " + session.AgentSessionID)
	}
	activity := session.LastStatusAt
	if activity.IsZero() {
		activity = session.CreatedAt
	}
	elsewhere := ""
	if session.Elsewhere {
		elsewhere = " · elsewhere"
	}
	// A remote snapshot carries no times.
	if !activity.IsZero() {
		meta += metaStyle.Render(" · " + relSince(activity) + elsewhere)
	}
	if r.ctx.ComfortableRows {
		indent := pad + trail + "  " + strings.Repeat(" ", ansi.StringWidth(handle))
		return r.tallRow(session, head, meta, indent, selected, width, background)
	}
	return r.compactRow(session, head, meta, selected, width, background)
}

func (r renderer) compactRow(session Session, head, meta string, selected bool, width int, background string) string {
	quiet := r.palette.subtle
	if selected {
		quiet = r.palette.muted
	}
	const gap = 2
	room := width - railGutter - ansi.StringWidth(head) - ansi.StringWidth(meta) - 2*gap
	if room >= 8 {
		if cell := r.compactCell(session, quiet, room); cell != "" {
			head += strings.Repeat(" ", gap) + cell
		}
	}
	return paint(rowColumns(head, meta, width-railGutter), width, background)
}

func (r renderer) compactCell(session Session, quiet lipgloss.Style, room int) string {
	if session.PaneLine != "" || session.Status == "working" {
		return r.replyCell(session, quiet, room)
	}
	if prompt := oneLine(session.Prompt); prompt != "" {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(r.ctx.Theme.Accent)).Render("❯ ") +
			lipgloss.NewStyle().Foreground(lipgloss.Color(mix(r.ctx.Theme.Accent, r.ctx.Theme.Dim, 0.5))).
				Render(ansi.Truncate(prompt, max(room-2, 1), "…"))
	}
	return ""
}

func (r renderer) tallRow(session Session, head, meta, indent string, selected bool, width int, background string) string {
	quiet := r.palette.subtle
	if selected {
		quiet = r.palette.muted
	}
	top := rowColumns(head, meta, width-railGutter)
	room := width - railGutter - ansi.StringWidth(indent) - 2
	lines := []string{paint(top, width, background)}
	if !session.IsShell {
		promptLine := indent + quiet.Render("-")
		if prompt := oneLine(session.Prompt); prompt != "" && room >= 8 {
			promptLine = indent + lipgloss.NewStyle().Foreground(lipgloss.Color(r.ctx.Theme.Accent)).Render("❯ ") +
				lipgloss.NewStyle().Foreground(lipgloss.Color(mix(r.ctx.Theme.Accent, r.ctx.Theme.Dim, 0.5))).Render(ansi.Truncate(prompt, room, "…"))
		}
		lines = append(lines, paint(promptLine, width, background))
	}
	lines = append(lines, paint(indent+r.replyCell(session, quiet, room+2), width, background))
	return strings.Join(lines, "\n")
}

func (r renderer) replyCell(session Session, quiet lipgloss.Style, room int) string {
	if session.Status == "working" && session.PaneLine == "" {
		frame := startupFrames[r.ctx.StartupPhase%len(startupFrames)]
		return lipgloss.NewStyle().Foreground(r.palette.statusColor("working")).Render(frame + " working")
	}
	if session.PaneLine == "" {
		return quiet.Render("-")
	}
	line := ansi.Truncate(session.PaneLine, max(room-2, 1), "…")
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(mix(string(r.palette.statusColor(session.Status)), r.ctx.Theme.Subtle, 0.5)))
	if session.Status == "waiting" {
		style = lipgloss.NewStyle().Foreground(r.palette.statusColor(session.Status))
	}
	return r.palette.subtle.Render("↳ ") + style.Render(line)
}

func (r renderer) renderGroup(row treeRow, selected bool, width int, pad, guides, background string) string {
	marker := "▾"
	if r.model.collapsed[row.foldKey()] {
		marker = "▸"
	}
	nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(r.ctx.Theme.Accent2)).Bold(true)
	name := presentation.EscapeControlsInline(baseName(row.group))
	if selected {
		nameStyle = nameStyle.Foreground(lipgloss.Color(r.ctx.Theme.Bright))
	}
	if row.isRoot() {
		marker, name = " ", "root"
		if !selected {
			nameStyle = nameStyle.Foreground(lipgloss.Color(mix(r.ctx.Theme.Accent2, r.ctx.Theme.Subtle, 0.5)))
		}
	}
	lead := pad + guides + r.palette.subtle.Render(marker) + " "
	head := lead + r.rowHandle(row, selected) + r.palette.highlight(name, r.model.search, nameStyle)
	meta := r.groupStatusGlyphs(row.host, row.group, false)
	if meta == "" {
		meta = r.palette.subtle.Render("no agents yet")
	}
	return paint(rowColumns(head, meta, width-railGutter), width, background)
}

// groupStatusGlyphs counts the agents in group's subtree; a whole host
// counts every agent on it, where the local root counts only its own.
func (r renderer) groupStatusGlyphs(host, group string, whole bool) string {
	counts := map[string]int{}
	for _, session := range r.model.listed(host, r.model.sessionsOf(host)) {
		if !session.IsShell && (whole || inGroupSubtree(session.Group, group)) {
			counts[session.Status]++
		}
	}
	var parts []string
	for _, state := range []string{"starting", "working", "waiting", "finished", "errored", "idle", "dead"} {
		if counts[state] == 0 {
			continue
		}
		parts = append(parts, lipgloss.NewStyle().Foreground(r.palette.statusColor(state)).
			Render(fmt.Sprintf("%s %d", statusGlyph(state), counts[state])))
	}
	return strings.Join(parts, r.palette.subtle.Render("  "))
}
