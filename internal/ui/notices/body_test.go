package notices

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func pickersRelease() Release {
	return Release{
		Version:  "v0.40.0",
		Headline: "Model + reasoning pickers are here!",
		Summary:  "Every session runs on its own model. Press `ctrl+x` for effort.",
		Highlights: []string{
			"`Model and reasoning pickers` are here, in the New Session modal",
			"Antigravity CLI and Oh My Pi are built-in agents",
		},
		Thanks: []string{"@someone asked for it (#1)"},
		Changes: []Change{
			{Kind: KindFeature, Text: "Config: Add Antigravity CLI support", Author: "@mateuszgachowski"},
			{Kind: KindFix, Text: "UI: Keep the focused pane painted"},
			{Kind: KindFix, Text: "Codex: Keep pending messages out of the reply line", Author: "@drakeo338"},
			{Kind: KindOther, Text: "Ship the agent skills"},
		},
		TotalChanges: 4,
	}
}

func bodyText(n Notice, width int) []string {
	rows := renderBody(n, width, testTheme())
	plain := make([]string, len(rows))
	for index, row := range rows {
		plain[index] = ansi.Strip(row)
	}
	return plain
}

func rowIndex(rows []string, want string) int {
	for index, row := range rows {
		if strings.Contains(row, want) {
			return index
		}
	}
	return -1
}

func TestReleaseBodyOrdersHeadlineSummaryHighlightsAndLists(t *testing.T) {
	n := Notice{
		Headline:      "Model + reasoning pickers are here!",
		ReleaseNotes:  true,
		Body:          []string{"Updated from v0.39.0 to v0.40.0."},
		Releases:      []Release{pickersRelease()},
		RangeComplete: true,
		After:         []string{"Enter opens the full release notes."},
	}
	rows := bodyText(n, 100)
	order := []string{
		"Model + reasoning pickers are here!",
		strings.Repeat("━", 35),
		"Updated from v0.39.0 to v0.40.0.",
		"Every session runs on its own model. Press ctrl+x for effort.",
		"HIGHLIGHTS",
		"• Model and reasoning pickers are here, in the New Session modal",
		"FEATURES · 1",
		"• Config: Add Antigravity CLI support",
		"FIXES · 2",
		"• UI: Keep the focused pane painted",
		"OTHER · 1",
		"• Ship the agent skills",
		"THANK YOU",
		"• @someone asked for it (#1)",
		"Enter opens the full release notes.",
	}
	last := -1
	for _, want := range order {
		at := rowIndex(rows, want)
		if at <= last {
			t.Fatalf("%q is at row %d, want it after row %d:\n%s", want, at, last, strings.Join(rows, "\n"))
		}
		last = at
	}
	if rows[0] != "Model + reasoning pickers are here!" {
		t.Fatalf("the headline opens the body, got %q", rows[0])
	}
	if got := strings.Count(strings.Join(rows, "\n"), "v0.40.0"); got != 1 {
		t.Fatalf("a single release needs no version heading, found the version %d times", got)
	}
	if strings.Contains(strings.Join(rows, "\n"), "`") {
		t.Fatalf("accent marks must not reach the screen:\n%s", strings.Join(rows, "\n"))
	}
}

func TestReleaseBodyAccentsTheMarkedWords(t *testing.T) {
	useTrueColor(t)
	n := Notice{ReleaseNotes: true, Releases: []Release{pickersRelease()}, RangeComplete: true}
	joined := strings.Join(renderBody(n, 100, testTheme()), "\n")
	for _, want := range []string{testTheme().Key.Render("ctrl+x"), testTheme().Key.Render("Model and reasoning pickers")} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing accent run %q", want)
		}
	}
}

func TestReleaseBodyRightAlignsTheAuthor(t *testing.T) {
	n := Notice{ReleaseNotes: true, Releases: []Release{pickersRelease()}, RangeComplete: true}
	rows := bodyText(n, 100)
	row := rows[rowIndex(rows, "Add Antigravity CLI support")]
	if !strings.HasSuffix(row, "@mateuszgachowski") || lipgloss.Width(row) != 100 {
		t.Fatalf("the author sits at the right edge of the body: %q (%d cells)", row, lipgloss.Width(row))
	}
}

