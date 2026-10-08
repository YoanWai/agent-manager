package ui

import (
	"fmt"
	"strings"

	"github.com/YoanWai/agent-manager/internal/update"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// noticeModalMax is the widest row the modal makes room for, the longest line remote text may carry.
const noticeModalMax = 160

const noticeWheelRows = 3

// noticeScrollbarWidth is the scrollbar's column and the gap before it.
const noticeScrollbarWidth = 2

// noticeProseWidth keeps a paragraph to a readable measure on a wide modal.
const noticeProseWidth = 76

var changeGroups = []struct {
	kind, label, one, many string
}{
	{update.KindFeature, "FEATURES", "feature", "features"},
	{update.KindFix, "FIXES", "fix", "fixes"},
	{update.KindOther, "OTHER", "other", "other"},
}

func noticeInnerWidth(notices []notice, terminalWidth int) int {
	widest := 0
	for _, n := range notices {
		for _, line := range noticeMeasure(n) {
			widest = max(widest, lipgloss.Width(line))
		}
	}
	inner := min(max(widest+noticeScrollbarWidth, noticeModalInner), noticeModalMax+noticeScrollbarWidth)
	if fit := terminalWidth - 8; inner > fit {
		inner = max(fit, 1)
	}
	return inner
}

// noticeBodyLayout gives a body that overflows narrower rows, leaving room for the scrollbar.
func noticeBodyLayout(n notice, inner, room int) (body []string, scrolls bool) {
	body = renderNoticeBody(n, inner)
	if len(body) <= room {
		return body, false
	}
	return renderNoticeBody(n, max(inner-noticeScrollbarWidth, 1)), true
}

func noticeScrollWindow(body []string, room, offset, inner int) []string {
	lastOffset := len(body) - room
	offset = min(max(offset, 0), lastOffset)
	thumb := max(1, room*room/len(body))
	thumbTop := offset * (room - thumb) / lastOffset
	track := subtleStyle.Render("│")
	grip := lipgloss.NewStyle().Foreground(colorAccent).Render("┃")
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

// noticeMeasure leaves the summary out: a paragraph wraps to the modal and must never widen it.
func noticeMeasure(n notice) []string {
	lines := append([]string{n.headline}, n.body...)
	lines = append(lines, n.after...)
	ranged := len(n.releases) > 1
	for index, release := range n.releases {
		newest := index == len(n.releases)-1
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

func renderNoticeBody(n notice, width int) []string {
	var body []string
	if n.headline != "" {
		body = appendWrapped(body, []textRun{{text: n.headline}}, width, noticeHeadingStyle())
		rule := 0
		for _, row := range body {
			rule = max(rule, lipgloss.Width(row))
		}
		body = append(body, lipgloss.NewStyle().Foreground(colorAccent).Render(strings.Repeat("━", rule)))
		if !n.releaseNotes {
			body = append(body, "")
		}
	}
	lead := valueStyle
	if n.releaseNotes {
		lead = mutedStyle
	}
	for _, line := range n.body {
		body = appendWrapped(body, phraseRuns(line, n.accent), width, lead)
	}
	ranged := len(n.releases) > 1
	for index := len(n.releases) - 1; index >= 0; index-- {
		newest := index == len(n.releases)-1
		for _, section := range releaseSections(n.releases[index], newest, ranged, width) {
			body = append(body, "")
			body = append(body, section...)
		}
	}
	if len(n.releases) > 0 && !n.rangeComplete {
		body = appendWrapped(append(body, ""), []textRun{{text: "The local catalog covers part of this range; Enter opens the complete notes."}}, width, subtleStyle)
	}
	if len(n.after) > 0 {
		body = append(body, "")
		for _, line := range n.after {
			body = appendWrapped(body, []textRun{{text: line}}, width, mutedStyle)
		}
	}
	return body
}

// releaseSections shows counts in place of an older release's changes, unless it has no highlights to stand for them.
func releaseSections(release update.Release, newest, ranged bool, width int) [][]string {
	var sections [][]string
	var opening []string
	if ranged {
		opening = append(opening, noticeHeadingStyle().Render(ansi.Truncate(releaseHeading(release, newest), width, "…")))
	}
	if newest && release.Summary != "" {
		opening = appendWrapped(opening, markedRuns(release.Summary), min(width, noticeProseWidth), valueStyle)
	}
	if len(opening) > 0 {
		sections = append(sections, opening)
	}
	if len(release.Highlights) > 0 {
		section := []string{noticeLabel("HIGHLIGHTS")}
		mark := lipgloss.NewStyle().Foreground(colorAccent)
		for _, highlight := range release.Highlights {
			section = appendBullet(section, markedRuns(highlight), width, valueStyle, mark)
		}
		sections = append(sections, section)
	}
	switch {
	case newest || len(release.Highlights) == 0:
		sections = append(sections, changeSections(release, width)...)
	case len(release.Changes) > 0:
		sections = append(sections, []string{subtleStyle.Render(changeCounts(release.Changes))})
	}
	if len(release.Thanks) > 0 {
		section := []string{noticeLabel("THANK YOU")}
		for _, thanks := range release.Thanks {
			section = appendBullet(section, []textRun{{text: thanks}}, width, mutedStyle, mutedStyle)
		}
		sections = append(sections, section)
	}
	shown := len(release.Highlights) + len(release.Changes) + len(release.Thanks)
	if shown == 0 && !(newest && release.Summary != "") {
		sections = append(sections, []string{subtleStyle.Render("No summarized changes.")})
	}
	return sections
}

func releaseHeading(release update.Release, newest bool) string {
	if newest || release.Headline == "" {
		return release.Version
	}
	return release.Version + " · " + release.Headline
}

func changeSections(release update.Release, width int) [][]string {
	var sections [][]string
	for _, group := range changeGroups {
		var rows []string
		count := 0
		for _, change := range release.Changes {
			if change.Kind != group.kind {
				continue
			}
			count++
			rows = appendChange(rows, change, width)
		}
		if count > 0 {
			sections = append(sections, append([]string{noticeLabel(fmt.Sprintf("%s · %d", group.label, count))}, rows...))
		}
	}
	if omitted := release.TotalChanges - len(release.Changes); omitted > 0 && len(sections) > 0 {
		last := len(sections) - 1
		sections[last] = append(sections[last], subtleStyle.Render(fmt.Sprintf("  +%d more in the full notes", omitted)))
	}
	return sections
}

func changeCounts(changes []update.Change) string {
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
func appendChange(rows []string, change update.Change, width int) []string {
	rows = appendBullet(rows, []textRun{{text: change.Text}}, width, mutedStyle, mutedStyle)
	if change.Author == "" {
		return rows
	}
	author := lipgloss.NewStyle().Foreground(colorAccent2).Render(change.Author)
	authorWidth := lipgloss.Width(change.Author)
	last := len(rows) - 1
	if gap := width - lipgloss.Width(rows[last]) - authorWidth; gap >= 2 {
		rows[last] += strings.Repeat(" ", gap) + author
		return rows
	}
	return append(rows, strings.Repeat(" ", max(width-authorWidth, 0))+author)
}

func measuredChange(change update.Change) string {
	if change.Author == "" {
		return "• " + change.Text
	}
	return "• " + change.Text + "  " + change.Author
}

func noticeLabel(label string) string {
	return lipgloss.NewStyle().Foreground(colorSubtle).Bold(true).Render(label)
}

func noticeHeadingStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(colorBright).Bold(true)
}

func appendBullet(rows []string, runs []textRun, width int, base, mark lipgloss.Style) []string {
	for index, line := range wrapRuns(runs, max(width-2, 1)) {
		prefix := "  "
		if index == 0 {
			prefix = mark.Render("• ")
		}
		rows = append(rows, prefix+renderRuns(line, base))
	}
	return rows
}

func appendWrapped(rows []string, runs []textRun, width int, base lipgloss.Style) []string {
	lines := wrapRuns(runs, width)
	if len(lines) == 0 {
		return append(rows, "")
	}
	for _, line := range lines {
		rows = append(rows, renderRuns(line, base))
	}
	return rows
}
