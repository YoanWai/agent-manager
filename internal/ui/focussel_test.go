package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
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

// A pane held taller than the panel crops at its content and never at the
// caret, which can sit below the content on an empty prompt row; the caret
// must land on the painted row the crop gave it.
func TestCaretRowSurvivesTallPaneCrop(t *testing.T) {
	rows := append([]string{"one", "two"}, make([]string, 38)...)
	m := paneAt(t, rows...)
	m.pane.box.height = 10
	m.pane.cursor = paneCursor{x: 0, y: 25, ok: true}
	row, col, ok := m.cursorCell(m.pane.box.height)
	if !ok || row != 9 || col != 0 {
		t.Fatalf("caret at pane row 25 = (%d,%d,%v), want painted row 9", row, col, ok)
	}
}

func press(m *Model, x, y int) {
	m.handleFocusMouse(tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: y})
}

func drag(m *Model, x, y int) {
	m.handleFocusMouse(tea.MouseMotionMsg{Button: tea.MouseLeft, X: x, Y: y})
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
	m.handleFocusMouse(tea.MouseClickMsg{
		Button: tea.MouseLeft, Mod: tea.ModAlt, X: 10, Y: 5,
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
		{name: "left", button: tea.MouseLeft, want: leftButton},
		{name: "middle", button: tea.MouseMiddle, want: middleButton},
		{name: "right", button: tea.MouseRight, want: rightButton},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Drive the real forwarding path through a focused tmux session,
			// so forwardFocusMouse reaches its send instead of returning
			// early on an empty selection.
			m, sess := focusedMouseApp(t, "mouse-tool", "release-"+tc.name)
			x, y := m.pane.box.x+2, m.pane.box.y+1

			m.handleFocusMouse(tea.MouseClickMsg{
				Button: tc.button, Mod: tea.ModAlt, X: x, Y: y,
			})
			if !m.forwardingMouse || m.sel.active {
				t.Fatalf("Alt press did not start forwarding: forwarding=%v selection=%v", m.forwardingMouse, m.sel.active)
			}
			if m.forwardingButton != tc.want {
				t.Fatalf("forwardingButton = %d, want %d", m.forwardingButton, tc.want)
			}

			// X10 reports a release as MouseButtonNone, so the stored pressed
			// button must supply the SGR release code that reaches the app.
			m.handleFocusMouse(tea.MouseReleaseMsg{
				Button: tea.MouseNone, X: x, Y: y,
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
	m.handleFocusMouse(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: 12, Y: 5})
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
	m.handleFocusMouse(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: 13, Y: 6})
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
	m.handleFocusMouse(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: 16, Y: 5})
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
	m.handleFocusMouse(tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: y})
	if m.sel.active {
		t.Fatal("press in a mouse pane should not open a selection")
	}
	m.handleFocusMouse(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: x, Y: y})
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

func TestDragSelectionKeepsCompleteGraphemes(t *testing.T) {
	for _, tc := range []struct {
		name       string
		line       string
		start, end int
		want       string
	}{
		{name: "wide rune end inside glyph", line: "甲乙丙丁", start: 2, end: 5, want: "乙丙"},
		{name: "emoji sequence", line: "x👩‍💻y", start: 1, end: 2, want: "👩‍💻"},
		{name: "nfd combining mark", line: "e\u0301 x", start: 0, end: 1, want: "e\u0301"},
		{name: "keycap sequence", line: "#\ufe0f\u20e3", start: 0, end: 1, want: "#\ufe0f\u20e3"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := paneAt(t, tc.line)
			press(m, m.pane.box.x+tc.start, m.pane.box.y)
			drag(m, m.pane.box.x+tc.end, m.pane.box.y)
			if got := m.selectionText(); got != tc.want {
				t.Fatalf("drag copied %q, want %q", got, tc.want)
			}
		})
	}
}