func TestNarrowReleaseBodySetsTheAuthorAtTheRightEdgeOfItsOwnRows(t *testing.T) {
	n := Notice{ReleaseNotes: true, Releases: []Release{pickersRelease()}, RangeComplete: true}
	rows := bodyText(n, 30)
	for _, row := range rows {
		if lipgloss.Width(row) > 30 {
			t.Fatalf("row is wider than the body: %q", row)
		}
	}
	for _, author := range []string{"@mateuszgachowski", "@drakeo338"} {
		at := rowIndex(rows, author)
		if at < 0 || !strings.HasSuffix(rows[at], author) || lipgloss.Width(rows[at]) != 30 {
			t.Fatalf("%s sits at the right edge of its row:\n%s", author, strings.Join(rows, "\n"))
		}
		if strings.Contains(rows[at], "·") {
			t.Fatalf("no separator before the author: %q", rows[at])
		}
	}
}

func TestAuthorSharesTheLastWrappedRowWhenItFits(t *testing.T) {
	release := pickersRelease()
	release.Changes = []Change{{Kind: KindFix, Text: "Codex: Keep pending messages out of the reply line when the queue fills up", Author: "@drakeo338"}}
	rows := bodyText(Notice{ReleaseNotes: true, Releases: []Release{release}, RangeComplete: true}, 60)
	first := rowIndex(rows, "• Codex: Keep pending")
	if first < 0 || first+1 >= len(rows) {
		t.Fatalf("change row missing:\n%s", strings.Join(rows, "\n"))
	}
	second := rows[first+1]
	if !strings.Contains(second, "fills up") || !strings.HasSuffix(second, "@drakeo338") || lipgloss.Width(second) != 60 {
		t.Fatalf("the author shares the second row at the right edge: %q", second)
	}
}

func TestLongHeadlineWrapsUnderOneRule(t *testing.T) {
	headline := "Model and reasoning pickers are here for every agent in every session you run now"
	rows := bodyText(Notice{Headline: headline, Body: []string{"text"}}, 30)
	rule := rowIndex(rows, "━")
	if rule < 2 {
		t.Fatalf("the headline wraps onto rows above its rule:\n%s", strings.Join(rows, "\n"))
	}
	widest := 0
	for _, row := range rows[:rule] {
		widest = max(widest, lipgloss.Width(row))
	}
	if lipgloss.Width(rows[rule]) != widest {
		t.Fatalf("rule is %d cells, want the widest headline row %d", lipgloss.Width(rows[rule]), widest)
	}
	if got := strings.Join(rows[:rule], " "); got != headline || strings.Contains(got, "…") {
		t.Fatalf("headline rows = %q, want every word", got)
	}
}

func TestPartialRangeLineWraps(t *testing.T) {
	rows := bodyText(Notice{ReleaseNotes: true, Releases: []Release{pickersRelease()}}, 40)
	for _, row := range rows {
		if lipgloss.Width(row) > 40 {
			t.Fatalf("row is wider than the body: %q", row)
		}
	}
	if joined := strings.Join(strings.Fields(strings.Join(rows, " ")), " "); !strings.Contains(joined, "Enter opens the complete notes.") {
		t.Fatalf("partial-range line lost words:\n%s", strings.Join(rows, "\n"))
	}
}

func TestSummaryWrapsAtProseWidth(t *testing.T) {
	release := pickersRelease()
	release.Summary = strings.Repeat("summary words keep going ", 12)
	rows := bodyText(Notice{ReleaseNotes: true, Releases: []Release{release}, RangeComplete: true}, 120)
	summaryRows := 0
	for _, row := range rows {
		if strings.Contains(row, "summary") {
			summaryRows++
			if lipgloss.Width(row) > proseWidth {
				t.Fatalf("summary row is %d cells, want at most %d: %q", lipgloss.Width(row), proseWidth, row)
			}
		}
	}
	if summaryRows < 2 {
		t.Fatalf("the summary should wrap, got %d rows", summaryRows)
	}
}

