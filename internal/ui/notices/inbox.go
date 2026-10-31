package notices

import (
	"fmt"
	"strings"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// A terminal this wide fits the list, the seam, the pane's padding, a
// proseWidth message with its scrollbar, and the card's margin beside it.
const (
	splitMinWidth = 124
	listWidth     = 34
	panePadding   = 3
)

func split(width int) bool { return width >= splitMinWidth }

// inboxWidths is the card's content column and the message pane inside it:
// the same column stacked, the room right of the list and its seam split.
func inboxWidths(notices []Notice, width int) (inner, pane int) {
	if !split(width) {
		inner = innerWidth(notices, width)
		return inner, inner
	}
	pane = max(innerWidth(notices, width-listWidth-1-panePadding), proseWidth+scrollbarWidth)
	return listWidth + 1 + panePadding + pane, pane
}

var groupLabels = [...]string{
	KindRelease: "RELEASES",
	KindNews:    "NEWS",
	KindHeadsUp: "HEADS UP",
	KindTip:     "TIPS",
}

// listRows is the list column, notices grouped by kind under a label each.
// owners holds the notice index each row shows, -1 for a label or a gap.
func listRows(notices []Notice, cursor, width int, t Theme, mode presentation.GlyphMode, twoLine bool) (rows []string, owners []int) {
	add := func(row string, owner int) {
		rows = append(rows, row)
		owners = append(owners, owner)
	}
	label := t.Muted.Bold(true)
	for i, n := range notices {
		if i == 0 || n.Kind != notices[i-1].Kind {
			if i > 0 && twoLine {
				add("", -1)
			}
			add("  "+label.Render(groupLabels[n.Kind]), -1)
		}
		lines := []string{titleLine(n, width-2, t, mode)}
		if twoLine {
			lines = append(lines, "  "+t.Muted.Render(ansi.Truncate(n.Subtitle, width-5, "…")))
		}
		for _, line := range lines {
			add(listRow(line, i == cursor, width, t, mode), i)
		}
	}
	return rows, owners
}

// titleLine is the kind glyph and the title, bold and ending in the unread
// mark while unread, with a cell to spare at the right.
func titleLine(n Notice, width int, t Theme, mode presentation.GlyphMode) string {
	style, room := t.Value, width-3
	if n.Unread {
		style, room = lipgloss.NewStyle().Foreground(t.Bright).Bold(true), room-2
	}
	line := kindGlyph(n, t, mode) + " " + style.Render(ansi.Truncate(n.Title, room, "…"))
	if !n.Unread {
		return line
	}
	return padRight(line, width-2) + lipgloss.NewStyle().Foreground(t.Accent).Render(presentation.GlyphsFor(mode).UnreadGlyph)
}

func kindGlyph(n Notice, t Theme, mode presentation.GlyphMode) string {
	g := presentation.GlyphsFor(mode)
	glyph, color := g.FeedGlyph, t.Accent2
	switch n.Kind {
	case KindRelease:
		glyph, color = g.NewGlyph, t.Accent
		if n.IsUpdate() {
			glyph = g.UpdateGlyph
		}
	case KindHeadsUp:
		glyph, color = g.HeadsUpGlyph, t.Working
	case KindTip:
		glyph, color = g.TipGlyph, t.Waiting
	}
	return lipgloss.NewStyle().Foreground(color).Render(glyph)
}

// listRow sets a line after the selection bar's column, on the surface fill
// when it is the selected notice's.
func listRow(line string, selected bool, width int, t Theme, mode presentation.GlyphMode) string {
	if !selected {
		return padRight("  "+line, width)
	}
	bar := lipgloss.NewStyle().Foreground(t.Accent).Render(presentation.GlyphsFor(mode).SectionBarGlyph)
	return t.Surface(bar+" "+line, width)
}

// besideList sets the message right of the list, a seam between them.
func besideList(list, message []string, t Theme) []string {
	seam := t.Border.Render("│") + strings.Repeat(" ", panePadding)
	rows := make([]string, max(len(list), len(message)))
	for i := range rows {
		left, right := "", ""
		if i < len(list) {
			left = list[i]
		}
		if i < len(message) {
			right = message[i]
		}
		rows[i] = padRight(left, listWidth) + seam + right
	}
	return rows
}

// unreadCount is the title strip's right side, empty when all is read.
func unreadCount(notices []Notice, t Theme) string {
	count := 0
	for _, n := range notices {
		if n.Unread {
			count++
		}
	}
	if count == 0 {
		return ""
	}
	return lipgloss.NewStyle().Foreground(t.Accent).Render(fmt.Sprintf(" %d unread ", count))
}
