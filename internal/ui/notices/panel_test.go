package notices

import (
	"slices"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/charmbracelet/x/ansi"
)

func testSources(latest string) Sources {
	return Sources{
		Store:       true,
		Update:      Update{Version: "v0.2.0", Latest: latest},
		SessionKeys: keybind.DefaultSession(),
		ListKeys:    keybind.DefaultList(),
	}
}

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

func TestPanelRunsOnCopiedSources(t *testing.T) {
	src := testSources("v0.3.0")
	p := NewPanel(map[string]bool{})
	notices := p.Active(src)
	if got := ids(notices); !slices.Equal(got, []string{"update-v0.3.0", Welcome, ArrowStep}) {
		t.Fatalf("notices = %v", got)
	}
	p.Open(notices, Welcome)
	if p.Cursor() != 1 {
		t.Fatalf("open selected %d, want the welcome", p.Cursor())
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

func TestPanelDismissingTheLastNoticeCloses(t *testing.T) {
	src := testSources("")
	p := NewPanel(map[string]bool{Welcome: true})
	notices := p.Active(src)
	p.Open(notices, ArrowStep)
	if out := p.Key(key("d"), notices, testFrame()); out.Action != Dismiss || out.ID != ArrowStep || !out.Close {
		t.Fatalf("d on the only notice = %+v", out)
	}
	p.Dismiss(ArrowStep)
	if notices = p.Active(src); len(notices) != 0 {
		t.Fatalf("dismissed notices still listed: %v", ids(notices))
	}
	p.Restore(ArrowStep)
	if !slices.Equal(ids(p.Active(src)), []string{ArrowStep}) {
		t.Fatal("a restored notice is listed again")
	}
}

func TestPanelReselectsByID(t *testing.T) {
	p := NewPanel(map[string]bool{})
	before := p.Active(testSources(""))
	p.Open(before, ArrowStep)
	selected := p.Selected(before)
	after := p.Active(testSources("v0.3.0"))
	p.Reselect(after, selected)
	if after[p.Cursor()].ID != ArrowStep {
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
	if notices := NewPanel(map[string]bool{}).Active(src); notices != nil {
		t.Fatalf("notices = %v", ids(notices))
	}
}

// Each key the bottom edge names sits on the columns that paint it, for
// every width the edge's text takes, including the cut that drops keys.
func TestPanelFootKeysSitOnTheirLabels(t *testing.T) {
	for _, width := range []int{120, 60, 44} {
		for _, update := range []string{"", "v0.3.0"} {
			p := NewPanel(map[string]bool{})
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
			if !labels["r"] || !labels["↵"] || !labels["x"] || !labels["esc"] {
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
	p := NewPanel(map[string]bool{})
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
	if out := p.Click(second, notices, testFrame()); out.Action != Stay || p.Cursor() != 1 {
		t.Fatalf("first click = %+v, cursor %d", out, p.Cursor())
	}
	out := p.Click(second, notices, testFrame())
	if notices[1].URL == "" {
		t.Fatalf("test setup: %q has no link", notices[1].ID)
	}
	if out.Action != OpenLink || out.URL != notices[1].URL {
		t.Fatalf("second click = %+v, want its link", out)
	}
	if out := p.Click(second+len(notices), notices, testFrame()); out.Action != Stay || p.Cursor() != 1 {
		t.Fatalf("a click under the titles = %+v, cursor %d", out, p.Cursor())
	}
}
