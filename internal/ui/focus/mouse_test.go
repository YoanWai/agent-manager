package focus

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	testOriginX = 10
	testOriginY = 5
)

func TestSelectionUsesDisplayedPaneCells(t *testing.T) {
	model := mouseModel(t, []string{"abcdef", "ghijkl"}, 40, PaneUpdate{})
	ctx := mouseContext(time.Unix(10, 0), 2)

	model.Mouse(leftPress(2, testOriginY), ctx)
	if model.selection.active {
		t.Fatal("press outside the displayed pane started a selection")
	}

	model.Mouse(leftPress(testOriginX+2, testOriginY), ctx)
	model.Mouse(tea.MouseMsg{
		Action: tea.MouseActionMotion,
		Button: tea.MouseButtonLeft,
		X:      testOriginX + 3,
		Y:      testOriginY + 1,
	}, ctx)
	result := model.Mouse(leftRelease(testOriginX+3, testOriginY+1), ctx)
	if result.Copy == nil || result.Copy.Text != "cdef\nghi" {
		t.Fatalf("drag copy = %+v, want cdef\\nghi", result.Copy)
	}
}

func TestAltSelectsPlainPaneAndForwardsClaimedPane(t *testing.T) {
	now := time.Unix(20, 0)
	plain := mouseModel(t, []string{"alpha beta"}, 40, PaneUpdate{})
	plain.Mouse(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
		Alt:    true,
		X:      testOriginX,
		Y:      testOriginY,
	}, mouseContext(now, 1))
	if !plain.selection.active {
		t.Fatal("Alt press on a plain pane did not start selection")
	}

	claimed := mouseModel(t, []string{"alpha beta"}, 40, PaneUpdate{Mouse: true, Motion: true, SGR: true})
	press := claimed.Mouse(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonMiddle,
		Alt:    true,
		X:      testOriginX + 2,
		Y:      testOriginY,
	}, mouseContext(now, 1))
	if claimed.selection.active || !claimed.forward.active {
		t.Fatalf("Alt press state: selection=%v forwarding=%v", claimed.selection.active, claimed.forward.active)
	}
	if !strings.Contains(press.SendReport, "\x1b[<35;3;1M") || !strings.Contains(press.SendReport, "\x1b[<1;3;1M") {
		t.Fatalf("forwarded press = %q, want motion then middle press", press.SendReport)
	}
	release := claimed.Mouse(tea.MouseMsg{
		Action: tea.MouseActionRelease,
		Button: tea.MouseButtonNone,
		X:      testOriginX + 80,
		Y:      testOriginY + 80,
	}, mouseContext(now, 1))
	if release.SendReport != "\x1b[<1;3;1m" || claimed.forward.active {
		t.Fatalf("outside release = %q forwarding=%v", release.SendReport, claimed.forward.active)
	}
}

func TestMouseBackReleasesForwardedGesture(t *testing.T) {
	model := mouseModel(t, []string{"alpha"}, 20, PaneUpdate{Mouse: true, SGR: true})
	ctx := mouseContext(time.Unix(30, 0), 1)
	model.Mouse(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
		Alt:    true,
		X:      testOriginX + 1,
		Y:      testOriginY,
	}, ctx)

	result := model.Mouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonBackward}, ctx)
	if !result.Leave || result.SendReport != "\x1b[<0;2;1m" {
		t.Fatalf("mouse back result = %+v, want leave plus release", result)
	}
	if model.forward.active {
		t.Fatal("mouse back left a forwarded button held")
	}
}

func TestClaimedPaneDistinguishesClickFromDrag(t *testing.T) {
	now := time.Unix(40, 0)
	ctx := mouseContext(now, 2)
	click := mouseModel(t, []string{"abcdef", "ghijkl"}, 40, PaneUpdate{Mouse: true, SGR: true})
	click.Mouse(leftPress(testOriginX+2, testOriginY), ctx)
	result := click.Mouse(leftRelease(testOriginX+2, testOriginY), ctx)
	if result.SendReport != "\x1b[<0;3;1M\x1b[<0;3;1m" || result.Copy != nil || click.selection.active {
		t.Fatalf("deferred click = %+v selection=%v", result, click.selection.active)
	}

	drag := mouseModel(t, []string{"abcdef", "ghijkl"}, 40, PaneUpdate{Mouse: true, SGR: true})
	drag.Mouse(leftPress(testOriginX+2, testOriginY), ctx)
	result = drag.Mouse(leftRelease(testOriginX+3, testOriginY+1), ctx)
	if result.SendReport != "" || result.Copy == nil || result.Copy.Text != "cdef\nghi" {
		t.Fatalf("motionless drag = %+v, want selection copy", result)
	}
}

