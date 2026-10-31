package notices

import (
	"slices"
	"strings"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Panel is the messages modal's own state: the selected notice, the body
// scroll, a release's tab and filter, the notices dismissed for good and the
// ones already read. The root persists a dismissal and restores it when the
// write fails.
type Panel struct {
	cursor int
	scroll int
	tab    Tab
	filter string
	// lines is the notice each painted row inside the frame shows, for
	// clicks; listCols is how far across a row its notice reaches, zero
	// for the whole row.
	lines    presentation.Lines[int]
	listCols int
	hits     []paneHit
	// dismissed holds the notice ids the user closed for good; the set
	// persists in settings so a dismissed message never comes back.
	dismissed map[string]bool
	// seen holds the notice ids that have been the selected message while
	// the panel was open; it persists beside the dismissed set.
	seen map[string]bool
}

func NewPanel(dismissed, seen map[string]bool) Panel {
	return Panel{dismissed: dismissed, seen: seen}
}

func (p Panel) Cursor() int { return p.cursor }

// Offset is the body scroll as last set; a view clamps it to the page.
func (p Panel) Offset() int { return p.scroll }

func (p Panel) Dismissed(id string) bool { return p.dismissed[id] }

// Dismiss drops a notice from the list ahead of the write that persists it.
func (p *Panel) Dismiss(id string) { p.dismissed[id] = true }

func (p *Panel) Restore(id string) { delete(p.dismissed, id) }

func (p Panel) Seen(id string) bool { return p.seen[id] }

// See marks a notice read ahead of the write that persists it.
func (p *Panel) See(id string) { p.seen[id] = true }

// Open shows the panel on the notice with id, or on the first one. It opens
// even with nothing in it: a fresh install and the first run after an
// update both retire every message, and that is exactly when someone
// reaches for r to look again.
func (p *Panel) Open(notices []Notice, id string) {
	p.show(0)
	for i, n := range notices {
		if n.ID == id {
			p.show(i)
			break
		}
	}
}

// show selects notice i on its first page and its Overview, unfiltered.
func (p *Panel) show(i int) {
	p.cursor, p.scroll, p.tab, p.filter = i, 0, TabOverview, ""
}

func (p Panel) onRelease(notices []Notice) bool {
	return p.cursor < len(notices) && notices[p.cursor].ReleaseNotes
}

// Selected is the id of the notice under the cursor, empty when none is.
func (p Panel) Selected(notices []Notice) string {
	if p.cursor < len(notices) {
		return notices[p.cursor].ID
	}
	return ""
}

// Reselect points the cursor back at the notice with id after the list
// changed under it, or at the first one when it is gone. Index arithmetic
// cannot do this: whether the list actually changed depends on dismissals
// and on what the change replaced.
func (p *Panel) Reselect(notices []Notice, id string) {
	for i, n := range notices {
		if n.ID == id {
			p.cursor = i
			return
		}
	}
	p.show(0)
}

type Theme struct {
	Accent   lipgloss.Color
	Accent2  lipgloss.Color
	Bright   lipgloss.Color
	Working  lipgloss.Color
	Waiting  lipgloss.Color
	Finished lipgloss.Color
	// Ink is the backdrop's tone, for text set on an accent fill.
	Ink    lipgloss.Color
	Subtle lipgloss.Style
	Muted  lipgloss.Style
	Value  lipgloss.Style
	Key    lipgloss.Style
	// Border is the card's frame; Title its legend.
	Border lipgloss.Style
	Title  lipgloss.Style
	Fill   func(row string, width int) string
	// Surface fills the selected list row and a chip over the card; Raised
	// is one step above it, for a key and the active filter.
	Surface func(row string, width int) string
	Raised  func(row string, width int) string
	// Callout is a call to action's faint fill, tinted toward tone.
	Callout func(row string, width int, tone lipgloss.Color) string
}

const FeedPendingText = "checking messages…"

// Frame is what one paint or key of the panel sees: the terminal, the
// refresh and download in flight, the status bar's message and the theme.
type Frame struct {
	CloseKey    keybind.Binding
	GlyphMode   presentation.GlyphMode
	Width       int
	Height      int
	Refreshing  bool
	FeedPending bool
	Applying    bool
	Latest      string
	Status      string
	Theme       Theme
}

type Action uint8

const (
	Stay Action = iota
	Refresh
	StartUpdate
	OpenLink
	// Dismiss persists dismissing ID, when there is one, and closes the
	// panel when Close says the list is now empty.
	Dismiss
	Close
)

type Outcome struct {
	Action Action
	ID     string
	URL    string
	Close  bool
}

const WheelRows = 3

func (p *Panel) Key(msg tea.KeyMsg, notices []Notice, f Frame) Outcome {
	if closeBinding(f.CloseKey).Has(keybind.Normalize(msg.String())) {
		return Outcome{Action: Close}
	}
	switch msg.String() {
	case "r":
		return Outcome{Action: Refresh}
	case "u":
		if p.cursor < len(notices) && notices[p.cursor].IsUpdate() {
			return Outcome{Action: StartUpdate}
		}
	case "up", "k":
		if p.cursor > 0 {
			p.show(p.cursor - 1)
		}
	case "down", "j":
		if p.cursor < len(notices)-1 {
			p.show(p.cursor + 1)
		}
	case "tab", "right", "l":
		p.stepTab(notices, 1)
	case "shift+tab", "left", "h":
		p.stepTab(notices, len(tabLabels)-1)
	case "f":
		if p.onRelease(notices) && p.tab == TabChanges {
			p.filter = changeFilters[(slices.Index(changeFilters, p.filter)+1)%len(changeFilters)]
			p.scroll = 0
		}
	case "pgup", "ctrl+u":
		p.scrollBy(notices, -max(4, f.Height/3), f)
	case "pgdown", "ctrl+d":
		p.scrollBy(notices, max(4, f.Height/3), f)
	case "home", "g":
		p.scroll = 0
	case "end", "G":
		p.scroll = p.ScrollLimit(notices, f)
	case "enter":
		if p.cursor < len(notices) && notices[p.cursor].URL != "" {
			return Outcome{Action: OpenLink, URL: notices[p.cursor].URL}
		}
	case "x", "d":
		out := Outcome{Action: Dismiss, ID: p.Selected(notices)}
		if len(notices) <= 1 {
			out.Close = true
			return out
		}
		p.show(min(p.cursor, len(notices)-2))
		return out
	case "esc", "q":
		return Outcome{Action: Close}
	}
	return Outcome{}
}

func (p *Panel) stepTab(notices []Notice, step int) {
	if p.onRelease(notices) {
		p.tab = Tab((int(p.tab) + step) % len(tabLabels))
		p.scroll = 0
	}
}

// Click is a press at col of row line inside the frame. A click on a tab or
// a filter chip shows it, one on the update bar updates the way u does, and
// one on a link opens it the way ↵ does.
// The first click on a row selects it; one on the row already selected opens
// its link, the way ↵ does.
func (p *Panel) Click(col, line int, notices []Notice, f Frame) Outcome {
	for _, hit := range p.hits {
		if line != hit.line || col < hit.x0 || col >= hit.x1 {
			continue
		}
		switch hit.action {
		case StartUpdate:
			return p.Key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}}, notices, f)
		case OpenLink:
			return p.Key(tea.KeyMsg{Type: tea.KeyEnter}, notices, f)
		}
		p.tab, p.filter, p.scroll = hit.tab, hit.filter, 0
		return Outcome{}
	}
	i, ok := p.lines.At(line)
	switch {
	case !ok || p.listCols > 0 && col >= p.listCols:
		return Outcome{}
	case !presentation.ClickRow(&p.cursor, i):
		p.show(i)
		return Outcome{}
	}
	return p.Key(tea.KeyMsg{Type: tea.KeyEnter}, notices, f)
}

