package ui

import (
	"fmt"
	"strings"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// helpCardMaxWidth keeps the card readable on a wide terminal: past this the
// eye has to travel too far from a key to its description.
const helpCardMaxWidth = 92

// helpBodyLines lays the catalog out as one scrollable column: a titled rule
// per section, then its bindings in two aligned columns.
func helpBodyLines(sections []helpSection, session, list keybind.Table, arrowStep bool, width int, query string) []string {
	keyColumn := helpKeyColumn(session, list, arrowStep)
	if room := width / 3; keyColumn > room {
		keyColumn = max(room, 4)
	}
	var lines []string
	for i, section := range sections {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, divider(section.title, width))
		for _, row := range section.rows {
			// A row with no key is a note about the one above it, so it
			// recedes into the description column instead of claiming a
			// binding of its own.
			if row[0] == "" {
				lines = append(lines, strings.Repeat(" ", keyColumn)+
					subtleStyle.Render(ansi.Truncate(row[1], max(width-keyColumn, 1), "…")))
				continue
			}
			key := padRight(keyStyle.Render(row[0]), keyColumn)
			lines = append(lines, key+highlightMatch(row[1], query, width-keyColumn))
		}
	}
	return lines
}

// highlightMatch renders a description with the matched run picked out, so
// a search lands the eye on the word it found instead of on the row.
func highlightMatch(text, query string, width int) string {
	text = ansi.Truncate(text, max(width, 1), "…")
	query = strings.TrimSpace(query)
	if query == "" {
		return mutedStyle.Render(text)
	}
	// Case folding can change a string's byte length, so the run to paint is
	// as long as the folded query, and a fold that moved the offsets past the
	// original drops the highlight instead of slicing out of range.
	folded := strings.ToLower(query)
	at := strings.Index(strings.ToLower(text), folded)
	if at < 0 || at+len(folded) > len(text) {
		return mutedStyle.Render(text)
	}
	hit := lipgloss.NewStyle().Foreground(colorBright).Bold(true)
	return mutedStyle.Render(text[:at]) + hit.Render(text[at:at+len(folded)]) +
		mutedStyle.Render(text[at+len(folded):])
}

func helpCardWidth(terminalWidth int) int {
	width := helpCardMaxWidth
	if terminalWidth >= 28 && width > terminalWidth-4 {
		width = terminalWidth - 4
	}
	return width
}

// bodyRoom is the rows of catalog the card can show after its chrome, search,
// status, and wrapping hint rows take their space.
func (h helpState) bodyRoom(ctx helpContext) int {
	inner := cardInnerWidth(helpCardWidth(ctx.width))
	room := ctx.height - 5 - lipgloss.Height(legendInline(h.hint(), inner))
	if h.searchActive() {
		room -= 2
	}
	if ctx.status != "" {
		room -= 2
	}
	return max(room, 1)
}

func (h helpState) searchActive() bool {
	return h.searching || h.query != ""
}

func (h helpState) scrollLimit(ctx helpContext) int {
	sections := matchHelp(h.visibleSections(ctx), h.query)
	body := helpBodyLines(sections, ctx.sessionKeys, ctx.listKeys, ctx.arrowStep, cardInnerWidth(helpCardWidth(ctx.width)), h.query)
	return max(0, len(body)-h.bodyRoom(ctx))
}

func (h helpState) view(ctx helpContext) string {
	width := helpCardWidth(ctx.width)
	inner := cardInnerWidth(width)
	sections := matchHelp(h.visibleSections(ctx), h.query)

	var head []string
	if h.searchActive() {
		head = append(head, h.searchLine(sections), "")
	}

	body := helpBodyLines(sections, ctx.sessionKeys, ctx.listKeys, ctx.arrowStep, inner, h.query)
	if len(body) == 0 {
		body = []string{subtleStyle.Render("no key matches that")}
	}
	lines := append(head, fitBody(body, h.bodyRoom(ctx), h.scroll)...)

	title := "? Keys"
	if h.scope == helpReview {
		title = "? Review keys"
	}
	dialog := dialogRenderContext{width: ctx.width, height: ctx.height, status: ctx.status}
	return renderDialog(dialog, width, title, strings.Join(lines, "\n"), h.hint())
}

// searchLine is the search's own row: what was typed, and how much of the
// map still answers to it.
func (h helpState) searchLine(sections []helpSection) string {
	line := keyStyle.Render("search ") + valueStyle.Render(h.query)
	if h.searching {
		line += cursorAnchorMarker + lipgloss.NewStyle().Foreground(colorAccent).Render("▏")
	}
	count := helpRowCount(sections)
	label := " keys"
	if count == 1 {
		label = " key"
	}
	return line + subtleStyle.Render(fmt.Sprintf("   %d%s", count, label))
}

func (h helpState) hint() [][2]string {
	if h.searching {
		return [][2]string{{"type", "search"}, {"↵", "done"}, {"↑↓", "scroll"}, {"esc", "clear"}}
	}
	if h.query != "" {
		return [][2]string{{"↑↓/jk", "scroll"}, {"/", "search"}, {"esc", "clear search"}, {"q", "close"}}
	}
	return [][2]string{
		{"↑↓/jk", "scroll"}, {"pgup/pgdn", "page"}, {"g/G", "top/bottom"},
		{"/", "search"}, {"esc/q", "close"},
	}
}

func (m *Model) viewHelp() string {
	return m.help.view(m.helpContext())
}