func TestDoubleAndTripleClickSelection(t *testing.T) {
	model := mouseModel(t, []string{"first line", "alpha beta gamma"}, 40, PaneUpdate{})
	base := time.Unix(50, 0)
	for i := range 2 {
		ctx := mouseContext(base.Add(time.Duration(i)*time.Millisecond), 2)
		model.Mouse(leftPress(testOriginX+7, testOriginY+1), ctx)
	}
	word := model.Mouse(leftRelease(testOriginX+7, testOriginY+1), mouseContext(base.Add(2*time.Millisecond), 2))
	if word.Copy == nil || word.Copy.Text != "beta" {
		t.Fatalf("double click copy = %+v, want beta", word.Copy)
	}

	model.Mouse(leftPress(testOriginX+7, testOriginY+1), mouseContext(base.Add(3*time.Millisecond), 2))
	line := model.Mouse(leftRelease(testOriginX+7, testOriginY+1), mouseContext(base.Add(4*time.Millisecond), 2))
	if line.Copy == nil || line.Copy.Text != "alpha beta gamma" {
		t.Fatalf("triple click copy = %+v, want whole line", line.Copy)
	}
}

func TestSelectionKeepsWholeGraphemes(t *testing.T) {
	for _, test := range []struct {
		name       string
		line       string
		start, end int
		want       string
	}{
		{name: "wide runes", line: "甲乙丙丁", start: 2, end: 5, want: "乙丙"},
		{name: "emoji sequence", line: "x👩‍💻y", start: 1, end: 2, want: "👩‍💻"},
		{name: "combining mark", line: "e\u0301 x", start: 0, end: 1, want: "e\u0301"},
		{name: "keycap sequence", line: "#\ufe0f\u20e3", start: 0, end: 1, want: "#\ufe0f\u20e3"},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := mouseModel(t, []string{test.line}, 40, PaneUpdate{})
			ctx := mouseContext(time.Unix(60, 0), 1)
			model.Mouse(leftPress(testOriginX+test.start, testOriginY), ctx)
			model.Mouse(tea.MouseMsg{
				Action: tea.MouseActionMotion,
				Button: tea.MouseButtonLeft,
				X:      testOriginX + test.end,
				Y:      testOriginY,
			}, ctx)
			result := model.Mouse(leftRelease(testOriginX+test.end, testOriginY), ctx)
			if result.Copy == nil || result.Copy.Text != test.want {
				t.Fatalf("copy = %+v, want %q", result.Copy, test.want)
			}
		})
	}
}

func TestSelectionRenderingPreservesANSIAndText(t *testing.T) {
	preview := "\x1b[31mred\x1b[0m \x1b[34mblue\x1b[0m\n"
	model := mouseModel(t, []string{strings.TrimSuffix(preview, "\n")}, 20, PaneUpdate{})
	ctx := mouseContext(time.Unix(70, 0), 1)
	model.Mouse(leftPress(testOriginX+3, testOriginY), ctx)
	model.Mouse(tea.MouseMsg{
		Action: tea.MouseActionMotion,
		Button: tea.MouseButtonLeft,
		X:      testOriginX + 4,
		Y:      testOriginY,
	}, ctx)
	frame := model.PrepareFrame(FrameContext{
		Preview:        preview,
		Width:          20,
		Height:         1,
		OriginX:        testOriginX,
		OriginY:        testOriginY,
		CaretRow:       -1,
		CursorStyle:    lipgloss.NewStyle(),
		SelectionStyle: lipgloss.NewStyle().Reverse(true),
	})
	if len(frame.Rows) != 1 || strings.TrimRight(ansi.Strip(frame.Rows[0]), " ") != "red blue" {
		t.Fatalf("selection overlay changed pane text: %q", frame.Rows)
	}
	if !strings.Contains(frame.Rows[0], "\x1b[31m") || !strings.Contains(frame.Rows[0], "\x1b[34m") {
		t.Fatalf("selection overlay dropped surrounding colors: %q", frame.Rows[0])
	}
}

