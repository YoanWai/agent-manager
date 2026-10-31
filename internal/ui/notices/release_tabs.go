package notices

import (
	"fmt"
	"slices"
	"strings"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var changeGroups = []struct {
	kind, one, many, none string
}{
	{KindFeature, "feature", "features", "No features."},
	{KindFix, "fix", "fixes", "No fixes."},
	{KindOther, "under the hood", "under the hood", "Nothing under the hood."},
}

var thanksGroups = []struct {
	group, label string
}{
	{ThanksBuilt, "BUILT IT"},
	{ThanksFound, "FOUND IT"},
	{ThanksStarted, "STARTED SOMETHING"},
}

// Column caps keep a long label, scope or handle from squeezing the text
// beside it.
const (
	labelColumnMax  = 24
	scopeColumnMax  = 14
	handleColumnMax = 20
)

// overviewTab is the newest release's summary and highlights, each older
// release in the range as a heading with its highlights and counts, then
// the counts across the range.
func overviewTab(n Notice, width int, t Theme, mode presentation.GlyphMode) []string {
	var rows []string
	for _, line := range n.Body {
		rows = appendWrapped(rows, []textRun{{text: line}}, width, t.Value, t)
	}
	if len(n.Releases) == 0 {
		return rows
	}
	newest := n.Releases[len(n.Releases)-1]
	if newest.Summary != "" {
		rows = appendWrapped(rows, markedRuns(newest.Summary), min(width, proseWidth), t.Value, t)
	}
	if len(newest.Highlights) > 0 {
		heading := "WHAT'S NEW"
		if n.IsUpdate() {
			heading = "WHAT YOU GET"
		}
		rows = spaced(rows, groupLabel(heading, t))
		rows = append(rows, highlightRows(newest.Highlights, width, t, mode)...)
	}
	for index := len(n.Releases) - 2; index >= 0; index-- {
		release := n.Releases[index]
		heading := release.Version
		if release.Headline != "" {
			heading += " · " + release.Headline
		}
		rows = appendWrapped(spaced(rows), []textRun{{text: heading}}, width, headingStyle(t), t)
		rows = append(rows, highlightRows(release.Highlights, width, t, mode)...)
		if counts := changeCounts(release.Changes); counts != "" {
			rows = append(rows, t.Muted.Render(counts))
		}
	}
	rows = appendPartial(rows, n, width, t, mode)
	return spaced(rows, countChips(n, width, t, mode)...)
}

// highlightRows sets each label in one column so the details line up; an
// unlabelled highlight wraps under its bullet.
func highlightRows(highlights []Highlight, width int, t Theme, mode presentation.GlyphMode) []string {
	labelColumn := 0
	for _, highlight := range highlights {
		labelColumn = max(labelColumn, ansi.StringWidth(highlight.Label))
	}
	labelColumn = min(labelColumn, labelColumnMax, width/3)
	mark := lipgloss.NewStyle().Foreground(t.Accent).Render(presentation.GlyphsFor(mode).HighlightGlyph) + " "
	var rows []string
	for _, highlight := range highlights {
		runs := markedRuns(highlight.Text)
		if highlight.Label == "" {
			rows = append(rows, hanging(mark, 2, runs, width, t.Value, t.Key)...)
			continue
		}
		rows = append(rows, besideColumn(mark+headingStyle(t).Render(highlight.Label), labelColumn+2, runs, width, t.Value, t.Key)...)
	}
	return rows
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
		if count > 0 {
			parts = append(parts, plural(count, group.one, group.many))
		}
	}
	return strings.Join(parts, " · ")
}

func countChips(n Notice, width int, t Theme, mode presentation.GlyphMode) []string {
	var chips []string
	for _, group := range changeGroups {
		if count := kindCount(n, group.kind); count > 0 {
			chips = append(chips, changeMark(group.kind, t, mode)+" "+t.Muted.Render(plural(count, group.one, group.many)))
		}
	}
	if people := len(mergedThanks(n.Releases)); people > 0 {
		heart := lipgloss.NewStyle().Foreground(t.Accent2).Render(presentation.GlyphsFor(mode).PeopleGlyph)
		chips = append(chips, heart+" "+t.Muted.Render(plural(people, "person", "people")))
	}
	return flow(chips, "   ", width)
}

