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
	m.paneBox = paneBox{x: 10, y: 5, width: 40, height: len(lines), ok: true}
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
	if !m.paneBox.ok {
		t.Fatal("pane box never recorded")
	}
	if m.paneBox.y >= len(frame) {
		t.Fatalf("pane box row %d past frame of %d rows", m.paneBox.y, len(frame))
	}
	row := plainCells(frame[m.paneBox.y])
	if m.paneBox.x+len(marker) > len(row) {
		t.Fatalf("pane box x %d past row width %d", m.paneBox.x, len(row))
	}
	got := string(row[m.paneBox.x : m.paneBox.x+len(marker)])
	if got != marker {
		t.Fatalf("row %d at column %d = %q, want %q\nrow: %q",
			m.paneBox.y, m.paneBox.x, got, marker, string(row))
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

	y := m.paneBox.y + 1
	for i := 0; i < 3; i++ {
		press(m, m.paneBox.x+3, y)
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
	applyPaneState(&msg, "12,34,010,250,0\n")
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
	applyPaneState(&motion, "0,0,100,0,1")
	if !motion.paneMouse || !motion.paneMotion {
		t.Fatalf("all-motion pane state = %+v", motion)
	}

	var plain focusPreviewMsg
	applyPaneState(&plain, "0,0,000,0,0")
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
	m.paneCursor = paneCursor{x: 2, y: 1, ok: true}
	row, col, ok := m.cursorCell(3)
	if !ok || row != 1 || col != 2 {
		t.Fatalf("cursorCell = (%d,%d,%v), want (1,2,true)", row, col, ok)
	}

	// A capture taller than the panel is shown from its bottom, so the
	// cursor row shifts by the lines the panel dropped.
	tall := paneAt(t, "r0", "r1", "r2", "r3", "r4")
	tall.paneBox.height = 3
	tall.paneCursor = paneCursor{x: 4, y: 4, ok: true}
	row, col, ok = tall.cursorCell(3)
	if !ok || row != 2 || col != 4 {
		t.Fatalf("shifted cursorCell = (%d,%d,%v), want (2,4,true)", row, col, ok)
	}

	// A cursor above the visible window is not drawn.
	tall.paneCursor = paneCursor{x: 0, y: 1, ok: true}
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
	m.paneCursor = paneCursor{x: 1, y: 0, ok: true}
	withCursor := m.renderPaneRow(0, "abc", 20)
	if !strings.Contains(withCursor, "\x1b[") {
		t.Fatalf("cursor row carries no styling: %q", withCursor)
	}
	if ansi.Strip(withCursor) != "abc" {
		t.Fatalf("cursor changed the row text: %q", ansi.Strip(withCursor))
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
	m.paneCursor = paneCursor{x: 5, y: 0, ok: true}
	row := m.renderPaneRow(0, "ab", 20)
	if !strings.Contains(row, "\x1b[") {
		t.Fatalf("no cursor drawn past end of line: %q", row)
	}
	if plain := strings.TrimRight(ansi.Strip(row), " "); plain != "ab" {
		t.Fatalf("cursor changed the line text: %q", plain)
	}
}
