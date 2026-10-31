package notices

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func testSources(latest string) Sources {
	return Sources{
		Store:  true,
		Update: Update{Version: "v0.2.0", Latest: latest},
		Feed:   []Message{{ID: testTip, Kind: KindTip, Title: "A tip", URL: RepoURL + "#readme"}},
	}
}

const testTip = "feed-tip"

func testFrame() Frame {
	return Frame{Width: 100, Height: 40, Theme: testTheme()}
}

func ids(notices []Notice) []string {
	out := make([]string, len(notices))
	for i, n := range notices {
		out[i] = n.ID
	}
	return out
}

func TestPendingFeedNamesThePanelTail(t *testing.T) {
	frame := testFrame()
	frame.FeedPending = true
	panel := NewPanel(nil, nil)
	got := ansi.Strip(strings.Join(panel.View(nil, frame), "\n"))
	if !strings.Contains(got, FeedPendingText) {
		t.Fatalf("a pending feed should say so in the panel:\n%s", got)
	}
}

func TestPanelRunsOnCopiedSources(t *testing.T) {
	src := testSources("v0.3.0")
	p := NewPanel(map[string]bool{}, map[string]bool{})
	notices := p.Active(src)
	if got := ids(notices); !slices.Equal(got, []string{"update-v0.3.0", testTip}) {
		t.Fatalf("notices = %v", got)
	}
	p.Open(notices, testTip)
	if p.Cursor() != 1 {
		t.Fatalf("open selected %d, want the tip", p.Cursor())
	}

	if out := p.Key(key("u"), notices, testFrame()); out.Action != Stay {
		t.Fatalf("u off the update notice = %+v", out)
	}
	p.Key(key("up"), notices, testFrame())
	if out := p.Key(key("u"), notices, testFrame()); out.Action != StartUpdate {
		t.Fatalf("u on the update notice = %+v", out)
	}
	if out := p.Key(key("r"), notices, testFrame()); out.Action != Refresh {
		t.Fatalf("r = %+v", out)
	}
	if out := p.Key(key("enter"), notices, testFrame()); out.Action != Stay {
		t.Fatalf("enter on a notice without a link = %+v", out)
	}

	frame := testFrame()
	frame.Status = "refresh failed"
	if view := strings.Join(p.View(notices, frame), "\n"); !strings.Contains(view, "refresh failed") || !strings.Contains(view, "v0.3.0 available") {
		t.Fatalf("view missed the status or the list:\n%s", view)
	}

	if out := p.Key(key("x"), notices, testFrame()); out.Action != Dismiss || out.ID != "update-v0.3.0" || out.Close {
		t.Fatalf("x = %+v", out)
	}
	if out := p.Key(key("esc"), notices, testFrame()); out.Action != Close {
		t.Fatalf("esc = %+v", out)
	}
}

func TestUpdateNoticeHighlightsPressU(t *testing.T) {
	useTrueColor(t)
	p := NewPanel(map[string]bool{}, map[string]bool{})
	notices := p.Active(testSources("v0.3.0"))
	view := strings.Join(p.View(notices, testFrame()), "\n")
	theme := testTheme()
	if !strings.Contains(view, theme.Key.Render("u update")) || !strings.Contains(ansi.Strip(view), "u  Update to 0.3.0 and restart") {
		t.Fatalf("update notice lacks its call to action:\n%s", ansi.Strip(view))
	}
	p.Key(key("down"), notices, testFrame())
	next := strings.Join(p.View(notices, testFrame()), "\n")
	if strings.Contains(next, theme.Key.Render("u update")) || strings.Contains(ansi.Strip(next), "Update to") {
		t.Fatalf("press u stays on the update notice only:\n%s", ansi.Strip(next))
	}
}

// A wide but short terminal cannot hold the two-line list, so the inbox
// stacks rather than push its bottom edge off screen.
func TestShortWideTerminalStacksTheInbox(t *testing.T) {
	src := inboxSources()
	src.WhatsNewVersion, src.WhatsNewFrom = src.Update.Version, "v0.44.0"
	src.Feed = append(src.Feed, Message{ID: "feed-more", Kind: KindNews, Title: "More news", Banner: "a second entry"})
	p := NewPanel(map[string]bool{}, map[string]bool{})
	notices := p.Active(src)
	if len(notices) != 5 {
		t.Fatalf("test setup: %v", ids(notices))
	}
	f := inboxFrame(160)
	f.Height = 16
	view := p.View(notices, f)
	if len(view) > f.Height {
		t.Fatalf("the card is %d rows on a %d-row terminal:\n%s", len(view), f.Height, strings.Join(plainRows(view), "\n"))
	}
	plain := strings.Join(plainRows(view), "\n")
	if strings.Contains(plain, "control mode can crash") || !strings.Contains(plainRows(view)[len(view)-1], "esc/q") {
		t.Fatalf("a short terminal keeps the split list:\n%s", plain)
	}
	f.Height = 40
	if tall := strings.Join(plainRows(p.View(notices, f)), "\n"); !strings.Contains(tall, "control mode can crash") {
		t.Fatalf("a tall terminal splits:\n%s", tall)
	}
}

