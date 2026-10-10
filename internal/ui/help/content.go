package help

import (
	"fmt"
	"strings"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const helpCardMaxWidth = 92

type Glyphs struct {
	RowMenu          string
	Reorder          string
	AfterTurnArchive string
	AfterTurnKill    string
}

// QuickKeys are the quick prompt's fixed spawn-choice keys, which root owns.
type QuickKeys struct {
	Model   string
	Effort  string
	Profile string
}

// Styles is a snapshot of the root UI's active presentation values. State
// never retains it, so a theme change reaches the next render immediately.
type Styles struct {
	Section lipgloss.Style
	Rule    lipgloss.Style
	Key     lipgloss.Style
	Muted   lipgloss.Style
	Subtle  lipgloss.Style
	Value   lipgloss.Style
	Match   lipgloss.Style
	Cursor  lipgloss.Style
}

// Context is the copied presentation state Help needs to build its content.
type Context struct {
	SessionKeys  keybind.Table
	ListKeys     keybind.Table
	ArrowStep    bool
	Width        int
	Glyphs       Glyphs
	QuickKeys    QuickKeys
	Styles       Styles
	CursorMarker string
}

// Content is the feature-owned dialog body. The root owns viewport geometry
// and shared dialog chrome, then renders these values without callbacks.
type Content struct {
	Title  string
	Head   []string
	Lines  []string
	Hints  [][2]string
	Offset int
}

// CardWidth keeps the map readable on a wide terminal while preserving the
// established narrow-terminal policy.
func CardWidth(terminalWidth int) int {
	width := helpCardMaxWidth
	if terminalWidth >= 28 && width > terminalWidth-4 {
		width = terminalWidth - 4
	}
	return width
}

// helpKeyColumn is the widest key in the complete catalog plus a gap. Search
// results therefore never shift the description column under the reader.
func helpKeyColumn(ctx Context) int {
	width := 0
	for _, section := range helpSections(ctx) {
		for _, row := range section.rows {
			if w := ansi.StringWidth(row[0]); w > width {
				width = w
			}
		}
	}
	return width + 2
}

// helpBodyLines lays the catalog out as one scrollable column: a titled rule
// per section, then its bindings in two aligned columns.
func helpBodyLines(sections []helpSection, ctx Context, query string) []string {
	keyColumn := helpKeyColumn(ctx)
	if room := ctx.Width / 3; keyColumn > room {
		keyColumn = max(room, 4)
	}
	var lines []string
	for i, section := range sections {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, presentation.Divider(section.title, ctx.Width, ctx.Styles.Section, ctx.Styles.Rule))
		for _, row := range section.rows {
			// A row with no key is a note about the one above it, so it
			// recedes into the description column instead of claiming a
			// binding of its own.
			if row[0] == "" {
				lines = append(lines, strings.Repeat(" ", keyColumn)+
					ctx.Styles.Subtle.Render(ansi.Truncate(row[1], max(ctx.Width-keyColumn, 1), "…")))
				continue
			}
			key := presentation.PadRight(ctx.Styles.Key.Render(row[0]), keyColumn)
			lines = append(lines, key+highlightMatch(row[1], query, ctx.Width-keyColumn, ctx.Styles))
		}
	}
	return lines
}

// highlightMatch renders a description with the matched run picked out, so
// a search lands the eye on the word it found instead of on the row.
func highlightMatch(text, query string, width int, styles Styles) string {
	text = ansi.Truncate(text, max(width, 1), "…")
	query = strings.TrimSpace(query)
	if query == "" {
		return styles.Muted.Render(text)
	}
	// Case folding can change a string's byte length, so the run to paint is
	// as long as the folded query, and a fold that moved the offsets past the
	// original drops the highlight instead of slicing out of range.
	folded := strings.ToLower(query)
	at := strings.Index(strings.ToLower(text), folded)
	if at < 0 || at+len(folded) > len(text) {
		return styles.Muted.Render(text)
	}
	return styles.Muted.Render(text[:at]) + styles.Match.Render(text[at:at+len(folded)]) +
		styles.Muted.Render(text[at+len(folded):])
}

func (s State) searchActive() bool {
	return s.searching || s.query != ""
}

func (s State) Content(ctx Context) Content {
	sections := matchHelp(s.visibleSections(ctx), s.query)
	var head []string
	if s.searchActive() {
		head = append(head, s.searchLine(sections, ctx), "")
	}
	lines := helpBodyLines(sections, ctx, s.query)
	if len(lines) == 0 {
		lines = []string{ctx.Styles.Subtle.Render("no key matches that")}
	}
	title := "? Keys"
	if s.scope == Review {
		title = "? Review keys"
	}
	return Content{
		Title:  title,
		Head:   head,
		Lines:  lines,
		Hints:  s.hints(),
		Offset: s.scroll,
	}
}

// searchLine is the search's own row: what was typed, and how much of the
// map still answers to it.
func (s State) searchLine(sections []helpSection, ctx Context) string {
	line := ctx.Styles.Key.Render("search ") + ctx.Styles.Value.Render(s.query)
	if s.searching {
		line += ctx.CursorMarker + ctx.Styles.Cursor.Render("▏")
	}
	count := helpRowCount(sections)
	label := " keys"
	if count == 1 {
		label = " key"
	}
	return line + ctx.Styles.Subtle.Render(fmt.Sprintf("   %d%s", count, label))
}

func (s State) hints() [][2]string {
	if s.searching {
		return [][2]string{{"type", "search"}, {"↵", "done"}, {"↑↓", "scroll"}, {"esc", "clear"}}
	}
	docs := [2]string{"o", "docs"}
	if s.query != "" {
		return [][2]string{{"↑↓/jk", "scroll"}, {"/", "search"}, docs, {"esc", "clear search"}, {"q", "close"}}
	}
	return [][2]string{
		{"↑↓/jk", "scroll"}, {"pgup/pgdn", "page"}, {"g/G", "top/bottom"},
		{"/", "search"}, docs, {"esc/q", "close"},
	}
}