func TestWordSelectionKeepsWideGraphemeWhole(t *testing.T) {
	m := paneAt(t, "甲乙")
	m.sel = focusSelection{
		active: true, granule: selectWord,
		anchorRow: 0, anchorCol: 1,
		headRow: 0, headCol: 1,
	}
	m.expandSelection()
	if m.sel.anchorCol != 0 || m.sel.headCol != 4 {
		t.Fatalf("word bounds = [%d,%d), want [0,4)", m.sel.anchorCol, m.sel.headCol)
	}
	if got := m.selectionText(); got != "甲乙" {
		t.Fatalf("word selection copied %q, want %q", got, "甲乙")
	}
}

func TestWordSelectionKeepsCompleteGraphemes(t *testing.T) {
	for _, tc := range []struct {
		name string
		line string
		col  int
		want string
	}{
		{name: "nfd combining mark", line: "e\u0301 x", col: 0, want: "e\u0301"},
		{name: "keycap sequence", line: "#\ufe0f\u20e3", col: 0, want: "#\ufe0f\u20e3"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := paneAt(t, tc.line)
			m.sel = focusSelection{
				active: true, granule: selectWord,
				anchorRow: 0, anchorCol: tc.col,
				headRow: 0, headCol: tc.col,
			}
			m.expandSelection()
			if got := m.selectionText(); got != tc.want {
				t.Fatalf("word selection copied %q, want %q", got, tc.want)
			}
		})
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

func TestSelectionOverlayPreservesSurroundingANSI(t *testing.T) {
	m := paneAt(t, "\x1b[31mred\x1b[0m \x1b[34mblue\x1b[0m")
	m.sel = focusSelection{
		active:    true,
		anchorRow: 0,
		anchorCol: 3,
		headRow:   0,
		headCol:   4,
	}
	row := m.renderPaneRow(0, m.preview, 20)
	overlay := selectionStyle().Render(" ")
	redStart := strings.Index(row, "\x1b[31mred")
	overlayStart := strings.Index(row, overlay)
	blueStart := strings.Index(row, "\x1b[34mblue")
	if redStart < 0 || overlayStart < 0 || blueStart < 0 ||
		redStart >= overlayStart || overlayStart >= blueStart {
		t.Fatalf("selection overlay missing or misplaced: %q", row)
	}
}

// A selection edge landing on one cell of a wide grapheme snaps outward to
// the whole grapheme; the styled splice must keep the row's text intact
// rather than repeating the grapheme on both sides of the edge.
func TestSelectionOverlayKeepsWideGraphemesWhole(t *testing.T) {
	m := paneAt(t, "a\U0001f600b")
	m.sel = focusSelection{
		active:    true,
		anchorRow: 0,
		anchorCol: 0,
		headRow:   0,
		headCol:   2,
	}
	row := m.renderPaneRow(0, m.preview, 10)
	if plain := strings.TrimRight(ansi.Strip(row), " "); plain != "a\U0001f600b" {
		t.Fatalf("selection overlay changed row text: %q", plain)
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

	updated, _ := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	*m = *updated.(*Model)

	frame := splitLines(m.viewFrame())
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

	updated, _ := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	*m = *updated.(*Model)
	m.viewFrame()

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
	return []rune(ansi.Strip(row))
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

// The caret command-code parks on the pane's bottom row lives on the very
// blank row the old trim dropped: the pushed capture then held one row
// fewer than the caret's y, the bounds check bailed, and Left went dead on
// exactly the sessions whose caret rests at the bottom edge. Rows and
// caret copied from a live 182x47 pane (debug capture, 2026-08-26).
func TestBottomParkedCaretSurvivesControlCapture(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "ccpark", t.TempDir(), "")
	m.selectSessionRow(t, "ccpark")
	m.rows[m.cursor].sess.Tool = "command-code"
	rows := make([]string, 47)
	rows[36] = " TODOS  [4 items · 2 done] Sending Tier 3 messages… (paused) [ctrl+x to expand]"
	rows[38] = strings.Repeat("─", 60)
	rows[39] = "❯ Ask your question..."
	rows[40] = strings.Repeat("─", 60)
	rows[41] = "  » permission bypass on [shift+tab]"
	rows[42] = "  ? for shortcuts · PR #390 · taste on"
	controlJoin := strings.Join(rows, "\n")

	m.mode = modeFocus
	m.preview = matchExecShape(controlJoin)
	m.pane.forID = "s1"
	m.pane.cursor = paneCursor{x: 0, y: 46, ok: true}
	if got := len(strings.Split(strings.TrimSuffix(m.preview, "\n"), "\n")); got != 47 {
		t.Fatalf("preview kept %d rows, want all 47", got)
	}
	if !m.caretAtInputStart("s1", "command-code") {
		t.Fatal("the bottom-parked caret over an empty composer was not recognised")
	}

	// The same parked caret must not stretch the crop down to itself: the
	// rows between the footer and the caret are blank, and dragging them
	// into view floats the composer above a band of dead space.
	if got := m.paneCaretRow(); got != -1 {
		t.Fatalf("paneCaretRow = %d for a parked caret, want -1", got)
	}
	window, start := paneWindow(m.preview, 30, m.paneCaretRow())
	if last := window[len(window)-1]; strings.TrimSpace(last) != "? for shortcuts · PR #390 · taste on" {
		t.Fatalf("crop bottom = %q, want the footer, not blank fill (start=%d)", last, start)
	}

	// A caret inside the content keeps the crop pinned to it.
	m.pane.cursor = paneCursor{x: 2, y: 39, ok: true}
	if got := m.paneCaretRow(); got != 39 {
		t.Fatalf("paneCaretRow = %d for an in-content caret, want 39", got)
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

	const width = 30
	m := paneAt(t, "ok  \tgithub.com/x/y\t1.5s")
	m.pane.box.width = width
	rows := paneExact(m.preview, m.pane.box.height, width, -1)

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

// A click somewhere else ends the previous selection, in a pane that tracks
// the mouse and forwards the press as much as in one that selects on it: a
// highlight left standing reads as text still selected, and the copy
// confirmation belongs to the highlight it counted.
func TestClickElsewhereClearsSelection(t *testing.T) {
	for _, tracksMouse := range []bool{false, true} {
		m := paneAt(t, "alpha beta", "gamma delta")
		m.pane.mouse = tracksMouse
		press(m, 10, 5)
		drag(m, 14, 5)
		m.handleFocusMouse(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: 14, Y: 5})
		if got := m.selectionText(); got != "alph" {
			t.Fatalf("mouse=%v: drag selected %q", tracksMouse, got)
		}
		m.copied = 4

		press(m, 12, 6)
		if got := m.selectionText(); got != "" {
			t.Fatalf("mouse=%v: click elsewhere still selects %q", tracksMouse, got)
		}
		if m.copied != 0 {
			t.Fatalf("mouse=%v: click elsewhere kept the copy confirmation: %d", tracksMouse, m.copied)
		}
		m.handleFocusMouse(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: 12, Y: 6})
		if got := m.selectionText(); got != "" {
			t.Fatalf("mouse=%v: releasing the click restored the selection %q", tracksMouse, got)
		}
	}
}

// The clipboard writer runs off the update loop, so its confirmation can
// land after a click elsewhere has already dropped the highlight it counted.
// Re-arming the banner there would put "copied N chars" under no selection,
// which is the state this file exists to prevent.
func TestLateCopyConfirmationIsDroppedAfterTheSelectionGoes(t *testing.T) {
	m := paneAt(t, "alpha beta", "gamma delta")
	press(m, 10, 5)
	drag(m, 14, 5)
	m.handleFocusMouse(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: 14, Y: 5})
	inFlight := focusCopiedMsg{chars: 4, gen: m.copyGen}

	press(m, 12, 6)
	m.Update(inFlight)
	if m.copied != 0 {
		t.Fatalf("a write that landed after the click re-armed the count: %d", m.copied)
	}

	// The same confirmation still counts while its own selection stands.
	m2 := paneAt(t, "alpha beta", "gamma delta")
	press(m2, 10, 5)
	drag(m2, 14, 5)
	m2.handleFocusMouse(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: 14, Y: 5})
	m2.Update(focusCopiedMsg{chars: 4, gen: m2.copyGen})
	if m2.copied != 4 {
		t.Fatalf("the write for the standing selection was dropped: %d", m2.copied)
	}
}
