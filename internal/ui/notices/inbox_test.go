package notices

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	inboxNews    = "feed-news"
	inboxHeadsUp = "feed-tmux"
)

// inboxSources hold a release, a news entry and a heads-up, all unread.
func inboxSources() Sources {
	return Sources{
		Store: true,
		Update: Update{
			Version: "v0.45.0",
			Latest:  "v0.46.0",
			URL:     RepoURL + "/releases/tag/v0.46.0",
			Available: ReleaseRange{
				Releases: []Release{{Version: "v0.46.0", Headline: "Tabs for every release"}},
				Complete: true,
			},
		},
		Feed: []Message{
			{ID: inboxNews, Kind: KindNews, Title: "Community on Discord", Banner: "#help #workflows #dev", Body: []string{"Questions, setups and every release, in one place."}, URL: "https://discord.gg/ecS88An7pF"},
			{ID: inboxHeadsUp, Kind: KindHeadsUp, Title: "tmux below 3.7", Banner: "control mode can crash"},
		},
	}
}

func inboxFrame(width int) Frame {
	f := testFrame()
	f.Width = width
	return f
}

func plainRows(rows []string) []string {
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = ansi.Strip(row)
	}
	return out
}

func TestSplitStartsAt124Columns(t *testing.T) {
	if split(123) || !split(124) {
		t.Fatal("split threshold must be 124")
	}
}

func TestSplitListGroupsUnderLabelsAndDropsEmptyGroups(t *testing.T) {
	p := NewPanel(map[string]bool{}, map[string]bool{})
	notices := p.Active(inboxSources())
	rows, _ := listRows(notices, 0, listWidth, testTheme(), "", true)
	plain := strings.Join(plainRows(rows), "\n")
	at := func(label string) int { return strings.Index(plain, label) }
	if at("RELEASES") < 0 || at("NEWS") < 0 || at("HEADS UP") < 0 {
		t.Fatalf("missing a group label:\n%s", plain)
	}
	if !(at("RELEASES") < at("NEWS") && at("NEWS") < at("HEADS UP")) {
		t.Fatalf("groups out of order:\n%s", plain)
	}
	if strings.Contains(plain, "TIPS") {
		t.Fatalf("an empty group was labelled:\n%s", plain)
	}
}

func TestUnreadRowEndsWithTheUnreadMarkAndBoldTitle(t *testing.T) {
	useTrueColor(t)
	theme := testTheme()
	firstLine := func(notices []Notice, id string) (raw, plain string) {
		rows, owners := listRows(notices, 0, listWidth, theme, "", true)
		for i, owner := range owners {
			if owner >= 0 && notices[owner].ID == id {
				return rows[i], ansi.Strip(rows[i])
			}
		}
		t.Fatalf("no row for %s", id)
		return "", ""
	}
	bold := lipgloss.NewStyle().Foreground(theme.Bright).Bold(true).Render("Community on Discord")

	p := NewPanel(map[string]bool{}, map[string]bool{})
	raw, plain := firstLine(p.Active(inboxSources()), inboxNews)
	if !strings.HasSuffix(strings.TrimRight(plain, " "), "●") {
		t.Fatalf("unread row %q does not end with the unread mark", plain)
	}
	if !strings.Contains(raw, bold) {
		t.Fatalf("unread title is not bright bold: %q", raw)
	}

	p.See(inboxNews)
	raw, plain = firstLine(p.Active(inboxSources()), inboxNews)
	if strings.Contains(plain, "●") || strings.Contains(raw, bold) {
		t.Fatalf("a read row still reads unread: %q", plain)
	}
}

func TestStackedListUsesOneLinePerNotice(t *testing.T) {
	p := NewPanel(map[string]bool{}, map[string]bool{})
	notices := p.Active(inboxSources())
	_, owners := listRows(notices, 0, 92, testTheme(), "", false)
	rowsOf := map[int]int{}
	for _, owner := range owners {
		if owner >= 0 {
			rowsOf[owner]++
		}
	}
	for i := range notices {
		if rowsOf[i] != 1 {
			t.Fatalf("notice %d owns %d rows, want 1: %v", i, rowsOf[i], owners)
		}
	}
	view := strings.Join(plainRows(p.View(notices, inboxFrame(100))), "\n")
	for _, subtitle := range []string{"#help #workflows #dev", "control mode can crash"} {
		if strings.Contains(view, subtitle) {
			t.Fatalf("stacked list shows the subtitle %q:\n%s", subtitle, view)
		}
	}
	if !strings.Contains(view, "tmux below 3.7") {
		t.Fatalf("stacked list lost a title:\n%s", view)
	}
}