func (p Panel) ScrollLimit(notices []Notice, f Frame) int {
	if p.cursor >= len(notices) {
		return 0
	}
	splitView := p.splitView(notices, f)
	_, pane := inboxWidths(notices, f.Width, splitView)
	room := bodyRoom(f.Height, p.rowsAbovePane(notices, f, splitView), len(frameTail(f))+1)
	layout := p.message(notices[p.cursor], pane, room, f)
	return max(0, len(layout.body)-layout.room)
}

// splitView sets the list beside the message when the terminal is wide
// enough, and tall enough for the list's two-line rows between a blank row
// above and below.
func (p Panel) splitView(notices []Notice, f Frame) bool {
	if !split(f.Width) {
		return false
	}
	list, _ := listRows(notices, p.cursor, listWidth, f.Theme, f.GlyphMode, true)
	return 1+len(list)+1+2 <= f.Height
}

// minPageRows is the least of a tab a release keeps in view below its
// fixed header; a shorter room scrolls the header with the tab.
const minPageRows = 4

// paneLayout is the message pane: the head that stays put and the body that
// scrolls under it, with what a click on each reaches.
type paneLayout struct {
	head     []string
	hits     []paneHit
	body     []string
	bodyHits []paneHit
	room     int
	scrolls  bool
}

