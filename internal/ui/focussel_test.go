package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// paneAt sets up a model with a known pane box and captured text so
// selection can be driven in terminal coordinates.
func paneAt(t *testing.T, lines ...string) *Model {
	t.Helper()
	m := &Model{}
	m.mode = modeFocus
	m.cursorOn = true
	m.preview = strings.Join(lines, "\n") + "\n"
	m.pane.box = paneBox{x: 10, y: 5, width: 40, height: len(lines), ok: true}
	return m
}

func press(m *Model, x, y int) {
	m.handleFocusMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: y})
}

func drag(m *Model, x, y int) {
	m.handleFocusMouse(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: x, Y: y})
}

// A click outside the pane selects nothing: that region belongs to the
// rail, and the whole point of focus mode is that it is a closed window.
func TestSelectionIgnoresOutsidePane(t *testing.T) {
	m := paneAt(t, "alpha beta", "gamma delta")
	press(m, 2, 6)
	if m.sel.active {
		t.Fatal("click on the rail started a selection")
	}
	if _, _, ok := m.paneCell(9, 5); ok {
		t.Fatal("column left of the pane hit-tested inside it")
	}
	if _, _, ok := m.paneCell(50, 5); ok {
		t.Fatal("column right of the pane hit-tested inside it")
	}
}

// Alt is only a pass-through gesture for applications that claim the mouse;
// a plain pane keeps its ordinary selection and copy behavior.
func TestAltClickSelectsPlainPane(t *testing.T) {
	m := paneAt(t, "alpha beta")
	m.handleFocusMouse(tea.MouseMsg{
		Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Alt: true, X: 10, Y: 5,
	})
	if !m.sel.active {
		t.Fatal("Alt-click on a plain pane did not start a selection")
	}
}

func TestAltMouseForwardingKeepsTheRelease(t *testing.T) {
	for _, tc := range []struct {
		name   string
		button tea.MouseButton
		want   int
	}{
		{name: "left", button: tea.MouseButtonLeft, want: leftButton},
		{name: "middle", button: tea.MouseButtonMiddle, want: middleButton},
		{name: "right", button: tea.MouseButtonRight, want: rightButton},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Drive the real forwarding path through a focused tmux session,
			// so forwardFocusMouse reaches its send instead of returning
			// early on an empty selection.
			m, sess := focusedMouseApp(t, "mouse-tool", "release-"+tc.name)
			x, y := m.pane.box.x+2, m.pane.box.y+1

			m.handleFocusMouse(tea.MouseMsg{
				Action: tea.MouseActionPress, Button: tc.button, Alt: true, X: x, Y: y,
			})
			if !m.forwardingMouse || m.sel.active {
				t.Fatalf("Alt press did not start forwarding: forwarding=%v selection=%v", m.forwardingMouse, m.sel.active)
			}
			if m.forwardingButton != tc.want {
				t.Fatalf("forwardingButton = %d, want %d", m.forwardingButton, tc.want)
			}

			// X10 reports a release as MouseButtonNone, so the stored pressed
			// button must supply the SGR release code that reaches the app.
			m.handleFocusMouse(tea.MouseMsg{
				Action: tea.MouseActionRelease, Button: tea.MouseButtonNone, X: x, Y: y,
			})
			if m.forwardingMouse || m.forwardingButton != leftButton {
				t.Fatalf("release did not clear forwarding state: active=%v button=%d", m.forwardingMouse, m.forwardingButton)
			}

			deadline := time.Now().Add(5 * time.Second)
			for {
				pane, err := m.tmux.CapturePane(sess.ID)
				if err != nil {
					t.Fatalf("capture: %v", err)
				}
				// A press report (terminator M) and its matching release (m),
				// both carrying the button that was pressed.
				if sgrMouseReportRe(tc.want, false).MatchString(pane) &&
					sgrMouseReportRe(tc.want, true).MatchString(pane) {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("release report never reached the pane: %q", pane)
				}
				time.Sleep(50 * time.Millisecond)
			}
		})
	}
}