func TestLinkClicksUseDisplayedText(t *testing.T) {
	for _, tracking := range []bool{false, true} {
		model := mouseModel(t, []string{"read https://example.com/docs, then reply"}, 80, PaneUpdate{Mouse: tracking, SGR: true})
		ctx := mouseContext(time.Unix(80, 0), 1)
		model.Mouse(leftPress(testOriginX+8, testOriginY), ctx)
		result := model.Mouse(leftRelease(testOriginX+8, testOriginY), ctx)
		if result.OpenURL != "https://example.com/docs" || result.SendReport != "" || model.selection.active {
			t.Fatalf("tracking=%v link result=%+v selection=%v", tracking, result, model.selection.active)
		}
	}

	model := mouseModel(t, []string{
		"see (https://example.com/a).",
		"wiki https://en.wikipedia.org/wiki/Go_(language) rocks",
	}, 80, PaneUpdate{})
	if got := clickURL(&model, testOriginX+10, testOriginY); got != "https://example.com/a" {
		t.Fatalf("punctuated URL = %q", got)
	}
	if got := clickURL(&model, testOriginX+12, testOriginY+1); got != "https://en.wikipedia.org/wiki/Go_(language)" {
		t.Fatalf("balanced URL = %q", got)
	}
}

func TestWrappedLinksAndSchemes(t *testing.T) {
	wrapped := mouseModel(t, []string{
		"https://example.com/",
		"long/path more text",
	}, 20, PaneUpdate{})
	if got := clickURL(&wrapped, testOriginX+5, testOriginY); got != "https://example.com/long/path" {
		t.Fatalf("wrapped URL = %q", got)
	}

	indented := mouseModel(t, []string{
		"https://example.com/",
		"  indented follow-up",
	}, 20, PaneUpdate{})
	if got := clickURL(&indented, testOriginX+5, testOriginY); got != "https://example.com/" {
		t.Fatalf("indented continuation = %q", got)
	}

	schemes := mouseModel(t, []string{"open file:///etc/passwd or ftp://host/x now"}, 80, PaneUpdate{})
	if got := clickURL(&schemes, testOriginX+8, testOriginY); got != "" {
		t.Fatalf("file URL opened as %q", got)
	}
	if got := clickURL(&schemes, testOriginX+30, testOriginY); got != "" {
		t.Fatalf("ftp URL opened as %q", got)
	}
}

func TestMouseReportEncodingsAndFrameRowOffset(t *testing.T) {
	if got, want := sgrMouse(motionButton, 0, 0), "\x1b[<35;1;1M"; got != want {
		t.Fatalf("motion report = %q, want %q", got, want)
	}
	if got, want := sgrMouse(wheelDownButton, 11, 4), "\x1b[<65;12;5M"; got != want {
		t.Fatalf("wheel down report = %q, want %q", got, want)
	}
	if got, ok := x10Mouse(wheelUpButton, 0, 0); !ok || got != "\x1b[M`!!" {
		t.Fatalf("X10 wheel = %q ok=%v", got, ok)
	}
	if got, ok := x10Mouse(motionButton, 11, 4); !ok || got != "\x1b[MC,%" {
		t.Fatalf("X10 motion = %q ok=%v, want %q", got, ok, "\x1b[MC,%")
	}
	if _, ok := x10Mouse(wheelUpButton, x10Limit, 4); ok {
		t.Fatal("X10 encoded a column outside its range")
	}
	if _, ok := x10Mouse(wheelUpButton, 4, x10Limit); ok {
		t.Fatal("X10 encoded a row outside its range")
	}

	model := mouseModel(t, []string{"a", "b", "c", "d", "one", "two"}, 40, PaneUpdate{Mouse: true, SGR: true})
	model.PrepareFrame(FrameContext{
		Preview:        "a\nb\nc\nd\none\ntwo\n",
		Width:          40,
		Height:         2,
		OriginX:        testOriginX,
		OriginY:        testOriginY,
		CaretRow:       -1,
		CursorStyle:    lipgloss.NewStyle(),
		SelectionStyle: lipgloss.NewStyle(),
	})
	result := model.Mouse(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelUp,
		X:      testOriginX + 3,
		Y:      testOriginY + 1,
	}, MouseContext{SessionID: "s1", Rows: 2, RuntimeReady: true})
	if result.SendReport != "\x1b[<64;4;6M" {
		t.Fatalf("cropped-frame wheel report = %q", result.SendReport)
	}
}