// changesTab lists every change in the range, newest release first, each
// release under its version when there are several.
func changesTab(n Notice, filter string, width int, t Theme, mode presentation.GlyphMode) []string {
	if changeCount(n) == 0 {
		return appendPartial([]string{t.Muted.Render("No summarized changes.")}, n, width, t, mode)
	}
	scopeColumn := 0
	for _, release := range n.Releases {
		for _, change := range release.Changes {
			scopeColumn = max(scopeColumn, ansi.StringWidth(change.Scope))
		}
	}
	scopeColumn = min(scopeColumn, scopeColumnMax, width/4)
	var rows []string
	for index := len(n.Releases) - 1; index >= 0; index-- {
		release := n.Releases[index]
		var section []string
		for _, change := range release.Changes {
			if filter == "" || change.Kind == filter {
				section = append(section, changeRow(change, scopeColumn, width, t, mode)...)
			}
		}
		if omitted := release.TotalChanges - len(release.Changes); omitted > 0 {
			section = append(section, t.Muted.Render(fmt.Sprintf("+%d more in the full notes", omitted)))
		}
		switch {
		case len(section) == 0:
		case len(n.Releases) > 1:
			rows = spaced(rows, append([]string{groupLabel(release.Version, t)}, section...)...)
		default:
			rows = append(rows, section...)
		}
	}
	if len(rows) == 0 {
		for _, group := range changeGroups {
			if group.kind == filter {
				rows = []string{t.Muted.Render(group.none)}
			}
		}
	}
	return appendPartial(rows, n, width, t, mode)
}

func changeRow(change Change, scopeColumn, width int, t Theme, mode presentation.GlyphMode) []string {
	text, scope := t.Value, lipgloss.NewStyle().Foreground(t.Accent2)
	if change.Kind == KindOther {
		text, scope = t.Muted, t.Muted
	}
	lead, indent := changeMark(change.Kind, t, mode)+" ", 2
	if scopeColumn > 0 {
		lead += padRight(scope.Render(ansi.Truncate(change.Scope, scopeColumn, "…")), scopeColumn) + "  "
		indent += scopeColumn + 2
	}
	rows := hanging(lead, indent, []textRun{{text: change.Text}}, width, text, text)
	return trailing(rows, change.Author, lipgloss.NewStyle().Foreground(t.Accent2), width)
}

// thanksTab thanks each person once, in the group of what they did.
func thanksTab(n Notice, width int, t Theme, mode presentation.GlyphMode) []string {
	people := mergedThanks(n.Releases)
	if len(people) == 0 {
		return []string{t.Muted.Render("No thank-you lines.")}
	}
	rows := appendWrapped(nil, []textRun{{text: thanksLine(n, len(people))}}, width, t.Value, t)
	handleColumn := 0
	for _, person := range people {
		handleColumn = max(handleColumn, ansi.StringWidth(person.Handle))
	}
	handleColumn = min(handleColumn, handleColumnMax, width/3)
	handle := lipgloss.NewStyle().Foreground(t.Accent2).Bold(true)
	for _, group := range thanksGroups {
		section := []string{thanksMark(group.group, t, mode) + " " + groupLabel(group.label, t)}
		for _, person := range people {
			if person.Group != group.group {
				continue
			}
			row := besideColumn(handle.Render(person.Handle), handleColumn, []textRun{{text: person.Text}}, width, t.Value, t.Value)
			section = append(section, trailing(row, strings.Join(person.Refs, " "), t.Muted, width)...)
		}
		if len(section) > 1 {
			rows = spaced(rows, section...)
		}
	}
	return rows
}

func thanksLine(n Notice, people int) string {
	what := "this release"
	if len(n.Releases) > 1 {
		what = "these releases"
	}
	line := plural(people, "person", "people") + " made " + what
	if n.From != "" {
		line += " since " + bareVersion(n.From)
	}
	return line + ". Thank you."
}

func thanksMark(group string, t Theme, mode presentation.GlyphMode) string {
	g := presentation.GlyphsFor(mode)
	switch group {
	case ThanksBuilt:
		return lipgloss.NewStyle().Foreground(t.Finished).Render(g.FeatureGlyph)
	case ThanksFound:
		return lipgloss.NewStyle().Foreground(t.Working).Render(g.FixGlyph)
	}
	return lipgloss.NewStyle().Foreground(t.Waiting).Render(g.StartedGlyph)
}

// mergedThanks is one entry per person across the range, oldest release
// first: the group of their first appearance, every text and number they
// earned. A line without a handle stays as written.
func mergedThanks(releases []Release) []Thanks {
	var people []Thanks
	at := map[string]int{}
	for _, release := range releases {
		for _, entry := range release.Thanks {
			key := strings.ToLower(entry.Handle)
			index, seen := at[key]
			if entry.Handle == "" || !seen {
				if entry.Handle != "" {
					at[key] = len(people)
				}
				entry.Refs = slices.Clone(entry.Refs)
				people = append(people, entry)
				continue
			}
			person := &people[index]
			if entry.Text != "" && entry.Text != person.Text {
				person.Text = joinTexts(person.Text, entry.Text)
			}
			for _, ref := range entry.Refs {
				if !slices.Contains(person.Refs, ref) {
					person.Refs = append(person.Refs, ref)
				}
			}
		}
	}
	return people
}

func joinTexts(first, second string) string {
	if first == "" {
		return second
	}
	return first + "; " + second
}