// In a mouse-tracking pane a press is held back: motion proves it a
// selection drag anchored at the press cell, and the app never sees it.
func TestDeferredPressBecomesDragSelection(t *testing.T) {
	m := paneAt(t, "abcdef", "ghijkl")
	m.pane.mouse = true
	press(m, 12, 5)
	if m.sel.active || !m.pending.active {
		t.Fatalf("press in a mouse pane should defer, not select: sel=%v pending=%v", m.sel.active, m.pending.active)
	}
	drag(m, 13, 6)
	if m.pending.active {
		t.Fatal("motion should resolve the pending press")
	}
	if got := m.selectionText(); got != "cdef\nghi" {
		t.Fatalf("deferred drag copied %q", got)
	}
}

// Release at the press cell proves a click; nothing selects, and with no
// live focus session the forward is simply dropped.
func TestDeferredPressReleaseIsAClick(t *testing.T) {
	m := paneAt(t, "alpha beta")
	m.pane.mouse = true
	press(m, 12, 5)
	m.handleFocusMouse(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: 12, Y: 5})
	if m.pending.active || m.sel.active {
		t.Fatalf("click left state behind: pending=%v sel=%v", m.pending.active, m.sel.active)
	}
}

// A terminal limited to plain button reporting sends no motion between
// press and release, so a release away from the press cell is that
// terminal's drag: it selects instead of clicking or vanishing.
func TestDeferredPressMotionlessDragSelects(t *testing.T) {
	m := paneAt(t, "abcdef", "ghijkl")
	m.pane.mouse = true
	press(m, 12, 5)
	m.handleFocusMouse(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: 13, Y: 6})
	if m.pending.active {
		t.Fatal("release should resolve the pending press")
	}
	if got := m.selectionText(); got != "cdef\nghi" {
		t.Fatalf("motionless drag copied %q", got)
	}
}

// The second press of a double click stays on the selection path even in a
// mouse-tracking pane, so word and line copy keep working there.
func TestDoubleClickStillSelectsInMousePane(t *testing.T) {
	m := paneAt(t, "alpha beta gamma")
	m.pane.mouse = true
	press(m, 16, 5)
	m.handleFocusMouse(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: 16, Y: 5})
	press(m, 16, 5)
	if got := m.selectionText(); got != "beta" {
		t.Fatalf("double click in a mouse pane copied %q", got)
	}
}

// A deferred click that reaches a live pane arrives as a complete
// press-release pair.
func TestDeferredClickForwardsPressReleasePair(t *testing.T) {
	m, sess := focusedMouseApp(t, "mouse-tool", "deferred-click")
	x, y := m.pane.box.x+2, m.pane.box.y+1
	m.handleFocusMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: y})
	if m.sel.active {
		t.Fatal("press in a mouse pane should not open a selection")
	}
	m.handleFocusMouse(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: x, Y: y})
	deadline := time.Now().Add(5 * time.Second)
	for {
		pane, err := m.tmux.CapturePane(sess.ID)
		if err != nil {
			t.Fatalf("capture: %v", err)
		}
		if sgrMouseReportRe(leftButton, false).MatchString(pane) &&
			sgrMouseReportRe(leftButton, true).MatchString(pane) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("click pair never reached the pane: %q", pane)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Triple click takes exactly the pane line it landed on.
func TestTripleClickSelectsPaneLineOnly(t *testing.T) {
	m := paneAt(t, "first line here", "second line here")
	for i := 0; i < 3; i++ {
		press(m, 13, 6)
	}
	if got := m.selectionText(); got != "second line here" {
		t.Fatalf("triple click copied %q", got)
	}
}

func TestDoubleClickSelectsWord(t *testing.T) {
	m := paneAt(t, "alpha beta gamma")
	press(m, 16, 5)
	press(m, 16, 5)
	if got := m.selectionText(); got != "beta" {
		t.Fatalf("double click copied %q", got)
	}
}

func TestDragSelectsAcrossRows(t *testing.T) {
	m := paneAt(t, "abcdef", "ghijkl")
	press(m, 12, 5)
	drag(m, 13, 6)
	if got := m.selectionText(); got != "cdef\nghi" {
		t.Fatalf("drag copied %q", got)
	}
}

// Mouse coordinates are terminal cells, not rune offsets. A CJK glyph uses
// two cells, so treating the cell as a rune index selected text to the right
// of the pointer.
func TestDragSelectsWideRunesAtDisplayColumns(t *testing.T) {
	m := paneAt(t, "甲乙丙丁")
	press(m, 12, 5) // display column 2: 乙
	drag(m, 16, 5)  // display column 6: before 丁
	if got := m.selectionText(); got != "乙丙" {
		t.Fatalf("drag copied %q, want %q", got, "乙丙")
	}
}

// Selection indices are taken from the plain text of a colored capture, so
// escape sequences never shift what gets copied.
func TestSelectionIgnoresANSI(t *testing.T) {
	m := paneAt(t, "\x1b[31mred\x1b[0m plain")
	press(m, 10, 5)
	press(m, 10, 5)
	if got := m.selectionText(); got != "red" {
		t.Fatalf("double click on colored text copied %q", got)
	}
}

func TestSelectionSurvivesShortLines(t *testing.T) {
	m := paneAt(t, "ab", "")
	press(m, 10, 5)
	drag(m, 40, 6)
	if got := m.selectionText(); got != "ab\n" {
		t.Fatalf("copied %q", got)
	}
}

// The recorded pane box must match where the frame actually paints the
// captured rows: every selection coordinate depends on it.
func TestPaneBoxMatchesPaintedFrame(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "boxed", t.TempDir(), "")
	m.selectSessionRow(t, "boxed")
	marker := "PANE-BOX-MARKER"
	m.preview = marker + "\nsecond row\n"

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)

	frame := splitLines(m.View())
	if !m.pane.box.ok {
		t.Fatal("pane box never recorded")
	}
	if m.pane.box.y >= len(frame) {
		t.Fatalf("pane box row %d past frame of %d rows", m.pane.box.y, len(frame))
	}
	row := plainCells(frame[m.pane.box.y])
	if m.pane.box.x+len(marker) > len(row) {
		t.Fatalf("pane box x %d past row width %d", m.pane.box.x, len(row))
	}
	got := string(row[m.pane.box.x : m.pane.box.x+len(marker)])
	if got != marker {
		t.Fatalf("row %d at column %d = %q, want %q\nrow: %q",
			m.pane.box.y, m.pane.box.x, got, marker, string(row))
	}
}

