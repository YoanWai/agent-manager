package notices

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Panel is the messages modal's own state: the selected notice, the body
// scroll and the notices dismissed for good. The root persists a dismissal
// and restores it when the write fails.
type Panel struct {
	cursor int
	scroll int
	// lines is the notice each painted row inside the frame shows, for
	// clicks.
	lines presentation.Lines[int]
	// dismissed holds the notice ids the user closed for good; the set
	// persists in settings so a dismissed message never comes back.
	dismissed map[string]bool
}

// NewPanel starts a panel with the dismissals loaded from settings.
func NewPanel(dismissed map[string]bool) Panel {
	return Panel{dismissed: dismissed}
}

func (p Panel) Cursor() int { return p.cursor }

// Offset is the body scroll as last set; a view clamps it to the page.
func (p Panel) Offset() int { return p.scroll }

func (p Panel) Dismissed(id string) bool { return p.dismissed[id] }

// Dismiss drops a notice from the list ahead of the write that persists it.
func (p *Panel) Dismiss(id string) { p.dismissed[id] = true }

// Restore lists a notice again after its dismissal failed to persist.
func (p *Panel) Restore(id string) { delete(p.dismissed, id) }

// Open shows the panel on the notice with id, or on the first one. It opens
// even with nothing in it: a fresh install and the first run after an
// update both retire every message, and that is exactly when someone
// reaches for r to look again.
func (p *Panel) Open(notices []Notice, id string) {
	p.cursor = 0
	p.scroll = 0
	for i, n := range notices {
		if n.ID == id {
			p.cursor = i
			break
		}
	}
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
	p.cursor = 0
	for i, n := range notices {
		if n.ID == id {
			p.cursor = i
			break
		}
	}
}

// Theme is the root's palette and the card fill the panel draws with.
type Theme struct {
	Accent      lipgloss.Color
	Accent2     lipgloss.Color
	Bright      lipgloss.Color
	SubtleColor lipgloss.Color
	Subtle      lipgloss.Style
	Muted       lipgloss.Style
	Value       lipgloss.Style
	Key         lipgloss.Style
	// Border is the card's frame; Title its legend.
	Border lipgloss.Style
	Title  lipgloss.Style
	// Fill pads a row to width over the card's background.
	Fill func(row string, width int) string
}

// Frame is what one paint or key of the panel sees: the terminal, the
// refresh and download in flight, the status bar's message and the theme.
type Frame struct {
	GlyphMode  presentation.GlyphMode
	Width      int
	Height     int
	Refreshing bool
	Applying   bool
	Latest     string
	Status     string
	Theme      Theme
}

// Action is what a key asks the root to do.
type Action uint8