func TestOlderReleasesShowHeadlineHighlightsAndCounts(t *testing.T) {
	older := Release{
		Version:    "v0.39.0",
		Headline:   "Mouse rows",
		Summary:    "An older summary that stays on the web page.",
		Highlights: []string{"One click focuses a session"},
		Changes: []Change{
			{Kind: KindFeature, Text: "UI: Row handles"},
			{Kind: KindFix, Text: "UI: Older fix one"},
			{Kind: KindFix, Text: "UI: Older fix two"},
		},
		TotalChanges: 3,
	}
	n := Notice{ReleaseNotes: true, Releases: []Release{older, pickersRelease()}, RangeComplete: true}
	rows := bodyText(n, 100)
	joined := strings.Join(rows, "\n")
	if newest, old := rowIndex(rows, "v0.40.0"), rowIndex(rows, "v0.39.0 · Mouse rows"); newest < 0 || old < newest {
		t.Fatalf("newest release first, then the older one with its headline:\n%s", joined)
	}
	for _, want := range []string{"• One click focuses a session", "1 feature · 2 fixes"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("older release missing %q:\n%s", want, joined)
		}
	}
	for _, unwanted := range []string{"Older fix one", "An older summary"} {
		if strings.Contains(joined, unwanted) {
			t.Fatalf("an older release keeps its list and summary on the web page, found %q:\n%s", unwanted, joined)
		}
	}
}

func TestOlderReleaseWithoutHighlightsListsItsChanges(t *testing.T) {
	older := releaseOf("v0.33.0", "UI: A change")
	n := Notice{ReleaseNotes: true, Releases: []Release{older, pickersRelease()}, RangeComplete: true}
	joined := strings.Join(bodyText(n, 100), "\n")
	if !strings.Contains(joined, "v0.33.0") || !strings.Contains(joined, "• UI: A change") {
		t.Fatalf("an older release with nothing authored must not be empty:\n%s", joined)
	}
}

func TestFeedBodyShowsHeadlineAndAccentPhrases(t *testing.T) {
	useTrueColor(t)
	n := Notice{
		Headline: "Model + reasoning pickers are here!",
		Accent:   []string{"ctrl+l"},
		Body:     []string{"In quick prompt mode, ctrl+l opens the model list."},
	}
	rows := renderBody(n, 80, testTheme())
	if ansi.Strip(rows[0]) != "Model + reasoning pickers are here!" || ansi.Strip(rows[2]) != "" {
		t.Fatalf("headline, rule, then a blank row before the body: %q", rows[:3])
	}
	if !strings.Contains(strings.Join(rows, "\n"), testTheme().Key.Render("ctrl+l")) {
		t.Fatalf("accent phrase is not styled")
	}
}

func TestBodyWithoutAHeadlineStartsWithItsText(t *testing.T) {
	rows := bodyText(Notice{Body: []string{"first line", "", "third line"}}, 80)
	if len(rows) != 3 || rows[0] != "first line" || rows[1] != "" || rows[2] != "third line" {
		t.Fatalf("rows = %q", rows)
	}
}

func TestSummaryNeverWidensTheModal(t *testing.T) {
	release := pickersRelease()
	release.Summary = strings.Repeat("a long summary sentence ", 20)
	notices := []Notice{{ReleaseNotes: true, Releases: []Release{release}}}
	if got := innerWidth(notices, 300); got > 90 {
		t.Fatalf("inner width %d follows the summary, want it to follow the longest row", got)
	}
}

func TestModalWidthStopsAtItsCeiling(t *testing.T) {
	notices := []Notice{{Body: []string{strings.Repeat("x", 400)}}}
	if got := innerWidth(notices, 300); got != modalMax+scrollbarWidth {
		t.Fatalf("inner width = %d, want %d", got, modalMax+scrollbarWidth)
	}
	if got := innerWidth(notices, 100); got != 92 {
		t.Fatalf("inner width = %d, want the terminal less 8", got)
	}
}