// A triple click on a painted pane row copies that row and nothing from
// the rail beside it.
func TestTripleClickOnRealFrameTakesPaneRowOnly(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "railmate", t.TempDir(), "")
	m.selectSessionRow(t, "railmate")
	m.preview = "pane row one\npane row two\n"

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	m.View()

	y := m.pane.box.y + 1
	for i := 0; i < 3; i++ {
		press(m, m.pane.box.x+3, y)
	}
	got := m.selectionText()
	if got != "pane row two" {
		t.Fatalf("triple click copied %q, want the pane row alone", got)
	}
	if strings.Contains(got, "railmate") {
		t.Fatalf("selection leaked rail content: %q", got)
	}
}

// plainCells is a painted frame row as visible cells, escape sequences
// removed, so a column index in the frame is a column on screen.
func plainCells(row string) []rune {
	return []rune(strings.NewReplacer("\u200e", "", "\u2066", "", "\u2069", "").Replace(ansi.Strip(row)))
}

// A capture taken by the slower poll path must not repaint over a frame
// the control client just pushed: that race is what makes typed
// characters blink in and out while focused.
func TestPushedPreviewWinsOverStalePoll(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "typing", t.TempDir(), "")
	m.selectSessionRow(t, "typing")
	sess := m.rows[m.cursor].sess

	m.focus = newFocusWatch(m.tmux, func(tea.Msg) {})
	m.focus.setFocus(sess.ID)
	t.Cleanup(m.focus.Close)

	deadline := time.Now().Add(5 * time.Second)
	for !m.focus.serving(sess.ID) {
		if time.Now().After(deadline) {
			t.Skip("control client never came up on this host")
		}
		time.Sleep(20 * time.Millisecond)
	}

	fresh := "typed-just-now"
	updated, _ := m.Update(focusPreviewMsg{sessID: sess.ID, preview: fresh})
	*m = *updated.(*Model)
	if m.preview != fresh {
		t.Fatalf("pushed preview not stored: %q", m.preview)
	}

	updated, _ = m.Update(previewMsg{sessID: sess.ID, preview: "stale-capture"})
	*m = *updated.(*Model)
	if m.preview != fresh {
		t.Fatalf("stale tick capture overwrote the pushed frame: %q", m.preview)
	}

	updated, _ = m.Update(refreshMsg{sessions: m.sessions, procFor: sess.ID, preview: "stale-poll"})
	*m = *updated.(*Model)
	if m.preview != fresh {
		t.Fatalf("stale poll capture overwrote the pushed frame: %q", m.preview)
	}
}