// message lays the selected notice out in room rows: a release keeps its
// header and tabs above the part that scrolls.
func (p Panel) message(n Notice, width, room int, f Frame) paneLayout {
	t, mode := f.Theme, f.GlyphMode
	if !n.ReleaseNotes {
		render := func(width int) ([]string, []paneHit) {
			rows := feedPane(n, width, t, mode)
			if n.URL == "" {
				return rows, nil
			}
			return rows, []paneHit{{line: len(rows) - 1, x1: width, action: OpenLink}}
		}
		layout := paneLayout{room: room}
		layout.body, layout.bodyHits = render(width)
		if len(layout.body) > room {
			layout.body, layout.bodyHits = render(max(width-scrollbarWidth, 1))
			layout.scrolls = true
		}
		return layout
	}
	head, hits := releaseHead(n, p.tab, p.filter, width, t, mode)
	layout := paneLayout{head: head, hits: hits, room: room - len(head)}
	render := func(width int) ([]string, []paneHit) { return releaseTab(n, p.tab, p.filter, width, t, mode), nil }
	if layout.room < minPageRows {
		layout = paneLayout{room: room}
		render = func(width int) ([]string, []paneHit) { return releasePane(n, p.tab, p.filter, width, t, mode) }
	}
	layout.body, layout.bodyHits = render(width)
	if len(layout.body) > layout.room {
		layout.body, layout.bodyHits = render(max(width-scrollbarWidth, 1))
		layout.scrolls = true
	}
	return layout
}

func scrollWindow(body []string, room, offset, inner int, t Theme, mode presentation.GlyphMode) []string {
	lastOffset := len(body) - room
	offset = min(max(offset, 0), lastOffset)
	thumb := max(1, room*room/len(body))
	thumbTop := offset * (room - thumb) / lastOffset
	track := t.Subtle.Render("│")
	grip := lipgloss.NewStyle().Foreground(t.Accent).Render(presentation.ChromeFor(mode).ScrollThumbGlyph)
	rows := make([]string, room)
	for index := range rows {
		cell := track
		if index >= thumbTop && index < thumbTop+thumb {
			cell = grip
		}
		rows[index] = padRight(body[offset+index], inner-1) + cell
	}
	return rows
}

func (p *Panel) Wheel(step int, notices []Notice, f Frame) {
	p.scrollBy(notices, step*WheelRows, f)
}

// scrollBy steps from the page on screen, since a resize can leave the
// saved offset past the last page.
func (p *Panel) scrollBy(notices []Notice, rows int, f Frame) {
	limit := p.ScrollLimit(notices, f)
	p.scroll = min(max(min(p.scroll, limit)+rows, 0), limit)
}

func bodyRoom(height, above, tailRows int) int {
	return max(1, height-2-above-tailRows)
}

// rowsAbovePane is the blank top row, and when stacked the list and its rule.
func (p Panel) rowsAbovePane(notices []Notice, f Frame, splitView bool) int {
	if splitView {
		return 1
	}
	inner, _ := inboxWidths(notices, f.Width, false)
	list, _ := listRows(notices, p.cursor, inner, f.Theme, f.GlyphMode, false)
	return len(list) + 2
}

func frameTail(f Frame) []string {
	var tail []string
	if f.Refreshing {
		tail = append(tail, lipgloss.NewStyle().Foreground(f.Theme.Accent2).Render(presentation.GlyphsFor(f.GlyphMode).RefreshGlyph+" refreshing releases and messages…"))
	}
	if f.FeedPending {
		tail = append(tail, lipgloss.NewStyle().Foreground(f.Theme.Accent2).Render(presentation.GlyphsFor(f.GlyphMode).RefreshGlyph+" "+FeedPendingText))
	}
	if f.Applying {
		tail = append(tail, lipgloss.NewStyle().Foreground(f.Theme.Accent).Render(presentation.GlyphsFor(f.GlyphMode).DownloadGlyph+" downloading "+f.Latest+"…"))
	}
	if f.Status != "" {
		tail = append(tail, f.Status)
	}
	return tail
}