func TestTallPaneCropKeepsCaretOnPaintedRow(t *testing.T) {
	rows := append([]string{"one", "two"}, make([]string, 38)...)
	var model Model
	model.Enter(EnterContext{SessionID: "s1"})
	model.ApplyPane(PaneUpdate{
		SessionID: "s1",
		Cursor:    Cursor{X: 0, Y: 25, Visible: true, PositionKnown: true},
	}, "s1")
	model.PrepareFrame(FrameContext{
		Preview:        strings.Join(rows, "\n") + "\n",
		Width:          40,
		Height:         10,
		OriginX:        testOriginX,
		OriginY:        testOriginY,
		CaretRow:       25,
		CursorStyle:    lipgloss.NewStyle(),
		SelectionStyle: lipgloss.NewStyle(),
	})
	row, col, ok := model.cursorCell()
	if !ok || row != 9 || col != 0 {
		t.Fatalf("cropped caret = (%d,%d,%v), want painted row 9", row, col, ok)
	}
}

func TestLinkAtFindsTheLinkUnderTheClick(t *testing.T) {
	model := mouseModel(t, []string{
		"read https://example.com/docs, then reply",
		"no link on this row",
	}, 80, PaneUpdate{})
	if got := model.linkAt(0, 2); got != "" {
		t.Fatalf("click before the link = %q, want none", got)
	}
}

// X10 reports a release as MouseButtonNone, so the stored pressed button
// must supply the SGR release code that reaches the app.
func TestAltMouseForwardingKeepsTheRelease(t *testing.T) {
	model := mouseModel(t, []string{"alpha beta"}, 40, PaneUpdate{Mouse: true, SGR: true})
	ctx := mouseContext(time.Unix(150, 0), 1)
	press := model.Mouse(tea.MouseMsg{
		Action: tea.MouseActionPress, Button: tea.MouseButtonRight, Alt: true,
		X: testOriginX + 2, Y: testOriginY,
	}, ctx)
	if !model.forward.active || model.selection.active {
		t.Fatalf("Alt press did not start forwarding: forwarding=%v selection=%v", model.forward.active, model.selection.active)
	}
	if model.forward.button != rightButton {
		t.Fatalf("forwarded button = %d, want %d", model.forward.button, rightButton)
	}
	if press.SendReport != "\x1b[<2;3;1M" {
		t.Fatalf("forwarded press = %q, want the right-button press", press.SendReport)
	}

	release := model.Mouse(tea.MouseMsg{
		Action: tea.MouseActionRelease, Button: tea.MouseButtonNone,
		X: testOriginX + 2, Y: testOriginY,
	}, ctx)
	if release.SendReport != "\x1b[<2;3;1m" {
		t.Fatalf("forwarded release = %q, want the right-button release", release.SendReport)
	}
	if model.forward.active || model.forward.button != leftButton {
		t.Fatalf("release did not clear forwarding state: active=%v button=%d", model.forward.active, model.forward.button)
	}
}

func mouseModel(t *testing.T, rows []string, width int, pane PaneUpdate) Model {
	t.Helper()
	var model Model
	model.Enter(EnterContext{SessionID: "s1"})
	pane.SessionID = "s1"
	model.ApplyPane(pane, "s1")
	model.PrepareFrame(FrameContext{
		Preview:        strings.Join(rows, "\n") + "\n",
		Width:          width,
		Height:         len(rows),
		OriginX:        testOriginX,
		OriginY:        testOriginY,
		CaretRow:       -1,
		CursorStyle:    lipgloss.NewStyle(),
		SelectionStyle: lipgloss.NewStyle(),
	})
	return model
}

func mouseContext(now time.Time, rows int) MouseContext {
	return MouseContext{SessionID: "s1", Rows: rows, Now: now, RuntimeReady: true}
}

func leftPress(x, y int) tea.MouseMsg {
	return tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: y}
}

func leftRelease(x, y int) tea.MouseMsg {
	return tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: x, Y: y}
}

func clickURL(model *Model, x, y int) string {
	ctx := mouseContext(time.Unix(90, 0), model.FrameBox().Height)
	model.Mouse(leftPress(x, y), ctx)
	return model.Mouse(leftRelease(x, y), ctx).OpenURL
}