// Once the watcher lets go, the ordinary capture paths own the preview
// again — otherwise an unfocused session would freeze on its last frame.
func TestPollPreviewResumesAfterWatcherStops(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "released", t.TempDir(), "")
	m.selectSessionRow(t, "released")
	sess := m.rows[m.cursor].sess

	m.focus = newFocusWatch(m.tmux, func(tea.Msg) {})
	m.focus.setFocus(sess.ID)
	m.focus.Close()

	updated, _ := m.Update(previewMsg{sessID: sess.ID, preview: "polled"})
	*m = *updated.(*Model)
	if m.preview != "polled" {
		t.Fatalf("preview after watcher stop = %q, want the polled capture", m.preview)
	}
}

// Trailing blank pane rows are content: dropping them shifts every line
// up, which is what made the pushed and polled captures disagree.
func TestControlCaptureKeepsTrailingBlankRows(t *testing.T) {
	pane := "top line\n\n\n"
	if got, want := matchExecShape(pane), "top line\n\n\n"; got != want {
		t.Fatalf("matchExecShape(%q) = %q, want %q", pane, got, want)
	}
	if got := len(strings.Split(strings.TrimSuffix(matchExecShape(pane), "\n"), "\n")); got != 3 {
		t.Fatalf("kept %d rows, want 3", got)
	}
}

func TestApplyPaneState(t *testing.T) {
	var msg focusPreviewMsg
	applyPaneState(&msg, "12,34,010,250,0,1\n")
	if !msg.cursorOK || msg.cursorX != 12 || msg.cursorY != 34 {
		t.Fatalf("cursor = (%d,%d,%v)", msg.cursorX, msg.cursorY, msg.cursorOK)
	}
	if !msg.paneMouse {
		t.Fatal("mouse flag 1 not read as pane-owned mouse")
	}
	if msg.historySize != 250 {
		t.Fatalf("historySize = %d, want 250", msg.historySize)
	}
	if msg.paneMotion {
		t.Fatal("a button-tracking pane read as tracking all motion")
	}

	// tmux reports 1003 in mouse_all_flag, the apps that want a pointer
	// move with every event.
	var motion focusPreviewMsg
	applyPaneState(&motion, "0,0,100,0,1,1")
	if !motion.paneMouse || !motion.paneMotion {
		t.Fatalf("all-motion pane state = %+v", motion)
	}

	var plain focusPreviewMsg
	applyPaneState(&plain, "0,0,000,0,0,0")
	if plain.paneMouse || plain.paneMotion || plain.historySize != 0 || !plain.cursorOK {
		t.Fatalf("plain pane state = %+v", plain)
	}

	// tmux answers an unquoted format with its default status message;
	// that must never read as pane state.
	var bogus focusPreviewMsg
	applyPaneState(&bogus, "[am_x] 0:zsh, current pane 0 - (00:43)")
	if bogus.cursorOK || bogus.paneMouse || bogus.historySize != 0 {
		t.Fatal("applyPaneState accepted tmux's default message")
	}
	applyPaneState(&bogus, "nonsense")
	if bogus.cursorOK {
		t.Fatal("applyPaneState accepted junk")
	}
}

// The cursor is drawn at the pane cell tmux reported, shifted when the
// capture is taller than the panel showing its bottom rows.
func TestCursorCellMapsIntoVisibleRows(t *testing.T) {
	m := paneAt(t, "one", "two", "three")
	m.pane.cursor = paneCursor{x: 2, y: 1, ok: true}
	row, col, ok := m.cursorCell(3)
	if !ok || row != 1 || col != 2 {
		t.Fatalf("cursorCell = (%d,%d,%v), want (1,2,true)", row, col, ok)
	}

	// A capture taller than the panel is shown from its bottom, so the
	// cursor row shifts by the lines the panel dropped.
	tall := paneAt(t, "r0", "r1", "r2", "r3", "r4")
	tall.pane.box.height = 3
	tall.pane.cursor = paneCursor{x: 4, y: 4, ok: true}
	row, col, ok = tall.cursorCell(3)
	if !ok || row != 2 || col != 4 {
		t.Fatalf("shifted cursorCell = (%d,%d,%v), want (2,4,true)", row, col, ok)
	}

	// A cursor above the visible window is not drawn.
	tall.pane.cursor = paneCursor{x: 0, y: 1, ok: true}
	if _, _, ok := tall.cursorCell(3); ok {
		t.Fatal("cursor above the shown rows was mapped in")
	}
}

