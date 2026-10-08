package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/feed"
	"github.com/YoanWai/agent-manager/internal/update"
	tea "github.com/charmbracelet/bubbletea"
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

func TestNarrowReleaseBodySetsTheAuthorAtTheRightEdgeOfItsOwnRows(t *testing.T) {
	n := notice{releaseNotes: true, releases: []update.Release{pickersRelease()}, rangeComplete: true}
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
	release.Changes = []update.Change{{Kind: update.KindFix, Text: "Codex: Keep pending messages out of the reply line when the queue fills up", Author: "@drakeo338"}}
	rows := bodyText(notice{releaseNotes: true, releases: []update.Release{release}, rangeComplete: true}, 60)
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
	rows := bodyText(notice{headline: headline, body: []string{"text"}}, 30)
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
	rows := bodyText(notice{releaseNotes: true, releases: []update.Release{pickersRelease()}}, 40)
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
	rows := bodyText(notice{releaseNotes: true, releases: []update.Release{release}, rangeComplete: true}, 120)
	summaryRows := 0
	for _, row := range rows {
		if strings.Contains(row, "summary") {
			summaryRows++
			if lipgloss.Width(row) > noticeProseWidth {
				t.Fatalf("summary row is %d cells, want at most %d: %q", lipgloss.Width(row), noticeProseWidth, row)
			}
		}
	}
	if summaryRows < 2 {
		t.Fatalf("the summary should wrap, got %d rows", summaryRows)
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
	if got := noticeInnerWidth(notices, 300); got != noticeModalMax+noticeScrollbarWidth {
		t.Fatalf("inner width = %d, want %d", got, noticeModalMax+noticeScrollbarWidth)
	}
	if got := noticeInnerWidth(notices, 100); got != 92 {
		t.Fatalf("inner width = %d, want the terminal less 8", got)
	}
}

func TestScrollingBodyKeepsItsWidestRowWhole(t *testing.T) {
	release := pickersRelease()
	wide := update.Change{Kind: update.KindFeature, Text: "Coordination: Agents hand each other work through the shared task list and more words", Author: "@Qusavin"}
	release.Changes = append([]update.Change{wide}, release.Changes...)
	for index := 0; index < 30; index++ {
		release.Changes = append(release.Changes, update.Change{Kind: update.KindFix, Text: fmt.Sprintf("UI: Fix %02d", index)})
	}
	n := notice{releaseNotes: true, releases: []update.Release{release}, rangeComplete: true}
	inner := noticeInnerWidth([]notice{n}, 300)
	body, scrolls := noticeBodyLayout(n, inner, 10)
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
	older := update.Release{Version: "v0.39.0", Headline: strings.Repeat("h", 80), Highlights: []string{"A highlight"}}
	n := notice{releaseNotes: true, releases: []update.Release{older, pickersRelease()}, rangeComplete: true}
	want := lipgloss.Width("v0.39.0 · "+older.Headline) + noticeScrollbarWidth
	if got := noticeInnerWidth([]notice{n}, 300); got != want {
		t.Fatalf("inner width = %d, want %d to fit the older release's heading", got, want)
	}
	if rows := bodyText(n, want-noticeScrollbarWidth); rowIndex(rows, "v0.39.0 · "+older.Headline) < 0 {
		t.Fatalf("the heading is cut at the measured width:\n%s", strings.Join(rows, "\n"))
	}
}

func TestOlderChangesBehindHighlightsDoNotWidenTheModal(t *testing.T) {
	older := update.Release{
		Version:      "v0.39.0",
		Highlights:   []string{"A highlight"},
		Changes:      []update.Change{{Kind: update.KindFix, Text: strings.Repeat("c", 140)}},
		TotalChanges: 1,
	}
	n := notice{releaseNotes: true, releases: []update.Release{older, pickersRelease()}, rangeComplete: true}
	if got := noticeInnerWidth([]notice{n}, 300); got > 100 {
		t.Fatalf("inner width = %d follows a change row that only counts", got)
	}
}

func scrollModel(t *testing.T, lines int) *Model {
	t.Helper()
	m := modalModel(t)
	m.layout.width, m.layout.height = 70, 14
	var body []string
	for index := 0; index < lines; index++ {
		body = append(body, fmt.Sprintf("change line %02d", index))
	}
	m.notices.feedMessages = []feed.Message{{ID: "feed-scroll", Banner: "scroll", Title: "Scrollable summary", Body: body}}
	m.openNotices("feed-scroll")
	return m
}

func thumbRows(frame string) []int {
	var rows []int
	for index, line := range strings.Split(frame, "\n") {
		if strings.Contains(line, "┃") {
			rows = append(rows, index)
		}
	}
	return rows
}

func TestScrollWindowDrawsAThumbThatFollowsTheOffset(t *testing.T) {
	body := make([]string, 40)
	for index := range body {
		body[index] = fmt.Sprintf("row %02d", index)
	}
	top := noticeScrollWindow(body, 10, 0, 20)
	if len(top) != 10 || !strings.HasSuffix(ansi.Strip(top[0]), "┃") || !strings.HasSuffix(ansi.Strip(top[9]), "│") {
		t.Fatalf("at the top the thumb leads the track:\n%s", ansi.Strip(strings.Join(top, "\n")))
	}
	middle := noticeScrollWindow(body, 10, 15, 20)
	for index, row := range middle {
		thumb := index == 4 || index == 5
		if strings.HasSuffix(ansi.Strip(row), "┃") != thumb {
			t.Fatalf("in the middle the thumb sits on rows 4 and 5:\n%s", ansi.Strip(strings.Join(middle, "\n")))
		}
	}
	bottom := noticeScrollWindow(body, 10, 30, 20)
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
	rows := noticeScrollWindow(body, 4, 99, 10)
	if got := ansi.Strip(rows[3]); !strings.HasPrefix(got, "f") {
		t.Fatalf("an offset past the end shows the last page, got %q", got)
	}
}

func TestShortBodyHasNoScrollbar(t *testing.T) {
	m := scrollModel(t, 2)
	if frame := ansi.Strip(preparedView(m)); strings.Contains(frame, "┃") {
		t.Fatalf("a body that fits needs no scrollbar:\n%s", frame)
	}
}

func TestWheelScrollsTheMessageBody(t *testing.T) {
	m := scrollModel(t, 30)
	before := thumbRows(ansi.Strip(preparedView(m)))
	m.handleMouse(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	if m.notices.noticeScroll != noticeWheelRows {
		t.Fatalf("wheel down moved %d rows, want %d", m.notices.noticeScroll, noticeWheelRows)
	}
	for i := 0; i < 40; i++ {
		m.handleMouse(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	}
	limit := m.noticeScrollLimit(m.activeNotices())
	if m.notices.noticeScroll != limit {
		t.Fatalf("wheel scrolled to %d, want it bounded at %d", m.notices.noticeScroll, limit)
	}
	after := thumbRows(ansi.Strip(preparedView(m)))
	if len(before) == 0 || len(after) == 0 || after[0] <= before[0] {
		t.Fatalf("the thumb should have moved down: before %v, after %v", before, after)
	}
	m.handleMouse(tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	if m.notices.noticeScroll != limit-noticeWheelRows {
		t.Fatalf("wheel up moved to %d, want %d", m.notices.noticeScroll, limit-noticeWheelRows)
	}
	if m.mode != modeNotices {
		t.Fatalf("the wheel must not leave the panel, mode=%v", m.mode)
	}
}

func TestHomeAndEndJumpTheMessageBody(t *testing.T) {
	m := scrollModel(t, 30)
	limit := m.noticeScrollLimit(m.activeNotices())
	if limit <= 0 {
		t.Fatalf("the body should scroll, limit %d", limit)
	}
	for _, jump := range []string{"end", "G"} {
		m.notices.noticeScroll = 0
		m.handleNoticesKey(key(jump))
		if m.notices.noticeScroll != limit {
			t.Fatalf("%s moved to %d, want the bottom at %d", jump, m.notices.noticeScroll, limit)
		}
	}
	for _, jump := range []string{"home", "g"} {
		m.notices.noticeScroll = limit
		m.handleNoticesKey(key(jump))
		if m.notices.noticeScroll != 0 {
			t.Fatalf("%s moved to %d, want the top", jump, m.notices.noticeScroll)
		}
	}
}

func TestGrowingTheTerminalKeepsTheLastPageInView(t *testing.T) {
	m := scrollModel(t, 30)
	m.handleNoticesKey(key("end"))
	bottom := m.notices.noticeScroll
	m.layout.height = 20
	frame := ansi.Strip(preparedView(m))
	if !strings.Contains(frame, "change line 29") || !strings.Contains(frame, "╰") {
		t.Fatalf("an offset past the new last page shows that last page inside the frame:\n%s", frame)
	}
	limit := m.noticeScrollLimit(m.activeNotices())
	if limit >= bottom {
		t.Fatalf("the taller terminal should have fewer pages: limit %d, saved offset %d", limit, bottom)
	}
	m.handleMouse(tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	if m.notices.noticeScroll != limit-noticeWheelRows {
		t.Fatalf("the first scroll up starts from the page on screen: offset %d, want %d", m.notices.noticeScroll, limit-noticeWheelRows)
	}
}

func TestHintNamesHomeAndEndWhenItFits(t *testing.T) {
	m := modalModel(t)
	m.notices.feedMessages = []feed.Message{{ID: "feed-wide", Banner: "wide", Title: "Wide", Body: []string{strings.Repeat("wide words ", 12)}}}
	m.openNotices("feed-wide")
	m.layout.width = 180
	if frame := ansi.Strip(preparedView(m)); !strings.Contains(frame, "pgup/pgdn/home/end scroll") {
		t.Fatalf("a wide frame names home and end:\n%s", frame)
	}
	m.layout.width = 80
	frame := ansi.Strip(preparedView(m))
	for _, want := range []string{"x dismiss", "esc"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("a narrow frame keeps %q:\n%s", want, frame)
		}
	}
	if strings.Contains(frame, "home/end") {
		t.Fatalf("a narrow frame keeps the short hint:\n%s", frame)
	}
}
