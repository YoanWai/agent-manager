package focus

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestKeyPriorityAndScrollResume(t *testing.T) {
	var model Model
	model.Enter(EnterContext{SessionID: "s1"})
	model.pane.history = 40
	model.scroll = 9

	got := model.Key(tea.KeyMsg{Type: tea.KeyLeft}, KeyContext{
		SessionID:   "s1",
		Rows:        12,
		Review:      true,
		ArrowStep:   true,
		AtInputHead: true,
	})
	if got.Action != OpenReview {
		t.Fatalf("review action = %v, want OpenReview", got.Action)
	}
	if model.scroll != 9 {
		t.Fatalf("reserved key changed scroll to %d", model.scroll)
	}

	got = model.Key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")}, KeyContext{SessionID: "s1", Rows: 12})
	if got.Action != ForwardKey || got.Region == nil || got.Region.Offset != 0 {
		t.Fatalf("forward result = %+v, want key plus live-region fetch", got)
	}
	if model.scroll != 0 || !model.fetchInFlight {
		t.Fatalf("resume state scroll=%d inFlight=%v", model.scroll, model.fetchInFlight)
	}
}

func TestScrollCoalescesOntoLatestTarget(t *testing.T) {
	var model Model
	model.Enter(EnterContext{SessionID: "s1"})
	model.pane.history = 100

	first := model.Scroll(-1, "s1", 20)
	if first == nil || first.Offset != 3 {
		t.Fatalf("first request = %+v, want offset 3", first)
	}
	if next := model.Scroll(-1, "s1", 20); next != nil {
		t.Fatalf("second wheel queued another capture: %+v", next)
	}
	if model.scroll != 6 {
		t.Fatalf("target = %d, want 6", model.scroll)
	}

	stale := model.ApplyRegion(RegionResult{SessionID: "s1", Offset: 3, Rows: 20, OK: true}, "s1", 20)
	if stale.Apply || stale.Next == nil || stale.Next.Offset != 6 {
		t.Fatalf("stale completion = %+v, want one catch-up request", stale)
	}
	fresh := model.ApplyRegion(RegionResult{SessionID: "s1", Offset: 6, Rows: 20, Preview: "fresh\n", OK: true}, "s1", 20)
	if !fresh.Apply || fresh.Preview != "fresh\n" || fresh.Next != nil || model.fetchInFlight {
		t.Fatalf("fresh completion = %+v inFlight=%v", fresh, model.fetchInFlight)
	}
}

func TestCopyCompletionIsBoundToStandingSelection(t *testing.T) {
	model := renderedModel(t, "alpha beta\n", 20, 1)
	now := time.Unix(10, 0)
	model.Mouse(tea.MouseMsg{X: 1, Y: 2, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}, MouseContext{SessionID: "s1", Now: now})
	model.Mouse(tea.MouseMsg{X: 6, Y: 2, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft}, MouseContext{SessionID: "s1", Now: now})
	result := model.Mouse(tea.MouseMsg{X: 6, Y: 2, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft}, MouseContext{SessionID: "s1", Now: now})
	if result.Copy == nil || result.Copy.Text != "alpha" {
		t.Fatalf("copy request = %+v, want alpha", result.Copy)
	}

	model.clearSelection()
	if model.ApplyCopied(result.Copy.Generation, 5) {
		t.Fatal("stale clipboard completion was accepted")
	}
	if model.Status().CopiedChars != 0 {
		t.Fatalf("stale completion restored copied count: %+v", model.Status())
	}
}

func TestMouseReadsDisplayedFrameGeometryAndText(t *testing.T) {
	model := renderedModel(t, "https://example.com/path\n", 30, 1)

	outside := model.Mouse(tea.MouseMsg{X: 0, Y: 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}, MouseContext{SessionID: "s1", Now: time.Unix(20, 0)})
	if outside != (MouseResult{}) {
		t.Fatalf("outside press = %+v", outside)
	}

	now := time.Unix(21, 0)
	model.Mouse(tea.MouseMsg{X: 2, Y: 2, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}, MouseContext{SessionID: "s1", Now: now})
	opened := model.Mouse(tea.MouseMsg{X: 2, Y: 2, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft}, MouseContext{SessionID: "s1", Now: now})
	if opened.OpenURL != "https://example.com/path" {
		t.Fatalf("opened URL = %q", opened.OpenURL)
	}
}

func TestRenderedFrameProducesExactCursorAnchor(t *testing.T) {
	var model Model
	model.Enter(EnterContext{SessionID: "s1"})
	model.ApplyPane(PaneUpdate{
		SessionID: "s1",
		Cursor:    Cursor{X: 4, Y: 1, Visible: true, PositionKnown: true},
	}, "s1")
	model.PrepareFrame(FrameContext{
		Preview:        "first\nsecond\n",
		Width:          10,
		Height:         2,
		OriginX:        7,
		OriginY:        5,
		CaretRow:       1,
		CursorStyle:    lipgloss.NewStyle(),
		SelectionStyle: lipgloss.NewStyle(),
	})

	col, row, ok := model.CursorAnchor("s1")
	if !ok || col != 12 || row != 7 {
		t.Fatalf("anchor = (%d,%d,%v), want (12,7,true)", col, row, ok)
	}
}

func TestLivePaneUpdateHoldsPreviewWhileScrolled(t *testing.T) {
	var model Model
	model.Enter(EnterContext{SessionID: "s1"})
	model.pane.history = 80
	model.scroll = 9

	result := model.ApplyPane(PaneUpdate{SessionID: "s1", History: 80}, "s1")
	if result.UsePreview || model.scroll != 9 {
		t.Fatalf("history update = %+v scroll=%d", result, model.scroll)
	}
	result = model.ApplyPane(PaneUpdate{SessionID: "s1", Mouse: true}, "s1")
	if !result.UsePreview || model.scroll != 0 {
		t.Fatalf("mouse-owned update = %+v scroll=%d", result, model.scroll)
	}
}

func TestPreparedFrameStripsCapturedCursorMarker(t *testing.T) {
	model := renderedModel(t, "pane"+capturedCursorMarker+"output\n", 20, 1)
	frame := model.PrepareFrame(FrameContext{
		Preview:        "pane" + capturedCursorMarker + "output\n",
		Width:          20,
		Height:         1,
		CaretRow:       -1,
		CursorStyle:    lipgloss.NewStyle(),
		SelectionStyle: lipgloss.NewStyle(),
	})
	if len(frame.Rows) != 1 || strings.Contains(frame.Rows[0], capturedCursorMarker) {
		t.Fatalf("prepared row retained private cursor marker: %q", frame.Rows)
	}
}

func renderedModel(t *testing.T, preview string, width, height int) Model {
	t.Helper()
	var model Model
	model.Enter(EnterContext{SessionID: "s1"})
	model.PrepareFrame(FrameContext{
		Preview:        preview,
		Width:          width,
		Height:         height,
		OriginX:        1,
		OriginY:        2,
		CaretRow:       -1,
		CursorStyle:    lipgloss.NewStyle(),
		SelectionStyle: lipgloss.NewStyle(),
	})
	return model
}