const (
	Stay Action = iota
	// Refresh is one manual pass over releases and the feed.
	Refresh
	// StartUpdate starts the self-update from the update notice.
	StartUpdate
	// OpenLink opens URL.
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

// WheelRows is how far one wheel notch scrolls the body.
const WheelRows = 3

func (p *Panel) Key(msg tea.KeyMsg, notices []Notice, f Frame) Outcome {
	switch msg.String() {
	case "r":
		return Outcome{Action: Refresh}
	case "u":
		if p.cursor < len(notices) && notices[p.cursor].IsUpdate() {
			return Outcome{Action: StartUpdate}
		}
	case "up", "k":
		if p.cursor > 0 {
			p.cursor--
			p.scroll = 0
		}
	case "down", "j":
		if p.cursor < len(notices)-1 {
			p.cursor++
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
		p.cursor = min(p.cursor, len(notices)-2)
		p.scroll = 0
		return out
	case "esc", "q", "M":
		return Outcome{Action: Close}
	}
	return Outcome{}
}

// Click is a press on row line inside the frame. The first click on a
// title selects it; one on the title already selected opens its link, the
// way ↵ does.
func (p *Panel) Click(line int, notices []Notice, f Frame) Outcome {
	i, ok := p.lines.At(line)
	switch {
	case !ok:
		return Outcome{}
	case !presentation.ClickRow(&p.cursor, i):
		p.scroll = 0
		return Outcome{}
	}
	return p.Key(tea.KeyMsg{Type: tea.KeyEnter}, notices, f)
}

func (p Panel) ScrollLimit(notices []Notice, f Frame) int {
	if p.cursor >= len(notices) {
		return 0
	}
	inner := innerWidth(notices, f.Width)
	room := bodyRoom(f.Height, len(notices), len(p.tail(notices, f, inner))+1)
	body, _ := bodyLayout(notices[p.cursor], inner, room, f.Theme)
	return max(0, len(body)-room)
}

// Wheel scrolls the body by step notches of the wheel.
func (p *Panel) Wheel(step int, notices []Notice, f Frame) {
	p.scrollBy(notices, step*WheelRows, f)
}

// scrollBy steps from the page on screen, since a resize can leave the
// saved offset past the last page.
func (p *Panel) scrollBy(notices []Notice, rows int, f Frame) {
	limit := p.ScrollLimit(notices, f)
	p.scroll = min(max(min(p.scroll, limit)+rows, 0), limit)
}

func bodyRoom(height, noticeCount, tailRows int) int {
	return max(1, height-2-(noticeCount+2)-tailRows)
}

func (p Panel) tail(notices []Notice, f Frame, inner int) []string {
	var tail []string
	if p.cursor < len(notices) {
		if url := notices[p.cursor].URL; url != "" {
			tail = append(tail, f.Theme.Subtle.Render(presentation.GlyphsFor(f.GlyphMode).LinkGlyph+" "+presentation.TruncateTail(strings.TrimPrefix(url, "https://"), inner-2)))
		}
	}
	if f.Refreshing {
		tail = append(tail, lipgloss.NewStyle().Foreground(f.Theme.Accent2).Render(presentation.GlyphsFor(f.GlyphMode).RefreshGlyph+" refreshing releases and messages…"))
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
	inner := innerWidth(notices, f.Width)
	p.lines.Reset()
	if len(notices) == 0 {
		rows := []string{t.Subtle.Render("nothing new")}
		rows = append(rows, p.tail(notices, f, inner)...)
		frame := cardFrame(rows, inner, legend(t), t.Muted.Render(footText(footPairs(notices, p.cursor, inner))), t)
		return frame
	}
	var rows []string
	rows = append(rows, "")
	p.lines.Skip(1)
	for i, n := range notices {
		marker := "  "
		title := t.Value.Render(n.Title)
		if i == p.cursor {
			marker = lipgloss.NewStyle().Foreground(t.Accent).Render(presentation.GlyphsFor(f.GlyphMode).NoticeSelectionGlyph + " ")
			title = lipgloss.NewStyle().Foreground(t.Bright).Bold(true).Render(n.Title)
		}
		rows = append(rows, marker+n.mark(t)+" "+title)
		p.lines.Show(i, 1)
	}
	selected := notices[p.cursor]
	rows = append(rows, t.Border.Render(strings.Repeat("┄", inner)))

	tail := p.tail(notices, f, inner)
	tail = append(tail, "")

	room := bodyRoom(f.Height, len(notices), len(tail))
	body, scrolls := bodyLayout(selected, inner, room, t)
	if scrolls {
		body = scrollWindow(body, room, p.scroll, inner, t)
	}
	rows = append(rows, body...)
	rows = append(rows, tail...)

	frame := cardFrame(rows, inner,
		legend(t),
		t.Muted.Render(footText(footPairs(notices, p.cursor, inner))), t)
	return frame
}

// footStart is the column the bottom edge's text starts at, after its
// corner and the dash before it.
const footStart = 2

const footSep = " · "

// Foot is where each key the bottom edge names was painted, the ones the
// edge's truncation cut left out.
func (p Panel) Foot(notices []Notice, f Frame) []presentation.PaintedKey {
	inner := innerWidth(notices, f.Width)
	var keys []presentation.PaintedKey
	x := footStart
	for i, pair := range footPairs(notices, p.cursor, inner) {
		if i > 0 {
			x += lipgloss.Width(footSep)
		}
		width := lipgloss.Width(footPart(pair))
		if x+width > footStart+inner+1 {
			break
		}
		keys = append(keys, presentation.PaintedKey{X0: x, X1: x + width, Label: pair[0]})
		x += width
	}
	return keys
}

func footPairs(notices []Notice, cursor, inner int) [][2]string {
	if len(notices) == 0 {
		return [][2]string{{"r", "refresh"}, {"esc", ""}}
	}
	var update [][2]string
	if notices[cursor].IsUpdate() {
		update = [][2]string{{"u", "update"}}
	}
	actions := append(append([][2]string{{"r", "refresh"}}, update...), [][2]string{{"↵", "open"}, {"x", "dismiss"}, {"esc", ""}}...)
	// Navigation shrinks, then drops, then the actions lose their words, so
	// every key a click can run stays on the edge.
	bare := [][2]string{{"↑↓", ""}}
	for _, action := range actions {
		bare = append(bare, [2]string{action[0], ""})
	}
	for _, pairs := range [][][2]string{
		append([][2]string{{"↑↓", "pick"}, {"pgup/pgdn/home/end", "scroll"}}, actions...),
		append([][2]string{{"↑↓", "pick"}, {"pgup/pgdn", "scroll"}}, actions...),
		actions,
	} {
		if lipgloss.Width(footText(pairs)) <= inner+1 {
			return pairs
		}
	}
	return bare
}

func footPart(pair [2]string) string { return strings.TrimSpace(pair[0] + " " + pair[1]) }

func footText(pairs [][2]string) string {
	parts := make([]string, len(pairs))
	for i, pair := range pairs {
		parts[i] = footPart(pair)
	}
	return strings.Join(parts, footSep)
}

// legend is the card's title, set into the border: lowercase, warm,
// no fill, so it reads as a fieldset legend rather than a badge.
func legend(t Theme) string {
	return t.Title.Render(" messages ")
}

// cardFrame wraps content rows in the notices fieldset: a rounded
// yellow border with legends set into the top and bottom edges and the
// card fill behind every row. inner is the content column's width.
func cardFrame(rows []string, inner int, topLegend, bottomLegend string, t Theme) []string {
	border := t.Border
	edge := border.Render("│")
	legend := func(left, label, right string) string {
		label = ansi.Truncate(label, inner+1, "…")
		dashes := inner + 2 - lipgloss.Width(label)
		if label != "" {
			label = border.Render("─") + label
			dashes--
		}
		if dashes < 0 {
			dashes = 0
		}
		return border.Render(left) + label + border.Render(strings.Repeat("─", dashes)+right)
	}
	lines := []string{legend("╭", topLegend, "╮")}
	for _, row := range rows {
		row = ansi.Truncate(row, inner, "…")
		lines = append(lines, edge+t.Fill(" "+row, inner+2)+edge)
	}
	return append(lines, legend("╰", bottomLegend, "╯"))
}

func padRight(s string, width int) string { return presentation.PadRight(s, width) }
