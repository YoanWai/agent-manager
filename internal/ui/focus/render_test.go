package focus

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// litStyle renders through a TrueColor renderer of its own, so a painted
// cell carries escapes without touching the package-wide color profile.
func litStyle() lipgloss.Style {
	renderer := lipgloss.NewRenderer(io.Discard)
	renderer.SetColorProfile(termenv.TrueColor)
	return renderer.NewStyle().Reverse(true)
}

func caretAt(x, y int) PaneUpdate {
	return PaneUpdate{Cursor: Cursor{X: x, Y: y, Visible: true, PositionKnown: true}}
}

// The cursor is drawn at the pane cell tmux reported, shifted when the
// capture is taller than the panel showing its bottom rows.
func TestCursorCellMapsIntoVisibleRows(t *testing.T) {
	model := mouseModel(t, []string{"one", "two", "three"}, 40, caretAt(2, 1))
	row, col, ok := model.cursorCell()
	if !ok || row != 1 || col != 2 {
		t.Fatalf("cursorCell = (%d,%d,%v), want (1,2,true)", row, col, ok)
	}

	var tall Model
	tall.Enter(EnterContext{SessionID: "s1"})
	tall.PrepareFrame(FrameContext{
		Preview:  "r0\nr1\nr2\nr3\nr4\n",
		Width:    40,
		Height:   3,
		OriginX:  testOriginX,
		OriginY:  testOriginY,
		CaretRow: -1,
	})
	update := caretAt(4, 4)
	update.SessionID = "s1"
	tall.ApplyPane(update, "s1")
	row, col, ok = tall.cursorCell()
	if !ok || row != 2 || col != 4 {
		t.Fatalf("shifted cursorCell = (%d,%d,%v), want (2,4,true)", row, col, ok)
	}

	// A cursor above the visible window is not drawn.
	update = caretAt(0, 1)
	update.SessionID = "s1"
	tall.ApplyPane(update, "s1")
	if _, _, ok := tall.cursorCell(); ok {
		t.Fatal("cursor above the shown rows was mapped in")
	}
}

// The drawn row carries the cursor cell; unfocused rows stay untouched.
func TestCursorPaintedOnItsRow(t *testing.T) {
	model := mouseModel(t, []string{"abc", "def"}, 20, caretAt(1, 0))
	withCursor := model.renderPaneRow(0, "abc", 20, litStyle(), litStyle())
	if !strings.Contains(withCursor, "\x1b[") {
		t.Fatalf("cursor row carries no styling: %q", withCursor)
	}
	if plain := strings.TrimRight(ansi.Strip(withCursor), " "); plain != "abc" {
		t.Fatalf("cursor changed the row text: %q", plain)
	}
	if other := model.renderPaneRow(1, "def", 20, litStyle(), litStyle()); other != "def"+strings.Repeat(" ", 17) {
		t.Fatalf("non-cursor row was restyled: %q", other)
	}
}

// The cursor is drawn even when it sits past the end of a short line,
// which is where a shell prompt leaves it most of the time.
func TestCursorPastEndOfLine(t *testing.T) {
	model := mouseModel(t, []string{"ab"}, 20, caretAt(5, 0))
	row := model.renderPaneRow(0, "ab", 20, litStyle(), litStyle())
	if !strings.Contains(row, "\x1b[") {
		t.Fatalf("no cursor drawn past end of line: %q", row)
	}
	if plain := strings.TrimRight(ansi.Strip(row), " "); plain != "ab" {
		t.Fatalf("cursor changed the line text: %q", plain)
	}
}

func TestSelectionOverlayPreservesSurroundingANSI(t *testing.T) {
	line := "\x1b[31mred\x1b[0m \x1b[34mblue\x1b[0m"
	model := mouseModel(t, []string{line}, 20, PaneUpdate{})
	model.selection = selection{active: true, anchorRow: 0, anchorCol: 3, headRow: 0, headCol: 4}
	row := model.renderPaneRow(0, line, 20, lipgloss.NewStyle(), litStyle())
	overlay := litStyle().Render(" ")
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
	model := mouseModel(t, []string{"a\U0001f600b"}, 10, PaneUpdate{})
	model.selection = selection{active: true, anchorRow: 0, anchorCol: 0, headRow: 0, headCol: 2}
	row := model.renderPaneRow(0, "a\U0001f600b", 10, lipgloss.NewStyle(), litStyle())
	if plain := strings.TrimRight(ansi.Strip(row), " "); plain != "a\U0001f600b" {
		t.Fatalf("selection overlay changed row text: %q", plain)
	}
}

// tmux reports the caret and takes the mouse in painted cells, so on a row
// `go test` wrote the tab's cells have to count for the caret and the
// selection the same way they count for the paint.
func TestTabbedRowSharesItsColumns(t *testing.T) {
	const width = 30
	line := "ok  \tgithub.com/x/y\t1.5s"
	model := mouseModel(t, []string{line}, width, PaneUpdate{})
	rows, _ := paneExact(line+"\n", 1, width, -1)

	// Column 8 is where the pane paints the package name's first letter.
	update := caretAt(8, 0)
	update.SessionID = "s1"
	model.ApplyPane(update, "s1")
	if row := model.renderPaneRow(0, rows[0], width, litStyle(), litStyle()); !strings.Contains(row, litStyle().Render("g")) {
		t.Fatalf("caret missed the cell tmux reported: %q", row)
	}

	// The caret at the end of the painted row still lands inside the frame.
	update = caretAt(28, 0)
	update.SessionID = "s1"
	model.ApplyPane(update, "s1")
	if end := model.renderPaneRow(0, rows[0], width, litStyle(), litStyle()); !strings.Contains(end, "\x1b[") ||
		ansi.StringWidth(end) != width {
		t.Fatalf("caret at the row's end paints %d cells: %q", ansi.StringWidth(end), end)
	}

	update = PaneUpdate{SessionID: "s1"}
	model.ApplyPane(update, "s1")
	ctx := mouseContext(time.Unix(200, 0), 1)
	model.Mouse(leftPress(testOriginX+8, testOriginY), ctx)
	model.Mouse(leftMotion(testOriginX+15, testOriginY), ctx)
	if text := model.selectionText(); text != "github." {
		t.Fatalf("dragging over the painted columns copied %q", text)
	}
}
