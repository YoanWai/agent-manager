package ui

import (
	"fmt"
	"strings"

	"github.com/YoanWai/agent-manager/internal/update"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// noticeModalMax is the widest the content column grows, the longest line remote text may carry.
const noticeModalMax = 160

var changeGroups = []struct {
	kind, label, one, many string
}{
	{update.KindFeature, "FEATURES", "feature", "features"},
	{update.KindFix, "FIXES", "fix", "fixes"},
	{update.KindOther, "OTHER", "other", "other"},
}

func noticeInnerWidth(notices []notice, terminalWidth int) int {
	inner := noticeModalInner
	for _, n := range notices {
		for _, line := range noticeMeasure(n) {
			if width := lipgloss.Width(line); width > inner {
				inner = width
			}
		}
	}
	inner = min(inner, noticeModalMax)
	if fit := terminalWidth - 8; inner > fit {
		inner = max(fit, 1)
	}
	return inner
}

// noticeMeasure leaves the summary out: a paragraph wraps to the modal and must never widen it.
func noticeMeasure(n notice) []string {
	lines := append([]string{n.headline}, n.body...)
	lines = append(lines, n.after...)
	for _, release := range n.releases {
		for _, highlight := range release.Highlights {
			lines = append(lines, "• "+plainMarks(highlight))
		}
		for _, change := range release.Changes {
			lines = append(lines, "• "+changeRow(change))
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
		headline := ansi.Truncate(n.headline, width, "…")
		body = append(body,
			lipgloss.NewStyle().Foreground(colorBright).Bold(true).Render(headline),
			lipgloss.NewStyle().Foreground(colorAccent).Render(strings.Repeat("━", lipgloss.Width(headline))))
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
		body = append(body, "", subtleStyle.Render("The local catalog covers part of this range; Enter opens the complete notes."))
	}
	if len(n.after) > 0 {
		body = append(body, "")
		for _, line := range n.after {
			body = appendWrapped(body, []textRun{{text: line}}, width, mutedStyle)
		}
	}
	return body
}

// An older release shows counts in place of its changes, unless it has no highlights to stand for them.
func releaseSections(release update.Release, newest, ranged bool, width int) [][]string {
	var sections [][]string
	var opening []string
	if ranged {
		heading := release.Version
		if !newest && release.Headline != "" {
			heading += " · " + release.Headline
		}
		opening = append(opening, lipgloss.NewStyle().Foreground(colorBright).Bold(true).Render(ansi.Truncate(heading, width, "…")))
	}
	if newest && release.Summary != "" {
		opening = appendWrapped(opening, markedRuns(release.Summary), width, valueStyle)
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

// A body too narrow to set the author at the right edge keeps the credit beside the text.
func appendChange(rows []string, change update.Change, width int) []string {
	text := "• " + change.Text
	gap := width - lipgloss.Width(text) - lipgloss.Width(change.Author)
	if change.Author == "" || gap < 2 {
		return appendBullet(rows, []textRun{{text: changeRow(change)}}, width, mutedStyle, mutedStyle)
	}
	author := lipgloss.NewStyle().Foreground(colorAccent2).Render(change.Author)
	return append(rows, mutedStyle.Render(text)+strings.Repeat(" ", gap)+author)
}

func noticeLabel(label string) string {
	return lipgloss.NewStyle().Foreground(colorSubtle).Bold(true).Render(label)
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
