package notices

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// modalMax is the widest row the modal makes room for, the longest line remote text may carry.
const modalMax = 160

// scrollbarWidth is the scrollbar's column and the gap before it.
const scrollbarWidth = 2

// proseWidth keeps a paragraph to a readable measure on a wide modal.
const proseWidth = 76

// modalInner is the modal content column's floor, sized for the welcome
// notice's longest body line; longer bodies widen it up to the terminal.
const modalInner = 62

var changeGroups = []struct {
	kind, label, one, many string
}{
	{KindFeature, "FEATURES", "feature", "features"},
	{KindFix, "FIXES", "fix", "fixes"},
	{KindOther, "OTHER", "other", "other"},
}

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
func bodyLayout(n Notice, inner, room int, t Theme) (body []string, scrolls bool) {
	body = renderBody(n, inner, t)
	if len(body) <= room {
		return body, false
	}
	return renderBody(n, max(inner-scrollbarWidth, 1), t), true
}

func scrollWindow(body []string, room, offset, inner int, t Theme) []string {
	lastOffset := len(body) - room
	offset = min(max(offset, 0), lastOffset)
	thumb := max(1, room*room/len(body))
	thumbTop := offset * (room - thumb) / lastOffset
	track := t.Subtle.Render("│")
	grip := lipgloss.NewStyle().Foreground(t.Accent).Render("┃")
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
	lines = append(lines, n.After...)
	ranged := len(n.Releases) > 1
	for index, release := range n.Releases {
		newest := index == len(n.Releases)-1
		if ranged {
			lines = append(lines, releaseHeading(release, newest))
		}
		for _, highlight := range release.Highlights {
			lines = append(lines, "• "+plainMarks(highlight))
		}
		switch {
		case newest || len(release.Highlights) == 0:
			for _, change := range release.Changes {
				lines = append(lines, measuredChange(change))
			}
		case len(release.Changes) > 0:
			lines = append(lines, changeCounts(release.Changes))
		}
		for _, thanks := range release.Thanks {
			lines = append(lines, "• "+thanks)
		}
	}
	return lines
}

func renderBody(n Notice, width int, t Theme) []string {
	var body []string
	if n.Headline != "" {
		body = appendWrapped(body, []textRun{{text: n.Headline}}, width, headingStyle(t), t)
		rule := 0
		for _, row := range body {
			rule = max(rule, lipgloss.Width(row))
		}
		body = append(body, lipgloss.NewStyle().Foreground(t.Accent).Render(strings.Repeat("━", rule)))
		if !n.ReleaseNotes {
			body = append(body, "")
		}
	}
	lead := t.Value
	if n.ReleaseNotes {
		lead = t.Muted
	}
	for _, line := range n.Body {
		body = appendWrapped(body, phraseRuns(line, n.Accent), width, lead, t)
	}
	ranged := len(n.Releases) > 1
	for index := len(n.Releases) - 1; index >= 0; index-- {
		newest := index == len(n.Releases)-1
		for _, section := range releaseSections(n.Releases[index], newest, ranged, width, t) {
			body = append(body, "")
			body = append(body, section...)
		}
	}
	if len(n.Releases) > 0 && !n.RangeComplete {
		body = appendWrapped(append(body, ""), []textRun{{text: "The local catalog covers part of this range; Enter opens the complete notes."}}, width, t.Subtle, t)
	}
	if len(n.After) > 0 {
		body = append(body, "")
		for _, line := range n.After {
			body = appendWrapped(body, []textRun{{text: line}}, width, t.Muted, t)
		}
	}
	return body
}