func (p *Panel) View(notices []Notice, f Frame) []string {
	t := f.Theme
	splitView := p.splitView(notices, f)
	inner, pane := inboxWidths(notices, f.Width, splitView)
	p.lines.Reset()
	p.listCols = 0
	p.hits = nil
	foot := renderFoot(p.footPairs(notices, inner, f), t, f.GlyphMode)
	if len(notices) == 0 {
		rows := []string{t.Muted.Render("nothing new")}
		rows = append(rows, frameTail(f)...)
		return cardFrame(rows, inner, legend(t), "", foot, t, f.GlyphMode)
	}
	listColumn := inner
	if splitView {
		listColumn = listWidth
	}
	list, owners := listRows(notices, p.cursor, listColumn, t, f.GlyphMode, splitView)

	tail := frameTail(f)
	tail = append(tail, "")
	room := bodyRoom(f.Height, p.rowsAbovePane(notices, f, splitView), len(tail))
	layout := p.message(notices[p.cursor], pane, room, f)
	body, offset := layout.body, 0
	if layout.scrolls {
		offset = min(max(p.scroll, 0), len(body)-layout.room)
		body = scrollWindow(body, layout.room, p.scroll, pane, t, f.GlyphMode)
	}
	message := append(append(layout.head, body...), tail...)

	rows := []string{""}
	p.lines.Skip(1)
	for _, owner := range owners {
		if owner < 0 {
			p.lines.Skip(1)
		} else {
			p.lines.Show(owner, 1)
		}
	}
	top, left := len(rows), 0
	if splitView {
		p.listCols = listWidth
		left = listWidth + 1 + panePadding
		rows = append(rows, besideList(append(list, ""), message, t)...)
	} else {
		rows = append(rows, list...)
		rows = append(rows, t.Subtle.Render(strings.Repeat(presentation.ChromeFor(f.GlyphMode).DashedRuleGlyph, inner)))
		top = len(rows)
		rows = append(rows, message...)
	}
	for _, hit := range layout.hits {
		hit.line, hit.x0, hit.x1 = hit.line+top, hit.x0+left, hit.x1+left
		p.hits = append(p.hits, hit)
	}
	for _, hit := range layout.bodyHits {
		if row := hit.line - offset; row >= 0 && row < layout.room {
			hit.line, hit.x0, hit.x1 = top+len(layout.head)+row, hit.x0+left, hit.x1+left
			p.hits = append(p.hits, hit)
		}
	}
	return cardFrame(rows, inner, legend(t), unreadCount(notices, t), foot, t, f.GlyphMode)
}

// footStart is the column the bottom edge's text starts at, after its
// corner and the dash before it.
const footStart = 2

const footSep = "  "

// Foot is where each key the bottom edge names was painted, the ones the
// edge's truncation cut left out.
func (p Panel) Foot(notices []Notice, f Frame) []presentation.PaintedKey {
	inner, _ := inboxWidths(notices, f.Width, p.splitView(notices, f))
	var keys []presentation.PaintedKey
	x := footStart
	for i, pair := range p.footPairs(notices, inner, f) {
		if i > 0 {
			x += lipgloss.Width(footSep)
		}
		width := lipgloss.Width(footPart(pair, f.GlyphMode))
		if x+width > footStart+inner+1 {
			break
		}
		keys = append(keys, presentation.PaintedKey{X0: x, X1: x + width, Label: pair[0]})
		x += width
	}
	return keys
}