func TestScrollingBodyKeepsItsWidestRowWhole(t *testing.T) {
	release := pickersRelease()
	wide := Change{Kind: KindFeature, Text: "Coordination: Agents hand each other work through the shared task list and more words", Author: "@Qusavin"}
	release.Changes = append([]Change{wide}, release.Changes...)
	for index := 0; index < 30; index++ {
		release.Changes = append(release.Changes, Change{Kind: KindFix, Text: fmt.Sprintf("UI: Fix %02d", index)})
	}
	n := Notice{ReleaseNotes: true, Releases: []Release{release}, RangeComplete: true}
	inner := innerWidth([]Notice{n}, 300)
	body, scrolls := bodyLayout(n, inner, 10, testTheme())
	if !scrolls {
		t.Fatal("the body should scroll")
	}
	rows := make([]string, len(body))
	for index, row := range body {
		rows[index] = ansi.Strip(row)
	}
	at := rowIndex(rows, "• Coordination:")
	if at < 0 || !strings.Contains(rows[at], "and more words") || !strings.HasSuffix(rows[at], "@Qusavin") {
		t.Fatalf("the widest row wrapped beside the scrollbar:\n%s", strings.Join(rows, "\n"))
	}
}

func TestOlderHeadlineWidensTheModal(t *testing.T) {
	older := Release{Version: "v0.39.0", Headline: strings.Repeat("h", 80), Highlights: []string{"A highlight"}}
	n := Notice{ReleaseNotes: true, Releases: []Release{older, pickersRelease()}, RangeComplete: true}
	want := lipgloss.Width("v0.39.0 · "+older.Headline) + scrollbarWidth
	if got := innerWidth([]Notice{n}, 300); got != want {
		t.Fatalf("inner width = %d, want %d to fit the older release's heading", got, want)
	}
	if rows := bodyText(n, want-scrollbarWidth); rowIndex(rows, "v0.39.0 · "+older.Headline) < 0 {
		t.Fatalf("the heading is cut at the measured width:\n%s", strings.Join(rows, "\n"))
	}
}

func TestOlderChangesBehindHighlightsDoNotWidenTheModal(t *testing.T) {
	older := Release{
		Version:      "v0.39.0",
		Highlights:   []string{"A highlight"},
		Changes:      []Change{{Kind: KindFix, Text: strings.Repeat("c", 140)}},
		TotalChanges: 1,
	}
	n := Notice{ReleaseNotes: true, Releases: []Release{older, pickersRelease()}, RangeComplete: true}
	if got := innerWidth([]Notice{n}, 300); got > 100 {
		t.Fatalf("inner width = %d follows a change row that only counts", got)
	}
}

func TestScrollWindowDrawsAThumbThatFollowsTheOffset(t *testing.T) {
	body := make([]string, 40)
	for index := range body {
		body[index] = fmt.Sprintf("row %02d", index)
	}
	top := scrollWindow(body, 10, 0, 20, testTheme())
	if len(top) != 10 || !strings.HasSuffix(ansi.Strip(top[0]), "┃") || !strings.HasSuffix(ansi.Strip(top[9]), "│") {
		t.Fatalf("at the top the thumb leads the track:\n%s", ansi.Strip(strings.Join(top, "\n")))
	}
	middle := scrollWindow(body, 10, 15, 20, testTheme())
	for index, row := range middle {
		thumb := index == 4 || index == 5
		if strings.HasSuffix(ansi.Strip(row), "┃") != thumb {
			t.Fatalf("in the middle the thumb sits on rows 4 and 5:\n%s", ansi.Strip(strings.Join(middle, "\n")))
		}
	}
	bottom := scrollWindow(body, 10, 30, 20, testTheme())
	if !strings.HasSuffix(ansi.Strip(bottom[9]), "┃") || !strings.HasPrefix(ansi.Strip(bottom[9]), "row 39") {
		t.Fatalf("at the bottom the thumb ends the track and the last row shows:\n%s", ansi.Strip(strings.Join(bottom, "\n")))
	}
	for _, row := range top {
		if lipgloss.Width(row) != 20 {
			t.Fatalf("every row fills the inner width: %q is %d", ansi.Strip(row), lipgloss.Width(row))
		}
	}
}

func TestScrollWindowClampsAnOffsetPastTheEnd(t *testing.T) {
	body := []string{"a", "b", "c", "d", "e", "f"}
	rows := scrollWindow(body, 4, 99, 10, testTheme())
	if got := ansi.Strip(rows[3]); !strings.HasPrefix(got, "f") {
		t.Fatalf("an offset past the end shows the last page, got %q", got)
	}
}