// releaseSections shows counts in place of an older release's changes, unless it has no highlights to stand for them.
func releaseSections(release Release, newest, ranged bool, width int, t Theme) [][]string {
	var sections [][]string
	var opening []string
	if ranged {
		opening = append(opening, headingStyle(t).Render(ansi.Truncate(releaseHeading(release, newest), width, "…")))
	}
	if newest && release.Summary != "" {
		opening = appendWrapped(opening, markedRuns(release.Summary), min(width, proseWidth), t.Value, t)
	}
	if len(opening) > 0 {
		sections = append(sections, opening)
	}
	if len(release.Highlights) > 0 {
		section := []string{label("HIGHLIGHTS", t)}
		mark := lipgloss.NewStyle().Foreground(t.Accent)
		for _, highlight := range release.Highlights {
			section = appendBullet(section, markedRuns(highlight), width, t.Value, mark, t)
		}
		sections = append(sections, section)
	}
	switch {
	case newest || len(release.Highlights) == 0:
		sections = append(sections, changeSections(release, width, t)...)
	case len(release.Changes) > 0:
		sections = append(sections, []string{t.Subtle.Render(changeCounts(release.Changes))})
	}
	if len(release.Thanks) > 0 {
		section := []string{label("THANK YOU", t)}
		for _, thanks := range release.Thanks {
			section = appendBullet(section, []textRun{{text: thanks}}, width, t.Muted, t.Muted, t)
		}
		sections = append(sections, section)
	}
	shown := len(release.Highlights) + len(release.Changes) + len(release.Thanks)
	if shown == 0 && !(newest && release.Summary != "") {
		sections = append(sections, []string{t.Subtle.Render("No summarized changes.")})
	}
	return sections
}

func releaseHeading(release Release, newest bool) string {
	if newest || release.Headline == "" {
		return release.Version
	}
	return release.Version + " · " + release.Headline
}

func changeSections(release Release, width int, t Theme) [][]string {
	var sections [][]string
	for _, group := range changeGroups {
		var rows []string
		count := 0
		for _, change := range release.Changes {
			if change.Kind != group.kind {
				continue
			}
			count++
			rows = appendChange(rows, change, width, t)
		}
		if count > 0 {
			sections = append(sections, append([]string{label(fmt.Sprintf("%s · %d", group.label, count), t)}, rows...))
		}
	}
	if omitted := release.TotalChanges - len(release.Changes); omitted > 0 && len(sections) > 0 {
		last := len(sections) - 1
		sections[last] = append(sections[last], t.Subtle.Render(fmt.Sprintf("  +%d more in the full notes", omitted)))
	}
	return sections
}

func changeCounts(changes []Change) string {
	var parts []string
	for _, group := range changeGroups {
		count := 0
		for _, change := range changes {
			if change.Kind == group.kind {
				count++
			}
		}
		switch {
		case count == 1:
			parts = append(parts, "1 "+group.one)
		case count > 1:
			parts = append(parts, fmt.Sprintf("%d %s", count, group.many))
		}
	}
	return strings.Join(parts, " · ")
}

// appendChange sets the author at the right edge of the last wrapped row, or of a row of its own when two spaces do not fit.
func appendChange(rows []string, change Change, width int, t Theme) []string {
	rows = appendBullet(rows, []textRun{{text: change.Text}}, width, t.Muted, t.Muted, t)
	if change.Author == "" {
		return rows
	}
	author := lipgloss.NewStyle().Foreground(t.Accent2).Render(change.Author)
	authorWidth := lipgloss.Width(change.Author)
	last := len(rows) - 1
	if gap := width - lipgloss.Width(rows[last]) - authorWidth; gap >= 2 {
		rows[last] += strings.Repeat(" ", gap) + author
		return rows
	}
	return append(rows, strings.Repeat(" ", max(width-authorWidth, 0))+author)
}

func measuredChange(change Change) string {
	if change.Author == "" {
		return "• " + change.Text
	}
	return "• " + change.Text + "  " + change.Author
}

func label(text string, t Theme) string {
	return lipgloss.NewStyle().Foreground(t.SubtleColor).Bold(true).Render(text)
}

func headingStyle(t Theme) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(t.Bright).Bold(true)
}

func appendBullet(rows []string, runs []textRun, width int, base, mark lipgloss.Style, t Theme) []string {
	for index, line := range wrapRuns(runs, max(width-2, 1)) {
		prefix := "  "
		if index == 0 {
			prefix = mark.Render("• ")
		}
		rows = append(rows, prefix+renderRuns(line, base, t.Key))
	}
	return rows
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

// plainMarks drops the accent marks release text carries.
func plainMarks(text string) string {
	return strings.ReplaceAll(text, "`", "")
}
