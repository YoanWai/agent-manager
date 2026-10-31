package notices

import (
	"fmt"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func pickersRelease() Release {
	return Release{
		Version:  "v0.40.0",
		Headline: "Model + reasoning pickers are here!",
		Summary:  "Every session runs on its own model. Press `ctrl+x` for effort.",
		Highlights: []Highlight{
			{Text: "`Model and reasoning pickers` are here, in the New Session modal"},
			{Text: "Antigravity CLI and Oh My Pi are built-in agents"},
		},
		Thanks: []Thanks{{Handle: "@someone", Group: ThanksBuilt, Text: "asked for it", Refs: []string{"#1"}}},
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
	rows := renderBody(n, width, testTheme(), presentation.DefaultGlyphMode)
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

func TestFeedBodyShowsHeadlineAndAccentPhrases(t *testing.T) {
	useTrueColor(t)
	n := Notice{
		Headline: "Model + reasoning pickers are here!",
		Accent:   []string{"ctrl+l"},
		Body:     []string{"In quick prompt mode, ctrl+l opens the model list."},
	}
	rows := renderBody(n, 80, testTheme(), presentation.DefaultGlyphMode)
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

func TestOlderHeadlineWidensTheModal(t *testing.T) {
	older := Release{Version: "v0.39.0", Headline: strings.Repeat("h", 80), Highlights: []Highlight{{Text: "A highlight"}}}
	n := Notice{ReleaseNotes: true, Releases: []Release{older, pickersRelease()}, RangeComplete: true}
	want := lipgloss.Width("v0.39.0 · "+older.Headline) + scrollbarWidth
	if got := innerWidth([]Notice{n}, 300); got != want {
		t.Fatalf("inner width = %d, want %d to fit the older release's heading", got, want)
	}
}

func TestScrollWindowDrawsAThumbThatFollowsTheOffset(t *testing.T) {
	body := make([]string, 40)
	for index := range body {
		body[index] = fmt.Sprintf("row %02d", index)
	}
	top := scrollWindow(body, 10, 0, 20, testTheme(), presentation.DefaultGlyphMode)
	if len(top) != 10 || !strings.HasSuffix(ansi.Strip(top[0]), "┃") || !strings.HasSuffix(ansi.Strip(top[9]), "│") {
		t.Fatalf("at the top the thumb leads the track:\n%s", ansi.Strip(strings.Join(top, "\n")))
	}
	middle := scrollWindow(body, 10, 15, 20, testTheme(), presentation.DefaultGlyphMode)
	for index, row := range middle {
		thumb := index == 4 || index == 5
		if strings.HasSuffix(ansi.Strip(row), "┃") != thumb {
			t.Fatalf("in the middle the thumb sits on rows 4 and 5:\n%s", ansi.Strip(strings.Join(middle, "\n")))
		}
	}
	bottom := scrollWindow(body, 10, 30, 20, testTheme(), presentation.DefaultGlyphMode)
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
	rows := scrollWindow(body, 4, 99, 10, testTheme(), presentation.DefaultGlyphMode)
	if got := ansi.Strip(rows[3]); !strings.HasPrefix(got, "f") {
		t.Fatalf("an offset past the end shows the last page, got %q", got)
	}
}