func (p Panel) footPairs(notices []Notice, inner int, f Frame) [][2]string {
	closeKeys := "esc/q"
	for _, key := range closeBinding(f.CloseKey).Keys() {
		if key.Tea() != "q" {
			closeKeys += "/" + key.Glyph()
		}
	}
	if len(notices) == 0 {
		return [][2]string{{"r", "refresh"}, {closeKeys, ""}}
	}
	var sections, update [][2]string
	if p.onRelease(notices) {
		sections = [][2]string{{"tab", "section"}}
		if p.tab == TabChanges {
			sections = append(sections, [2]string{"f", "filter"})
		}
	}
	if notices[p.cursor].IsUpdate() {
		update = [][2]string{{"u", "update"}}
	}
	rest := append(append([][2]string{{"r", "refresh"}}, update...), [][2]string{{"↵", "open"}, {"x", "dismiss"}, {closeKeys, ""}}...)
	actions := append(slices.Clone(sections), rest...)
	// Navigation shrinks, then drops, then the sections and the actions lose
	// their words, so every key a click can run stays on the edge.
	keysOnly := func(pairs [][2]string) [][2]string {
		bare := make([][2]string, len(pairs))
		for i, pair := range pairs {
			bare[i] = [2]string{pair[0], ""}
		}
		return bare
	}
	bare := keysOnly(append([][2]string{{"↑↓", ""}}, actions...))
	for _, pairs := range [][][2]string{
		append([][2]string{{"↑↓", "message"}, {"pgup/pgdn/home/end", "scroll"}}, actions...),
		append([][2]string{{"↑↓", "message"}, {"pgup/pgdn", "scroll"}}, actions...),
		actions,
		append(keysOnly(sections), rest...),
	} {
		if lipgloss.Width(footText(pairs, f.GlyphMode)) <= inner+1 {
			return pairs
		}
	}
	return bare
}

func footPart(pair [2]string, mode presentation.GlyphMode) string {
	return strings.TrimSpace(presentation.KeyLabel(mode, pair[0]) + " " + pair[1])
}

func renderFoot(pairs [][2]string, t Theme, mode presentation.GlyphMode) string {
	keyStyle := lipgloss.NewStyle().Foreground(t.Accent)
	parts := make([]string, len(pairs))
	for i, pair := range pairs {
		parts[i] = keyStyle.Render(presentation.KeyLabel(mode, pair[0]))
		if pair[1] != "" {
			parts[i] += t.Muted.Render(" " + pair[1])
		}
	}
	return strings.Join(parts, t.Muted.Render(footSep))
}

func footText(pairs [][2]string, mode presentation.GlyphMode) string {
	parts := make([]string, len(pairs))
	for i, pair := range pairs {
		parts[i] = footPart(pair, mode)
	}
	return strings.Join(parts, footSep)
}

// legend is the card's title, set into the border: lowercase and bright,
// no fill, so it reads as a fieldset legend rather than a badge.
func legend(t Theme) string {
	return t.Title.Render(" messages ")
}

// cardFrame wraps content rows in the notices fieldset: a rounded
// accent border with legends set into the top and bottom edges, a second
// legend at the top edge's right, and the card fill behind every row.
// inner is the content column's width.
func cardFrame(rows []string, inner int, topLegend, topRight, bottomLegend string, t Theme, mode presentation.GlyphMode) []string {
	g := presentation.ChromeFor(mode)
	border := t.Border
	edge := border.Render("│")
	legend := func(left, label, rightLegend, right string) string {
		label = ansi.Truncate(label, inner+1, "…")
		if label != "" {
			label = border.Render("─") + label
		}
		if rightLegend != "" {
			rightLegend += border.Render("─")
		}
		if lipgloss.Width(label)+lipgloss.Width(rightLegend) > inner+2 {
			rightLegend = ""
		}
		dashes := max(inner+2-lipgloss.Width(label)-lipgloss.Width(rightLegend), 0)
		return border.Render(left) + label + border.Render(strings.Repeat("─", dashes)) + rightLegend + border.Render(right)
	}
	lines := []string{legend(g.TopLeftGlyph, topLegend, topRight, g.TopRightGlyph)}
	for _, row := range rows {
		row = ansi.Truncate(row, inner, "…")
		lines = append(lines, edge+t.Fill(" "+row, inner+2)+edge)
	}
	return append(lines, legend(g.BottomLeftGlyph, bottomLegend, "", g.BottomRightGlyph))
}

func padRight(s string, width int) string { return presentation.PadRight(s, width) }

// Reserve local actions even when the selected notice cannot use them.
func closeBinding(binding keybind.Binding) keybind.Binding {
	var keys []keybind.Key
	for _, key := range binding.Keys() {
		switch key.Tea() {
		case "r", "u", "up", "k", "down", "j", "pgup", "ctrl+u", "pgdn", "ctrl+d", "home", "g", "end", "G", "enter", "x", "d", "tab", "shift+tab", "right", "l", "left", "h", "f":
			continue
		}
		keys = append(keys, key)
	}
	return keybind.Keys(keys...)
}
