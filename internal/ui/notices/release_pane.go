package notices

import (
	"strconv"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Tab is the part of a release the pane shows under its header.
type Tab uint8

const (
	TabOverview Tab = iota
	TabChanges
	TabThanks
)

var tabLabels = [...]string{TabOverview: "Overview", TabChanges: "Changes", TabThanks: "Thanks"}

// changeFilters is the order f steps the Changes list through; "" is all.
var changeFilters = []string{"", KindFeature, KindFix, KindOther}

// displayZone is where a release date is read: the viewer's own clock.
var displayZone = time.Local

// paneHit is a tab or a filter chip the header painted: a click on its
// line between x0 and x1 shows that tab with that filter.
type paneHit struct {
	line, x0, x1 int
	tab          Tab
	filter       string
}

func releasePane(n Notice, tab Tab, filter string, width int, t Theme, mode presentation.GlyphMode) []string {
	head, _ := releaseHead(n, tab, filter, width, t, mode)
	return append(head, releaseTab(n, tab, filter, width, t, mode)...)
}

// releaseHead is what stays above the scrolling tab: the version, the
// headline, the update bar when one is on offer, the tabs and, on Changes,
// the filter chips.
func releaseHead(n Notice, tab Tab, filter string, width int, t Theme, mode presentation.GlyphMode) ([]string, []paneHit) {
	rows := []string{versionLine(n, width, t, mode)}
	if n.Headline != "" {
		rows = appendWrapped(rows, []textRun{{text: n.Headline}}, width, headingStyle(t), t)
	}
	if n.IsUpdate() {
		rows = append(rows, callToAction(n, width, t, mode))
	}
	rows = append(rows, "")
	strip, under, hits := tabStrip(n, tab, filter, t, mode)
	for i := range hits {
		hits[i].line = len(rows)
	}
	rows = append(rows, strip, under)
	if tab == TabChanges && changeCount(n) > 0 {
		chips, chipHits := filterChips(n, filter, t, mode)
		for i := range chipHits {
			chipHits[i].line = len(rows) + 1
		}
		rows = append(rows, "", chips)
		hits = append(hits, chipHits...)
	}
	return append(rows, ""), hits
}

func releaseTab(n Notice, tab Tab, filter string, width int, t Theme, mode presentation.GlyphMode) []string {
	switch tab {
	case TabChanges:
		return changesTab(n, filter, width, t, mode)
	case TabThanks:
		return thanksTab(n, width, t, mode)
	}
	return overviewTab(n, width, t, mode)
}

func versionLine(n Notice, width int, t Theme, mode presentation.GlyphMode) string {
	line := chip("v"+bareVersion(n.Version), t.Accent, t) + " " + t.Muted.Render(origin(n))
	if n.URL == "" {
		return line
	}
	link := t.Muted.Render(presentation.KeyLabel(mode, "↵") + " notes on GitHub")
	if gap := width - lipgloss.Width(line) - lipgloss.Width(link); gap >= 2 {
		line += strings.Repeat(" ", gap) + link
	}
	return line
}

// origin is when the release came out, or how many it spans, and the
// version this install is on.
func origin(n Notice) string {
	var parts []string
	switch {
	case len(n.Releases) > 1:
		parts = append(parts, releaseCountLabel(ReleaseRange{Releases: n.Releases, Complete: n.RangeComplete})+" releases")
	case n.IsUpdate():
		parts = append(parts, "out now")
	case len(n.Releases) == 1 && !n.Releases[0].PublishedAt.IsZero():
		parts = append(parts, n.Releases[0].PublishedAt.In(displayZone).Format("Jan 2"))
	}
	if n.From != "" {
		on := "you were on "
		if n.IsUpdate() {
			on = "you're on "
		}
		parts = append(parts, on+bareVersion(n.From))
	}
	return strings.Join(parts, " · ")
}

func bareVersion(version string) string { return strings.TrimPrefix(version, "v") }

func callToAction(n Notice, width int, t Theme, mode presentation.GlyphMode) string {
	return actionBar("u", "Update to "+bareVersion(n.Version)+" and restart", "", width, t, mode)
}

// chip is a label on a fill, in the backdrop's ink.
func chip(text string, fill lipgloss.Color, t Theme) string {
	return lipgloss.NewStyle().Background(fill).Foreground(t.Ink).Bold(true).Render(" " + text + " ")
}

// actionBar is a call to action on the faint accent fill: the key, what it
// does and, dim, where it leads, cut to the prose measure.
func actionBar(key, label, detail string, width int, t Theme, mode presentation.GlyphMode) string {
	room := min(width, proseWidth)
	bar := lipgloss.NewStyle().Foreground(t.Accent).Render(presentation.GlyphsFor(mode).SectionBarGlyph)
	row := bar + " " + t.Key.Render(key) + "  " + t.Value.Render(label)
	if detail != "" {
		row += "  " + t.Muted.Render(ansi.Truncate(detail, max(room-lipgloss.Width(row)-2, 0), "…"))
	}
	return t.Callout(row, room)
}

// tabStrip names each tab with its count, the active one bright over an
// accent underline.
func tabStrip(n Notice, active Tab, filter string, t Theme, mode presentation.GlyphMode) (strip, under string, hits []paneHit) {
	counts := [...]int{TabChanges: changeCount(n), TabThanks: len(mergedThanks(n.Releases))}
	underline := lipgloss.NewStyle().Foreground(t.Accent)
	x := 0
	for index, label := range tabLabels {
		tab := Tab(index)
		if index > 0 {
			strip, under, x = strip+"   ", under+"   ", x+3
		}
		style, text := t.Muted, label
		if tab == active {
			style = headingStyle(t)
		}
		cell := style.Render(label)
		if tab != TabOverview {
			count := strconv.Itoa(counts[tab])
			cell += " " + t.Muted.Render(count)
			text += " " + count
		}
		cells := ansi.StringWidth(text)
		strip += cell
		if tab == active {
			under += underline.Render(strings.Repeat(presentation.ChromeFor(mode).RuleGlyph, cells))
		} else {
			under += strings.Repeat(" ", cells)
		}
		hits = append(hits, paneHit{x0: x, x1: x + cells, tab: tab, filter: filter})
		x += cells
	}
	return strip, under, hits
}

func filterChips(n Notice, active string, t Theme, mode presentation.GlyphMode) (string, []paneHit) {
	var row string
	var hits []paneHit
	x := 0
	for index, filter := range changeFilters {
		if index > 0 {
			row, x = row+"  ", x+2
		}
		style := t.Muted
		if filter == active {
			style = headingStyle(t)
		}
		chip, text := style.Render("All"), "All"
		if filter != "" {
			count := strconv.Itoa(kindCount(n, filter))
			chip, text = changeMark(filter, t, mode)+" "+style.Render(count), "x "+count
		}
		cells := ansi.StringWidth(text) + 2
		if filter == active {
			row += t.Surface(" "+chip+" ", cells)
		} else {
			row += " " + chip + " "
		}
		hits = append(hits, paneHit{x0: x, x1: x + cells, tab: TabChanges, filter: filter})
		x += cells
	}
	return row, hits
}

// changeMark is a change kind's glyph in its color: finished green for a
// feature, working orange for a fix, dim for the rest.
func changeMark(kind string, t Theme, mode presentation.GlyphMode) string {
	g := presentation.GlyphsFor(mode)
	switch kind {
	case KindFeature:
		return lipgloss.NewStyle().Foreground(t.Finished).Render(g.FeatureGlyph)
	case KindFix:
		return lipgloss.NewStyle().Foreground(t.Working).Render(g.FixGlyph)
	}
	return t.Muted.Render(g.OtherGlyph)
}

func changeCount(n Notice) int {
	count := 0
	for _, release := range n.Releases {
		count += len(release.Changes)
	}
	return count
}

func kindCount(n Notice, kind string) int {
	count := 0
	for _, release := range n.Releases {
		for _, change := range release.Changes {
			if change.Kind == kind {
				count++
			}
		}
	}
	return count
}

func plural(count int, one, many string) string {
	if count == 1 {
		return "1 " + one
	}
	return strconv.Itoa(count) + " " + many
}

func groupLabel(text string, t Theme) string { return t.Muted.Bold(true).Render(text) }

// spaced appends more after a blank row, when rows already holds something.
func spaced(rows []string, more ...string) []string {
	if len(rows) > 0 {
		rows = append(rows, "")
	}
	return append(rows, more...)
}

// hanging wraps runs after a first-row lead indent cells wide, the later
// rows indented under the text.
func hanging(lead string, indent int, runs []textRun, width int, base, accent lipgloss.Style) []string {
	lines := wrapRuns(runs, max(width-indent, 1))
	if len(lines) == 0 {
		return []string{lead}
	}
	rows := make([]string, len(lines))
	for i, line := range lines {
		prefix := strings.Repeat(" ", indent)
		if i == 0 {
			prefix = lead
		}
		rows[i] = prefix + renderRuns(line, base, accent)
	}
	return rows
}

// besideColumn sets head in a column col cells wide with runs wrapped
// beside it; a head wider than the column takes a row of its own.
func besideColumn(head string, col int, runs []textRun, width int, base, accent lipgloss.Style) []string {
	indent := col + 2
	switch {
	case len(runs) == 0:
		return []string{head}
	case lipgloss.Width(head) > col:
		return append([]string{head}, hanging(strings.Repeat(" ", indent), indent, runs, width, base, accent)...)
	}
	return hanging(padRight(head, col)+"  ", indent, runs, width, base, accent)
}

// trailing sets text at the right edge of the last row, or of a row of its
// own when two spaces do not fit beside it.
func trailing(rows []string, text string, style lipgloss.Style, width int) []string {
	if text == "" {
		return rows
	}
	cells := ansi.StringWidth(text)
	last := len(rows) - 1
	if gap := width - lipgloss.Width(rows[last]) - cells; gap >= 2 {
		rows[last] += strings.Repeat(" ", gap) + style.Render(text)
		return rows
	}
	return append(rows, strings.Repeat(" ", max(width-cells, 0))+style.Render(text))
}

// flow lays parts out left to right, starting a row where the next would
// not fit.
func flow(parts []string, gap string, width int) []string {
	var rows []string
	row := ""
	for _, part := range parts {
		switch {
		case row == "":
			row = part
		case lipgloss.Width(row)+len(gap)+lipgloss.Width(part) > width:
			rows, row = append(rows, row), part
		default:
			row += gap + part
		}
	}
	if row != "" {
		rows = append(rows, row)
	}
	return rows
}

func appendPartial(rows []string, n Notice, width int, t Theme, mode presentation.GlyphMode) []string {
	if n.RangeComplete || len(n.Releases) == 0 {
		return rows
	}
	note := "The local catalog covers part of this range; " + presentation.KeyLabel(mode, "↵") + " opens the complete notes."
	return appendWrapped(spaced(rows), []textRun{{text: note}}, width, t.Muted, t)
}

// releaseMeasure is a release notice's rows as they would read unwrapped,
// for the modal's width.
func releaseMeasure(n Notice) []string {
	var lines []string
	for _, release := range n.Releases {
		lines = append(lines, release.Version+" · "+release.Headline)
		for _, highlight := range release.Highlights {
			lines = append(lines, "◆ "+highlight.Label+"  "+plainMarks(highlight.Text))
		}
		for _, change := range release.Changes {
			lines = append(lines, "✚ "+change.Scope+"  "+change.Text+"  "+change.Author)
		}
	}
	for _, person := range mergedThanks(n.Releases) {
		lines = append(lines, person.Handle+"  "+person.Text+"  "+strings.Join(person.Refs, " "))
	}
	return lines
}
