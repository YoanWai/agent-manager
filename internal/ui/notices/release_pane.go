package notices

import (
	"strconv"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
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

// paneHit is a tab, a filter chip or the update bar the header painted: a
// click on its line between x0 and x1 shows that tab with that filter, or
// runs the action.
type paneHit struct {
	line, x0, x1 int
	tab          Tab
	filter       string
	action       Action
}

func releasePane(n Notice, tab Tab, filter string, width int, t Theme, mode presentation.GlyphMode) ([]string, []paneHit) {
	head, hits := releaseHead(n, tab, filter, width, t, mode)
	return append(head, releaseTab(n, tab, filter, width, t, mode)...), hits
}

// releaseHead is what stays above the scrolling tab: the version, the
// headline, the update bar when one is on offer, the tabs and, on Changes,
// the filter chips.
func releaseHead(n Notice, tab Tab, filter string, width int, t Theme, mode presentation.GlyphMode) ([]string, []paneHit) {
	version := versionLine(n, width, t, mode)
	rows := []string{version, ""}
	var hits []paneHit
	if link := lipgloss.Width(notesLink(t, mode)); lipgloss.Width(version) == width && !n.IsUpdate() {
		hits = append(hits, paneHit{x0: width - link, x1: width, action: OpenLink})
	}
	if n.Headline != "" {
		rows = append(appendWrapped(rows, []textRun{{text: n.Headline}}, width, headingStyle(t), t), "")
	}
	if n.IsUpdate() {
		bar, cells := callToAction(n, width, t, mode)
		hits = append(hits, paneHit{line: len(rows), x1: cells, action: StartUpdate})
		if link := lipgloss.Width(notesLink(t, mode)); n.URL != "" && cells+2+link <= width-1 {
			hits = append(hits, paneHit{line: len(rows), x0: width - 1 - link, x1: width - 1, action: OpenLink})
		}
		rows = append(rows, bar, "")
	}
	strip, under, tabHits := tabStrip(n, tab, filter, width, t, mode)
	for i := range tabHits {
		tabHits[i].line = len(rows)
	}
	hits = append(hits, tabHits...)
	rows = append(rows, strip, under)
	if tab == TabChanges && changeCount(n) > 0 {
		chips, chipHits := filterChips(n, filter, width, t, mode)
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

// versionLine is the version chip, when it came out and what this install
// is on, and the notes link at the right; an available release moves the
// link into its update bar.
func versionLine(n Notice, width int, t Theme, mode presentation.GlyphMode) string {
	line := chip("v"+bareVersion(n.Version), t.Accent, t) + "  " + t.Muted.Render(origin(n))
	if n.URL == "" || n.IsUpdate() {
		return line
	}
	return atRight(line, notesLink(t, mode), width)
}

func notesLink(t Theme, mode presentation.GlyphMode) string {
	return keyChip("↵", t, mode) + t.Muted.Render(" notes on GitHub")
}

func keyChip(key string, t Theme, mode presentation.GlyphMode) string {
	label := " " + presentation.KeyLabel(mode, key) + " "
	return t.Raised(lipgloss.NewStyle().Foreground(t.Bright).Render(label), ansi.StringWidth(label))
}

// atRight sets right at the row's right edge, when two spaces still fit
// before it.
func atRight(row, right string, width int) string {
	if gap := width - lipgloss.Width(row) - lipgloss.Width(right); gap >= 2 {
		return row + strings.Repeat(" ", gap) + right
	}
	return row
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

func callToAction(n Notice, width int, t Theme, mode presentation.GlyphMode) (string, int) {
	right := ""
	if n.URL != "" {
		right = notesLink(t, mode)
	}
	return actionBar("u", "Update to "+bareVersion(n.Version)+" and restart", right, t.Accent, width, t, mode)
}

// chip is a label on a fill, in whichever of the backdrop's ink and the bright text reads clearer on it.
func chip(text string, fill lipgloss.Color, t Theme) string {
	return lipgloss.NewStyle().Background(fill).Foreground(chipInk(fill, t)).Bold(true).Render(" " + text + " ")
}

func chipInk(fill lipgloss.Color, t Theme) lipgloss.Color {
	if uitheme.ContrastRatio(string(t.Bright), string(fill)) > uitheme.ContrastRatio(string(t.Ink), string(fill)) {
		return t.Bright
	}
	return t.Ink
}

// actionBar is a call to action across the pane on a fill tinted toward
// tone: a bar and the key in that tone, what it does, and at the right
// where it leads. cells is how far the key and what it does reach.
func actionBar(key, label, right string, tone lipgloss.Color, width int, t Theme, mode presentation.GlyphMode) (row string, cells int) {
	bar := lipgloss.NewStyle().Foreground(tone).Render(presentation.GlyphsFor(mode).SectionBarGlyph)
	row = bar + " " + chip(" "+presentation.KeyLabel(mode, key)+" ", tone, t) + "  " + headingStyle(t).Render(label)
	cells = lipgloss.Width(row)
	if right != "" {
		row = atRight(row, ansi.Truncate(right, max(width-cells-3, 0), "…"), width-1)
	}
	return t.Callout(row, width, tone), cells
}

// tabStrip names each tab with its count over a rule across the pane, the
// active tab bright with the rule in the accent under it.
func tabStrip(n Notice, active Tab, filter string, width int, t Theme, mode presentation.GlyphMode) (strip, under string, hits []paneHit) {
	counts := [...]int{TabChanges: changeCount(n), TabThanks: len(mergedThanks(n.Releases))}
	underline := lipgloss.NewStyle().Foreground(t.Accent)
	x := 0
	for index, label := range tabLabels {
		tab := Tab(index)
		lead := 2
		if index == 0 {
			lead = 1
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
		cells := lead + ansi.StringWidth(text) + 2
		strip += strings.Repeat(" ", lead) + cell + "  "
		if tab == active {
			under += underline.Render(strings.Repeat(presentation.ChromeFor(mode).RuleGlyph, cells))
		} else {
			under += t.Subtle.Render(strings.Repeat("─", cells))
		}
		hits = append(hits, paneHit{x0: x, x1: x + cells, tab: tab, filter: filter})
		x += cells
	}
	return strip, under + t.Subtle.Render(strings.Repeat("─", max(width-x, 0))), hits
}

// filterChips is a chip per filter, each count in its kind's color and the
// active chip raised and bright, with the key that steps them at the right.
func filterChips(n Notice, active string, width int, t Theme, mode presentation.GlyphMode) (string, []paneHit) {
	var row string
	var hits []paneHit
	x := 0
	for index, filter := range changeFilters {
		if index > 0 {
			row, x = row+" ", x+1
		}
		style, fill := kindStyle(filter, t), t.Surface
		if filter == active {
			style, fill = headingStyle(t), t.Raised
		}
		label, text := style.Render("All"), "All"
		if filter != "" {
			count := strconv.Itoa(kindCount(n, filter))
			label, text = changeMark(filter, t, mode)+" "+style.Render(count), "x "+count
		}
		cells := ansi.StringWidth(text) + 2
		row += fill(" "+label+" ", cells)
		hits = append(hits, paneHit{x0: x, x1: x + cells, tab: TabChanges, filter: filter})
		x += cells
	}
	return atRight(row, keyChip("f", t, mode)+t.Muted.Render(" filter"), width), hits
}

func kindStyle(kind string, t Theme) lipgloss.Style {
	switch kind {
	case KindFeature:
		return lipgloss.NewStyle().Foreground(t.Finished)
	case KindFix:
		return lipgloss.NewStyle().Foreground(t.Working)
	}
	return t.Muted
}

func changeMark(kind string, t Theme, mode presentation.GlyphMode) string {
	return kindStyle(kind, t).Render(changeGlyph(kind, mode))
}

func changeGlyph(kind string, mode presentation.GlyphMode) string {
	g := presentation.GlyphsFor(mode)
	switch kind {
	case KindFeature:
		return g.FeatureGlyph
	case KindFix:
		return g.FixGlyph
	}
	return g.OtherGlyph
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