func TestTitleStripCountsUnread(t *testing.T) {
	useTrueColor(t)
	theme := testTheme()
	p := NewPanel(map[string]bool{}, map[string]bool{})
	p.See(inboxHeadsUp)
	notices := p.Active(inboxSources())
	for _, width := range []int{100, 160} {
		top := p.View(notices, inboxFrame(width))[0]
		plain := ansi.Strip(top)
		if !strings.Contains(plain, "messages") || !strings.Contains(plain, "2 unread") {
			t.Fatalf("width %d: top edge %q", width, plain)
		}
		if strings.Index(plain, "messages") > strings.Index(plain, "2 unread") {
			t.Fatalf("width %d: the count sits left of the legend: %q", width, plain)
		}
		if !strings.Contains(top, lipgloss.NewStyle().Foreground(theme.Accent).Render(" 2 unread ")) {
			t.Fatalf("width %d: the count is not in the accent: %q", width, top)
		}
	}
	p.See(inboxNews)
	p.See("update-v0.46.0")
	if top := ansi.Strip(p.View(p.Active(inboxSources()), inboxFrame(160))[0]); strings.Contains(top, "unread") {
		t.Fatalf("nothing unread, top edge %q", top)
	}
}

func TestNoRowOverflowsAtEitherSideOfTheThreshold(t *testing.T) {
	src := inboxSources()
	long := strings.Repeat("a very long line of text ", 8)
	src.Feed = append(src.Feed, Message{ID: "feed-long", Kind: KindTip, Title: long, Banner: long, Body: []string{long[:150]}, URL: "https://example.com/" + long})
	for _, width := range []int{100, 123, 124, 160} {
		for _, cursor := range []string{"update-v0.46.0", "feed-long"} {
			p := NewPanel(map[string]bool{}, map[string]bool{})
			notices := p.Active(src)
			p.Open(notices, cursor)
			f := inboxFrame(width)
			f.Status = long
			view := p.View(notices, f)
			for i, line := range view {
				if got := lipgloss.Width(line); got > width {
					t.Fatalf("width %d, %s: line %d is %d wide: %q", width, cursor, i, got, ansi.Strip(line))
				}
			}
			plain := strings.Join(plainRows(view), "\n")
			if shown := strings.Contains(plain, "control mode can crash"); shown != split(width) {
				t.Fatalf("width %d: subtitles shown = %v, want them only in a split inbox:\n%s", width, shown, plain)
			}
		}
	}
}

func TestSplitInboxSetsTheListBesideTheMessage(t *testing.T) {
	p := NewPanel(map[string]bool{}, map[string]bool{})
	notices := p.Active(inboxSources())
	view := plainRows(p.View(notices, inboxFrame(124)))
	if got := lipgloss.Width(view[0]); got != 124-4 {
		t.Fatalf("a 124-column terminal gets a %d-column card, want 120", got)
	}
	for _, row := range view {
		if strings.Contains(row, "RELEASES") {
			cells := []rune(row)
			if seam := 2 + listWidth; string(cells[seam]) != "│" {
				t.Fatalf("no seam after the list in %q", row)
			}
			if !strings.Contains(row, "Tabs for every release") {
				t.Fatalf("the message does not start beside the list: %q", row)
			}
			return
		}
	}
	t.Fatalf("no group label:\n%s", strings.Join(view, "\n"))
}

func TestClickOnAListRowSelectsThatNotice(t *testing.T) {
	p := NewPanel(map[string]bool{}, map[string]bool{})
	notices := p.Active(inboxSources())
	f := inboxFrame(160)
	line := func(text string) int {
		for row, plain := range plainRows(p.View(notices, f)) {
			if strings.Contains(plain, text) {
				return row - 1
			}
		}
		t.Fatalf("%q not painted", text)
		return -1
	}
	if out := p.Click(1, line("NEWS"), notices, f); out.Action != Stay || p.Cursor() != 0 {
		t.Fatalf("a click on a group label = %+v, cursor %d", out, p.Cursor())
	}
	subtitle := line("#help #workflows #dev")
	if out := p.Click(listWidth+10, subtitle, notices, f); out.Action != Stay || p.Cursor() != 0 {
		t.Fatalf("a click on the message beside a row = %+v, cursor %d", out, p.Cursor())
	}
	if out := p.Click(6, subtitle, notices, f); out.Action != Stay || p.Cursor() != 1 {
		t.Fatalf("a click on the subtitle line = %+v, cursor %d", out, p.Cursor())
	}
	if out := p.Click(6, line("Community on Discord"), notices, f); out.Action != OpenLink || out.URL != notices[1].URL {
		t.Fatalf("a second click on the selected row = %+v, want its link", out)
	}
	if !slices.Contains(ids(notices), inboxNews) || notices[1].ID != inboxNews {
		t.Fatalf("test setup: %v", ids(notices))
	}
}