// The drawn row carries the cursor cell; unfocused rows stay untouched.
func TestCursorPaintedOnItsRow(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	m := paneAt(t, "abc", "def")
	m.pane.cursor = paneCursor{x: 1, y: 0, ok: true}
	withCursor := m.renderPaneRow(0, "abc", 20)
	if !strings.Contains(withCursor, "\x1b[") {
		t.Fatalf("cursor row carries no styling: %q", withCursor)
	}
	if plain := strings.TrimRight(string(plainCells(withCursor)), " "); plain != "abc" {
		t.Fatalf("cursor changed the row text: %q", plain)
	}
	if other := m.renderPaneRow(1, "def", 20); other != previewLine("def", 20) {
		t.Fatalf("non-cursor row was restyled: %q", other)
	}
}

// tmux reports the cursor in display cells, so a wide rune ahead of it
// must not shift the drawn cursor off its cell.
func TestRuneAtColumn(t *testing.T) {
	cases := []struct {
		name   string
		line   string
		column int
		index  int
	}{
		{"ascii", "abc", 1, 1},
		{"after wide rune", "世界x", 4, 2},
		{"inside wide rune", "世界x", 1, 0},
		{"past end", "ab", 4, 2},
		{"empty line", "", 0, 0},
	}
	for _, c := range cases {
		if index := runeAtColumn([]rune(c.line), c.column); index != c.index {
			t.Errorf("%s: runeAtColumn(%q,%d) = %d, want %d",
				c.name, c.line, c.column, index, c.index)
		}
	}
}

// The cursor is drawn even when it sits past the end of a short line,
// which is where a shell prompt leaves it most of the time.
func TestCursorPastEndOfLine(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	m := paneAt(t, "ab")
	m.pane.cursor = paneCursor{x: 5, y: 0, ok: true}
	row := m.renderPaneRow(0, "ab", 20)
	if !strings.Contains(row, "\x1b[") {
		t.Fatalf("no cursor drawn past end of line: %q", row)
	}
	if plain := strings.TrimRight(string(plainCells(row)), " "); plain != "ab" {
		t.Fatalf("cursor changed the line text: %q", plain)
	}
}

// tmux reports the caret and takes the mouse in painted cells, so on a row
// `go test` wrote the tab's cells have to count for the caret and the
// selection the same way they count for the paint.
func TestTabbedRowSharesItsColumns(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	const width = 30
	m := paneAt(t, "ok  \tgithub.com/x/y\t1.5s")
	m.pane.box.width = width
	rows := paneExact(m.preview, m.pane.box.height, width)

	// Column 8 is where the pane paints the package name's first letter.
	m.pane.cursor = paneCursor{x: 8, y: 0, ok: true}
	row := m.renderPaneRow(0, rows[0], width)
	if !strings.Contains(row, cursorStyle().Render("g")) {
		t.Fatalf("caret missed the cell tmux reported: %q", row)
	}

	// The caret at the end of the painted row still lands inside the frame.
	m.pane.cursor = paneCursor{x: 28, y: 0, ok: true}
	if end := m.renderPaneRow(0, rows[0], width); !strings.Contains(end, "\x1b[") ||
		ansi.StringWidth(end) != width {
		t.Fatalf("caret at the row's end paints %d cells: %q", ansi.StringWidth(end), end)
	}

	m.pane.cursor = paneCursor{}
	press(m, m.pane.box.x+8, m.pane.box.y)
	drag(m, m.pane.box.x+15, m.pane.box.y)
	if text := m.selectionText(); text != "github." {
		t.Fatalf("dragging over the painted columns copied %q", text)
	}
}
