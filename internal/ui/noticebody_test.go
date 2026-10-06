package ui

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/update"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func pickersRelease() update.Release {
	return update.Release{
		Version:  "v0.40.0",
		Headline: "Model + reasoning pickers are here!",
		Summary:  "Every session runs on its own model. Press `ctrl+x` for effort.",
		Highlights: []string{
			"`Model and reasoning pickers` are here, in the New Session modal",
			"Antigravity CLI and Oh My Pi are built-in agents",
		},
		Thanks: []string{"@someone asked for it (#1)"},
		Changes: []update.Change{
			{Kind: update.KindFeature, Text: "Config: Add Antigravity CLI support", Author: "@mateuszgachowski"},
			{Kind: update.KindFix, Text: "UI: Keep the focused pane painted"},
			{Kind: update.KindFix, Text: "Codex: Keep pending messages out of the reply line", Author: "@drakeo338"},
			{Kind: update.KindOther, Text: "Ship the agent skills"},
		},
		TotalChanges: 4,
	}
}

func bodyText(n notice, width int) []string {
	rows := renderNoticeBody(n, width)
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
	n := notice{
		headline:      "Model + reasoning pickers are here!",
		releaseNotes:  true,
		body:          []string{"Updated from v0.39.0 to v0.40.0."},
		releases:      []update.Release{pickersRelease()},
		rangeComplete: true,
		after:         []string{"Enter opens the full release notes."},
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
	n := notice{releaseNotes: true, releases: []update.Release{pickersRelease()}, rangeComplete: true}
	joined := strings.Join(renderNoticeBody(n, 100), "\n")
	for _, want := range []string{keyStyle.Render("ctrl+x"), keyStyle.Render("Model and reasoning pickers")} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing accent run %q", want)
		}
	}
}

func TestReleaseBodyRightAlignsTheAuthor(t *testing.T) {
	n := notice{releaseNotes: true, releases: []update.Release{pickersRelease()}, rangeComplete: true}
	rows := bodyText(n, 100)
	row := rows[rowIndex(rows, "Add Antigravity CLI support")]
	if !strings.HasSuffix(row, "@mateuszgachowski") || lipgloss.Width(row) != 100 {
		t.Fatalf("the author sits at the right edge of the body: %q (%d cells)", row, lipgloss.Width(row))
	}
}

func TestReleaseBodyKeepsTheAuthorInlineWhenItCannotFit(t *testing.T) {
	n := notice{releaseNotes: true, releases: []update.Release{pickersRelease()}, rangeComplete: true}
	rows := bodyText(n, 30)
	joined := strings.Join(rows, " ")
	if !strings.Contains(strings.Join(strings.Fields(joined), " "), "support · @mateuszgachowski") {
		t.Fatalf("a narrow body keeps the credit beside the text:\n%s", strings.Join(rows, "\n"))
	}
	for _, row := range rows {
		if lipgloss.Width(row) > 30 {
			t.Fatalf("row is wider than the body: %q", row)
		}
	}
}

func TestOlderReleasesShowHeadlineHighlightsAndCounts(t *testing.T) {
	older := update.Release{
		Version:    "v0.39.0",
		Headline:   "Mouse rows",
		Summary:    "An older summary that stays on the web page.",
		Highlights: []string{"One click focuses a session"},
		Changes: []update.Change{
			{Kind: update.KindFeature, Text: "UI: Row handles"},
			{Kind: update.KindFix, Text: "UI: Older fix one"},
			{Kind: update.KindFix, Text: "UI: Older fix two"},
		},
		TotalChanges: 3,
	}
	n := notice{releaseNotes: true, releases: []update.Release{older, pickersRelease()}, rangeComplete: true}
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
	older := uiRelease("v0.33.0", "UI: A change")
	n := notice{releaseNotes: true, releases: []update.Release{older, pickersRelease()}, rangeComplete: true}
	joined := strings.Join(bodyText(n, 100), "\n")
	if !strings.Contains(joined, "v0.33.0") || !strings.Contains(joined, "• UI: A change") {
		t.Fatalf("an older release with nothing authored must not be empty:\n%s", joined)
	}
}

func TestFeedBodyShowsHeadlineAndAccentPhrases(t *testing.T) {
	useTrueColor(t)
	n := notice{
		headline: "Model + reasoning pickers are here!",
		accent:   []string{"ctrl+l"},
		body:     []string{"In quick prompt mode, ctrl+l opens the model list."},
	}
	rows := renderNoticeBody(n, 80)
	if ansi.Strip(rows[0]) != "Model + reasoning pickers are here!" || ansi.Strip(rows[2]) != "" {
		t.Fatalf("headline, rule, then a blank row before the body: %q", rows[:3])
	}
	if !strings.Contains(strings.Join(rows, "\n"), keyStyle.Render("ctrl+l")) {
		t.Fatalf("accent phrase is not styled")
	}
}

func TestBodyWithoutAHeadlineStartsWithItsText(t *testing.T) {
	rows := bodyText(notice{body: []string{"first line", "", "third line"}}, 80)
	if len(rows) != 3 || rows[0] != "first line" || rows[1] != "" || rows[2] != "third line" {
		t.Fatalf("rows = %q", rows)
	}
}

func TestSummaryNeverWidensTheModal(t *testing.T) {
	release := pickersRelease()
	release.Summary = strings.Repeat("a long summary sentence ", 20)
	notices := []notice{{releaseNotes: true, releases: []update.Release{release}}}
	if got := noticeInnerWidth(notices, 300); got > 90 {
		t.Fatalf("inner width %d follows the summary, want it to follow the longest row", got)
	}
}

func TestModalWidthStopsAtItsCeiling(t *testing.T) {
	notices := []notice{{body: []string{strings.Repeat("x", 400)}}}
	if got := noticeInnerWidth(notices, 300); got != noticeModalMax {
		t.Fatalf("inner width = %d, want %d", got, noticeModalMax)
	}
	if got := noticeInnerWidth(notices, 100); got != 92 {
		t.Fatalf("inner width = %d, want the terminal less 8", got)
	}
}