func TestReleaseSummaryMarksOmittedChangesAndPartialRange(t *testing.T) {
	n := Notice{
		Releases:      []Release{releaseWithTotal("v0.3.0", 4, "Visible change")},
		RangeComplete: false,
	}
	body := ansi.Strip(strings.Join(renderBody(n, modalInner, testTheme()), "\n"))
	for _, want := range []string{"OTHER · 1", "+3 more in the full notes", "catalog covers part of this range"} {
		if !strings.Contains(body, want) {
			t.Fatalf("partial summary missing %q:\n%s", want, body)
		}
	}
}

func TestReleaseSummaryShowsTheListUnderTheHighlights(t *testing.T) {
	release := releaseWithTotal("v0.34.0", 17, "UI: Full screen sessions mode")
	release.Highlights = []string{"The session list can take the whole terminal"}
	body := ansi.Strip(strings.Join(renderBody(Notice{Releases: []Release{release}, RangeComplete: true}, modalInner, testTheme()), "\n"))

	if !strings.Contains(body, "• The session list can take the whole terminal") {
		t.Fatalf("highlights missing:\n%s", body)
	}
	for _, want := range []string{"OTHER · 1", "• UI: Full screen sessions mode", "+16 more in the full notes"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the generated list follows the highlights, missing %q:\n%s", want, body)
		}
	}
}

func TestReleaseSummaryShowsThanksUnderHighlights(t *testing.T) {
	release := releaseWithTotal("v0.35.0", 7, "UI: Add header and stats visibility settings")
	release.Highlights = []string{"Revive without a captured id opens the tool's own picker"}
	release.Thanks = []string{
		"@dolutech asked in #388 and built the picker (#400)",
		"@fruch reported that a live rename moved the worktree (#418)",
	}
	body := ansi.Strip(strings.Join(renderBody(Notice{Releases: []Release{release}, RangeComplete: true}, modalInner, testTheme()), "\n"))

	for _, want := range []string{
		"• Revive without a captured id opens the tool's own picker",
		"THANK YOU",
		"• @dolutech asked in #388 and built the picker (#400)",
		"• @fruch reported that a live rename moved the worktree (#418)",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("thanks under highlights missing %q:\n%s", want, body)
		}
	}
	if !strings.Contains(body, "Add header and stats visibility settings") {
		t.Fatalf("the generated list follows the highlights:\n%s", body)
	}
}

func TestReleaseSummaryShowsThanksUnderGeneratedList(t *testing.T) {
	release := releaseOf("v0.33.0", "UI: A change")
	release.Thanks = []string{"@pandysp asked for a way to put the preview away in #357"}
	body := ansi.Strip(strings.Join(renderBody(Notice{
		Releases:      []Release{release},
		RangeComplete: true,
	}, modalInner, testTheme()), "\n"))
	for _, want := range []string{
		"OTHER · 1",
		"• UI: A change",
		"THANK YOU",
		"• @pandysp asked for a way to put the preview away in #357",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("thanks under generated list missing %q:\n%s", want, body)
		}
	}
}

func TestReleaseSummaryFallsBackToTheGeneratedList(t *testing.T) {
	body := ansi.Strip(strings.Join(renderBody(Notice{
		Releases:      []Release{releaseOf("v0.33.0", "UI: A change")},
		RangeComplete: true,
	}, modalInner, testTheme()), "\n"))
	if !strings.Contains(body, "OTHER · 1") || !strings.Contains(body, "• UI: A change") {
		t.Fatalf("release without highlights lost its generated list:\n%s", body)
	}
}

func releaseOf(version string, changes ...string) Release {
	return releaseWithTotal(version, len(changes), changes...)
}

func releaseWithTotal(version string, total int, changes ...string) Release {
	release := Release{
		Version:      version,
		URL:          RepoURL + "/releases/tag/" + version,
		TotalChanges: total,
	}
	for _, text := range changes {
		release.Changes = append(release.Changes, Change{Kind: KindOther, Text: text})
	}
	return release
}
