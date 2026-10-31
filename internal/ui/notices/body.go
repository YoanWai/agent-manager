package notices

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/lipgloss"
)

// modalMax is the widest row the modal makes room for, the longest line remote text may carry.
const modalMax = 160

const scrollbarWidth = 2

// proseWidth keeps a paragraph to a readable measure on a wide modal.
const proseWidth = 76

// modalInner is the modal content column's floor, sized for the welcome
// notice's longest body line; longer bodies widen it up to the terminal.
const modalInner = 62

func innerWidth(notices []Notice, terminalWidth int) int {
	widest := 0
	for _, n := range notices {
		for _, line := range measure(n) {
			widest = max(widest, lipgloss.Width(line))
		}
	}
	inner := min(max(widest+scrollbarWidth, modalInner), modalMax+scrollbarWidth)
	if fit := terminalWidth - 8; inner > fit {
		inner = max(fit, 1)
	}
	return inner
}

// bodyLayout gives a body that overflows narrower rows, leaving room for the scrollbar.
func bodyLayout(n Notice, inner, room int, t Theme, mode presentation.GlyphMode) (body []string, scrolls bool) {
	body = renderBody(n, inner, t, mode)
	if len(body) <= room {
		return body, false
	}
	return renderBody(n, max(inner-scrollbarWidth, 1), t, mode), true
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

// measure leaves the summary out: a paragraph wraps to the modal and must never widen it.
func measure(n Notice) []string {
	lines := append([]string{n.Headline}, n.Body...)
	for _, line := range n.After {
		lines = append(lines, plainMarks(line))
	}
	if n.ReleaseNotes {
		lines = append(lines, releaseMeasure(n)...)
	}
	return lines
}

func renderBody(n Notice, width int, t Theme, mode presentation.GlyphMode) []string {
	var body []string
	if n.Headline != "" {
		body = appendWrapped(body, []textRun{{text: n.Headline}}, width, headingStyle(t), t)
		rule := 0
		for _, row := range body {
			rule = max(rule, lipgloss.Width(row))
		}
		body = append(body, lipgloss.NewStyle().Foreground(t.Accent).Render(strings.Repeat(presentation.ChromeFor(mode).RuleGlyph, rule)), "")
	}
	for _, line := range n.Body {
		body = appendWrapped(body, phraseRuns(line, n.Accent), width, t.Value, t)
	}
	if len(n.After) > 0 {
		body = append(body, "")
		for _, line := range n.After {
			body = appendWrapped(body, markedRuns(line), width, t.Muted, t)
		}
	}
	return body
}

func headingStyle(t Theme) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(t.Bright).Bold(true)
}

func appendWrapped(rows []string, runs []textRun, width int, base lipgloss.Style, t Theme) []string {
	lines := wrapRuns(runs, width)
	if len(lines) == 0 {
		return append(rows, "")
	}
	for _, line := range lines {
		rows = append(rows, renderRuns(line, base, t.Key))
	}
	return rows
}

func plainMarks(text string) string {
	return strings.ReplaceAll(text, "`", "")
}
