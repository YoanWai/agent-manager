package focus

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func leftMotion(x, y int) tea.MouseMsg {
	return tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: x, Y: y}
}

// A click somewhere else ends the previous selection, in a pane that tracks
// the mouse and forwards the press as much as in one that selects on it: a
// highlight left standing reads as text still selected, and the copy
// confirmation belongs to the highlight it counted.
func TestClickElsewhereClearsSelection(t *testing.T) {
	for _, tracksMouse := range []bool{false, true} {
		model := mouseModel(t, []string{"alpha beta", "gamma delta"}, 40, PaneUpdate{Mouse: tracksMouse})
		ctx := mouseContext(time.Unix(100, 0), 2)
		model.Mouse(leftPress(10, 5), ctx)
		model.Mouse(leftMotion(14, 5), ctx)
		model.Mouse(leftRelease(14, 5), ctx)
		if got := model.selectionText(); got != "alph" {
			t.Fatalf("mouse=%v: drag selected %q", tracksMouse, got)
		}
		model.copied = 4

		model.Mouse(leftPress(12, 6), ctx)
		if got := model.selectionText(); got != "" {
			t.Fatalf("mouse=%v: click elsewhere still selects %q", tracksMouse, got)
		}
		if model.copied != 0 {
			t.Fatalf("mouse=%v: click elsewhere kept the copy confirmation: %d", tracksMouse, model.copied)
		}
		model.Mouse(leftRelease(12, 6), ctx)
		if got := model.selectionText(); got != "" {
			t.Fatalf("mouse=%v: releasing the click restored the selection %q", tracksMouse, got)
		}
	}
}

// In a mouse-tracking pane a press is held back: motion proves it a
// selection drag anchored at the press cell, and the app never sees it.
func TestDeferredPressBecomesDragSelection(t *testing.T) {
	model := mouseModel(t, []string{"abcdef", "ghijkl"}, 40, PaneUpdate{Mouse: true})
	ctx := mouseContext(time.Unix(110, 0), 2)
	model.Mouse(leftPress(12, 5), ctx)
	if model.selection.active || !model.pending.active {
		t.Fatalf("press in a mouse pane should defer, not select: sel=%v pending=%v", model.selection.active, model.pending.active)
	}
	if result := model.Mouse(leftMotion(13, 6), ctx); result.SendReport != "" {
		t.Fatalf("drag motion reached the app: %q", result.SendReport)
	}
	if model.pending.active {
		t.Fatal("motion should resolve the pending press")
	}
	if got := model.selectionText(); got != "cdef\nghi" {
		t.Fatalf("deferred drag copied %q", got)
	}
}

// The second press of a double click stays on the selection path even in a
// mouse-tracking pane, so word and line copy keep working there.
func TestDoubleClickStillSelectsInMousePane(t *testing.T) {
	model := mouseModel(t, []string{"alpha beta gamma"}, 40, PaneUpdate{Mouse: true})
	ctx := mouseContext(time.Unix(120, 0), 1)
	model.Mouse(leftPress(16, 5), ctx)
	model.Mouse(leftRelease(16, 5), ctx)
	model.Mouse(leftPress(16, 5), ctx)
	if got := model.selectionText(); got != "beta" {
		t.Fatalf("double click in a mouse pane copied %q", got)
	}
}

// A click outside the pane selects nothing: that region belongs to the
// rail, and focus mode is a closed window on the pane.
func TestSelectionIgnoresOutsidePane(t *testing.T) {
	model := mouseModel(t, []string{"alpha beta", "gamma delta"}, 40, PaneUpdate{})
	if _, _, ok := model.paneCell(50, 5); ok {
		t.Fatal("column right of the pane hit-tested inside it")
	}
}

// Selection indices are taken from the plain text of a colored capture, so
// escape sequences never shift what gets copied.
func TestSelectionIgnoresANSI(t *testing.T) {
	model := mouseModel(t, []string{"\x1b[31mred\x1b[0m plain"}, 40, PaneUpdate{})
	ctx := mouseContext(time.Unix(130, 0), 1)
	model.Mouse(leftPress(10, 5), ctx)
	model.Mouse(leftPress(10, 5), ctx)
	if got := model.selectionText(); got != "red" {
		t.Fatalf("double click on colored text copied %q", got)
	}
}

func TestSelectionSurvivesShortLines(t *testing.T) {
	model := mouseModel(t, []string{"ab", "", "later"}, 40, PaneUpdate{})
	ctx := mouseContext(time.Unix(140, 0), 3)
	model.Mouse(leftPress(10, 5), ctx)
	model.Mouse(leftMotion(40, 6), ctx)
	if got := model.selectionText(); got != "ab\n" {
		t.Fatalf("copied %q", got)
	}
}

func TestWordSelectionKeepsWideGraphemeWhole(t *testing.T) {
	model := mouseModel(t, []string{"甲乙"}, 40, PaneUpdate{})
	model.selection = selection{
		active: true, granule: selectWord,
		anchorRow: 0, anchorCol: 1,
		headRow: 0, headCol: 1,
	}
	model.expandSelection()
	if model.selection.anchorCol != 0 || model.selection.headCol != 4 {
		t.Fatalf("word bounds = [%d,%d), want [0,4)", model.selection.anchorCol, model.selection.headCol)
	}
	if got := model.selectionText(); got != "甲乙" {
		t.Fatalf("word selection copied %q, want %q", got, "甲乙")
	}
}

func TestWordSelectionKeepsCompleteGraphemes(t *testing.T) {
	for _, test := range []struct {
		name string
		line string
		col  int
		want string
	}{
		{name: "nfd combining mark", line: "é x", col: 0, want: "é"},
		{name: "keycap sequence", line: "#️⃣", col: 0, want: "#️⃣"},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := mouseModel(t, []string{test.line}, 40, PaneUpdate{})
			model.selection = selection{
				active: true, granule: selectWord,
				anchorRow: 0, anchorCol: test.col,
				headRow: 0, headCol: test.col,
			}
			model.expandSelection()
			if got := model.selectionText(); got != test.want {
				t.Fatalf("word selection copied %q, want %q", got, test.want)
			}
		})
	}
}