func TestPanelDismissingTheLastNoticeCloses(t *testing.T) {
	src := testSources("")
	p := NewPanel(map[string]bool{}, map[string]bool{})
	notices := p.Active(src)
	p.Open(notices, testTip)
	if out := p.Key(key("d"), notices, testFrame()); out.Action != Dismiss || out.ID != testTip || !out.Close {
		t.Fatalf("d on the only notice = %+v", out)
	}
	p.Dismiss(testTip)
	if notices = p.Active(src); len(notices) != 0 {
		t.Fatalf("dismissed notices still listed: %v", ids(notices))
	}
	p.Restore(testTip)
	if !slices.Equal(ids(p.Active(src)), []string{testTip}) {
		t.Fatal("a restored notice is listed again")
	}
}

func TestPanelReselectsByID(t *testing.T) {
	p := NewPanel(map[string]bool{}, map[string]bool{})
	before := p.Active(testSources(""))
	p.Open(before, testTip)
	selected := p.Selected(before)
	after := p.Active(testSources("v0.3.0"))
	p.Reselect(after, selected)
	if after[p.Cursor()].ID != testTip {
		t.Fatalf("selection moved to %q", after[p.Cursor()].ID)
	}
	p.Reselect(after, "gone")
	if p.Cursor() != 0 {
		t.Fatalf("a vanished selection left the cursor at %d", p.Cursor())
	}
}

func TestPanelWithoutAStoreListsNothing(t *testing.T) {
	src := testSources("v0.3.0")
	src.Store = false
	if notices := NewPanel(map[string]bool{}, map[string]bool{}).Active(src); notices != nil {
		t.Fatalf("notices = %v", ids(notices))
	}
}

// Each key the bottom edge names sits on the columns that paint it, for
// every width the edge's text takes, including the cut that drops keys.
func TestPanelFootKeysSitOnTheirLabels(t *testing.T) {
	for _, width := range []int{120, 60, 44} {
		for _, update := range []string{"", "v0.3.0"} {
			p := NewPanel(map[string]bool{}, map[string]bool{})
			notices := p.Active(testSources(update))
			p.Open(notices, "")
			frame := testFrame()
			frame.Width = width
			view := p.View(notices, frame)
			edge := []rune(ansi.Strip(view[len(view)-1]))
			feet := p.Foot(notices, frame)
			labels := map[string]bool{}
			for _, foot := range feet {
				labels[foot.Label] = true
			}
			if !labels["r"] || !labels["↵"] || !labels["x"] || !labels["esc/q"] {
				t.Fatalf("width %d: the edge names r, ↵, x and esc at least, got %+v", width, feet)
			}
			for _, foot := range feet {
				if foot.X1 > len(edge) {
					t.Fatalf("width %d: %+v runs past the edge %q", width, foot, string(edge))
				}
				if got := strings.TrimSpace(string(edge[foot.X0:foot.X1])); !strings.HasPrefix(got, foot.Label) {
					t.Errorf("width %d: key %q sits on %q", width, foot.Label, got)
				}
			}
		}
	}
}

// The first click on a title selects it; a second on the selected title
// opens its link, the way ↵ does.
func TestPanelClickSelectsThenOpensATitle(t *testing.T) {
	p := NewPanel(map[string]bool{}, map[string]bool{})
	notices := p.Active(testSources("v0.3.0"))
	p.Open(notices, "")
	if len(notices) < 2 {
		t.Fatalf("test setup: need two notices, have %v", ids(notices))
	}
	// The frame's top edge is card row 0; a click counts rows inside it.
	second := -1
	for row, line := range p.View(notices, testFrame()) {
		if strings.Contains(ansi.Strip(line), notices[1].Title) {
			second = row - 1
			break
		}
	}
	if out := p.Click(0, second, notices, testFrame()); out.Action != Stay || p.Cursor() != 1 {
		t.Fatalf("first click = %+v, cursor %d", out, p.Cursor())
	}
	out := p.Click(0, second, notices, testFrame())
	if notices[1].URL == "" {
		t.Fatalf("test setup: %q has no link", notices[1].ID)
	}
	if out.Action != OpenLink || out.URL != notices[1].URL {
		t.Fatalf("second click = %+v, want its link", out)
	}
	if out := p.Click(0, second+len(notices), notices, testFrame()); out.Action != Stay || p.Cursor() != 1 {
		t.Fatalf("a click under the titles = %+v, cursor %d", out, p.Cursor())
	}
}

func TestActiveMarksUnseenNoticesUnread(t *testing.T) {
	p := NewPanel(map[string]bool{}, map[string]bool{})
	p.See("update-v0.3.0")
	if !p.Seen("update-v0.3.0") || p.Seen(testTip) {
		t.Fatal("See should mark that notice alone")
	}
	var unread []string
	for _, n := range p.Active(testSources("v0.3.0")) {
		if n.Unread {
			unread = append(unread, n.ID)
		}
	}
	if !slices.Equal(unread, []string{testTip}) {
		t.Fatalf("unread = %v, want the tip alone", unread)
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
