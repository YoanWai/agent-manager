package ui

import (
	"fmt"
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A bare Model must default to mouse reporting on: mouseDisabled is named
// for its off polarity precisely so a zero-valued struct, the shape most
// tests build, does not silently disable the mouse everywhere.
func TestSyncMouseCaptureDefaultsOn(t *testing.T) {
	m := &Model{mode: modeList}
	// Nothing has changed from the zero value's implied "captured" state,
	// so there is nothing to tell the terminal.
	if cmd := m.syncMouseCapture(); cmd != nil {
		t.Fatal("default state should not re-announce mouse capture")
	}
	if m.mouseReleased {
		t.Fatal("mouse should stay captured by default")
	}
}

// The mouse-mode setting releases the mouse everywhere except focus mode,
// whose own forwarding predates the setting and stays on regardless.
func TestSyncMouseCaptureRespectsMouseDisabled(t *testing.T) {
	m := &Model{mode: modeList, prefs: preferences{mouseDisabled: true}}
	if cmd := m.syncMouseCapture(); cmd == nil {
		t.Fatal("turning the setting off should release the mouse")
	}
	if !m.mouseReleased {
		t.Fatal("mouseReleased should follow the setting in list mode")
	}
	m.mode = modeFocus
	if cmd := m.syncMouseCapture(); cmd == nil {
		t.Fatal("entering focus mode should re-capture regardless of the setting")
	}
	if m.mouseReleased {
		t.Fatal("focus mode must keep the mouse captured even with the setting off")
	}
}

func TestDragReleasePersistsAndExits(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	m := &Model{

		mode:   modeList,
		width:  100,
		height: 40,
		split:  splitState{ratio: defaultSplitRatio}, services: services{listKeys: keybind.DefaultList(),
			store: st},
	}
	updated, _ := m.enterResizeMode()
	m = updated.(*Model)

	div := m.dividerX()
	// Body starts at the header's height; any y inside the body range works.
	updated, _ = m.handleMouse(tea.MouseMsg{
		X: div, Y: 5, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	if !m.split.dragging {
		t.Fatal("press on divider should start drag")
	}

	updated, _ = m.handleMouse(tea.MouseMsg{
		X: 50, Y: 5, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	if left, _ := m.splitWidths(); left != 50 {
		t.Fatalf("motion should set left=50, got %d", left)
	}

	updated, cmd := m.handleMouse(tea.MouseMsg{
		X: 50, Y: 5, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	if m.split.dragging || m.split.resizeMode {
		t.Fatal("release should end drag and exit resize mode")
	}
	if cmd != nil {
		t.Fatal("release should not toggle mouse reporting")
	}

	raw, err := st.Setting(splitRatioSetting)
	if err != nil {
		t.Fatalf("read setting: %v", err)
	}
	got, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		t.Fatalf("parse setting %q: %v", raw, err)
	}
	if got != 0.5 {
		t.Fatalf("persisted ratio = %v want 0.5", got)
	}
}

// Motion updates the live ratio only; tmux resize happens once on release.
func TestDragResizesTmuxOnlyOnRelease(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "split-drag", t.TempDir(), "")
	id := m.sessionRows()[0].ID
	m.split.ratio = defaultSplitRatio
	m.resizeSessions()
	before := windowWidth(t, id)
	if before != m.previewPaneWidth() {
		t.Fatalf("setup width = %d want %d", before, m.previewPaneWidth())
	}

	// Drift the session away so a real resize is observable.
	if _, err := tmuxCmd("resize-window", "-t", "am_"+id, "-x", "100", "-y", "30").CombinedOutput(); err != nil {
		t.Fatalf("resize-window: %v", err)
	}
	if w := windowWidth(t, id); w != 100 {
		t.Fatalf("drifted width = %d want 100", w)
	}

	updated, _ := m.enterResizeMode()
	m = updated.(*Model)
	div := m.dividerX()
	y0, _ := m.bodyYRange()
	updated, _ = m.handleMouse(tea.MouseMsg{
		X: div, Y: y0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	updated, _ = m.handleMouse(tea.MouseMsg{
		X: 50, Y: y0, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	if w := windowWidth(t, id); w != 100 {
		t.Fatalf("motion must not resize tmux, width = %d want 100", w)
	}

	updated, _ = m.handleMouse(tea.MouseMsg{
		X: 50, Y: y0, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	// After exit, grip is gone; measure the committed preview width.
	wantPreview := m.previewPaneWidth()
	if wantPreview == 100 {
		t.Fatal("test setup: preview width should differ from drifted 100")
	}
	if w := windowWidth(t, id); w != wantPreview {
		t.Fatalf("release should resize once to preview width, got %d want %d", w, wantPreview)
	}
}

func TestPressOutsideBodyDoesNotDrag(t *testing.T) {
	m := &Model{
		mode:   modeList,
		width:  100,
		height: 40,
		split:  splitState{ratio: 0.34, resizeMode: true}, services: services{listKeys: keybind.DefaultList()},
	}
	div := m.dividerX()
	updated, _ := m.handleMouse(tea.MouseMsg{
		X: div, Y: 0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	if m.split.dragging {
		t.Fatal("press on header row must not start drag")
	}
	y0, y1 := m.bodyYRange()
	if y0 != m.listChromeRows() {
		t.Fatalf("body start = %d want %d", y0, m.listChromeRows())
	}
	updated, _ = m.handleMouse(tea.MouseMsg{
		X: div, Y: y1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	if m.split.dragging {
		t.Fatal("press on exclusive body end must not start drag")
	}
}

func TestDragCancelRestoresRatio(t *testing.T) {
	m := &Model{
		mode:   modeList,
		width:  100,
		height: 40,
		split:  splitState{ratio: 0.34}, services: services{listKeys: keybind.DefaultList()},
	}
	updated, _ := m.enterResizeMode()
	m = updated.(*Model)
	div := m.dividerX()
	updated, _ = m.handleMouse(tea.MouseMsg{
		X: div, Y: 5, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	updated, _ = m.handleMouse(tea.MouseMsg{
		X: 55, Y: 5, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	if left, _ := m.splitWidths(); left != 55 {
		t.Fatalf("pre-cancel left = %d want 55", left)
	}

	updated, _ = m.exitResizeMode(false)
	m = updated.(*Model)
	if m.split.resizeMode || m.split.dragging {
		t.Fatal("cancel should clear resize state")
	}
	if left, _ := m.splitWidths(); left != 34 {
		t.Fatalf("cancel should restore left=34, got %d", left)
	}
}

func TestPressOffDividerDoesNotDrag(t *testing.T) {
	m := &Model{
		mode:   modeList,
		width:  100,
		height: 40,
		split:  splitState{ratio: 0.34, resizeMode: true}, services: services{listKeys: keybind.DefaultList()},
	}
	updated, _ := m.handleMouse(tea.MouseMsg{
		X: 5, Y: 5, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	if m.split.dragging {
		t.Fatal("press far from divider should not start drag")
	}
}

// paintedRailLines paints the frame the real UI paints and returns the body
// lines it put each named session on. Going through View() is the point: a
// test that worked the rail's geometry out for itself would stay green the
// day viewListFrame started passing entryLines something else, which is the
// drift clickRow exists to rule out (#110).
func paintedRailLines(t *testing.T, m *Model, name string) []int {
	t.Helper()
	m.View()
	var lines []int
	for i, row := range m.rail.railHits {
		if row >= 0 && !m.rail.rows[row].isGroup && m.rail.rows[row].sess.Name == name {
			lines = append(lines, i)
		}
	}
	if len(lines) == 0 {
		t.Fatalf("test setup: %q painted no rail line", name)
	}
	return lines
}

func paintedGroupLines(t *testing.T, m *Model, path string) []int {
	t.Helper()
	m.View()
	var lines []int
	for i, row := range m.rail.railHits {
		if row >= 0 && m.rail.rows[row].isGroup && m.rail.rows[row].group == path {
			lines = append(lines, i)
		}
	}
	if len(lines) == 0 {
		t.Fatalf("test setup: group %q painted no rail line", path)
	}
	return lines
}

// A click on a session row selects it, reading the geometry the frame
// recorded while painting rather than re-deriving it (#110).
func TestClickSelectsRow(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "beta")

	line := paintedRailLines(t, m, "alpha")[0]
	y0, _ := m.bodyYRange()
	updated, cmd := m.handleMouse(tea.MouseMsg{
		X: 2, Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	sess, ok := m.selected()
	if !ok || sess.Name != "alpha" {
		t.Fatalf("click should select alpha, got %q ok=%v", sess.Name, ok)
	}
	if cmd == nil {
		t.Fatal("selecting a different row should schedule a preview")
	}
	if m.mode != modeList {
		t.Fatalf("first click should select, not focus, mode = %v", m.mode)
	}
}

func TestClickOnAnotherRowFromFocusFocusesIt(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	updated, _ := m.focusSelected()
	m = updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("test setup: focus alpha, mode = %v, err = %q", m.mode, m.errBar.text)
	}

	line := paintedRailLines(t, m, "beta")[0]
	y0, _ := m.bodyYRange()
	at := tea.MouseMsg{X: 2, Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	updated, _ = m.handleMouse(at)
	m = updated.(*Model)
	if m.mode != modeList {
		t.Fatalf("the press should leave focus, mode = %v", m.mode)
	}
	at.Action = tea.MouseActionRelease
	updated, _ = m.handleMouse(at)
	m = updated.(*Model)
	if sess, ok := m.selected(); m.mode != modeFocus || !ok || sess.Name != "beta" {
		t.Fatalf("the release should focus beta, mode = %v got %q", m.mode, sess.Name)
	}
}

func TestClickOnFocusedSessionRowLeavesFocus(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	updated, _ := m.focusSelected()
	m = updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("test setup: focus alpha, mode = %v, err = %q", m.mode, m.errBar.text)
	}

	line := paintedRailLines(t, m, "alpha")[0]
	y0, _ := m.bodyYRange()
	at := tea.MouseMsg{X: 2, Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	updated, _ = m.handleMouse(at)
	m = updated.(*Model)
	at.Action = tea.MouseActionRelease
	updated, _ = m.handleMouse(at)
	m = updated.(*Model)
	if m.mode != modeList {
		t.Fatalf("click on the focused row should leave focus, mode = %v", m.mode)
	}
	if sess, ok := m.selected(); !ok || sess.Name != "alpha" {
		t.Fatalf("selection should stay on alpha, got %q ok=%v", sess.Name, ok)
	}
}

func TestClickInFocusedPaneStaysFocused(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	updated, _ := m.focusSelected()
	m = updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("test setup: focus alpha, mode = %v, err = %q", m.mode, m.errBar.text)
	}
	m.View()
	if !m.focusPane.pane.box.ok {
		t.Fatal("test setup: focused pane has no box")
	}
	updated, _ = m.handleMouse(tea.MouseMsg{
		X: m.focusPane.pane.box.x, Y: m.focusPane.pane.box.y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("click in the pane should stay focused, mode = %v", m.mode)
	}
}

// Double click is the full layout's focus gesture. The pointer names the
// row, not the cursor: a wheel notch, a j or the poll can walk the cursor
// away between the two presses.
func TestDoubleClickFocusesTheRowUnderThePointer(t *testing.T) {
	m := buildModel(t)
	m.prefs.fullLayout = true
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")

	line := paintedRailLines(t, m, "alpha")[0]
	y0, _ := m.bodyYRange()
	press := tea.MouseMsg{X: 2, Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	updated, _ := m.handleMouse(press)
	m = updated.(*Model)
	m.selectSessionRow(t, "beta")

	updated, _ = m.handleMouse(press)
	m = updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("double click should focus, mode = %v, err = %q", m.mode, m.errBar.text)
	}
	if sess, ok := m.selected(); !ok || sess.Name != "alpha" {
		t.Fatalf("double click should focus the row it landed on, got %q ok=%v", sess.Name, ok)
	}
}

// The pair is matched on the row's identity: a rebuild between the presses
// renumbers m.rows, so an index that meant this row can mean another.
func TestDoubleClickPairsAcrossARebuild(t *testing.T) {
	m := buildModel(t)
	m.prefs.fullLayout = true
	dir := t.TempDir()
	for _, group := range []string{"aaa", "zzz"} {
		if err := m.services.store.CreateGroup(group, dir); err != nil {
			t.Fatalf("group %s: %v", group, err)
		}
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "filler", dir, "aaa")
	createSession(t, m, "target", dir, "zzz")

	y0, _ := m.bodyYRange()
	line := paintedRailLines(t, m, "target")[0]
	updated, _ := m.handleMouse(tea.MouseMsg{
		X: 2, Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	before := m.rail.cursor

	// Folding the group above target drops every row under it an index.
	m.rail.collapsed["aaa"] = true
	m.rebuildRows()
	line = paintedRailLines(t, m, "target")[0]
	m.selectSessionRow(t, "target")
	if m.rail.cursor == before {
		t.Fatal("test setup: folding should have renumbered target's row")
	}
	m.rail.cursor = before

	updated, _ = m.handleMouse(tea.MouseMsg{
		X: 2, Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("the pair should survive a rebuild, mode = %v, err = %q", m.mode, m.errBar.text)
	}
	if sess, ok := m.selected(); !ok || sess.Name != "target" {
		t.Fatalf("should focus target, got %q ok=%v", sess.Name, ok)
	}
}

// A session that has painted nothing yet leaves no pane box, and its
// column is still its own: clicking it must not eject the user.
func TestClickInTheFocusedColumnStaysFocused(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	updated, _ := m.focusSelected()
	m = updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("test setup: focus alpha, mode = %v, err = %q", m.mode, m.errBar.text)
	}

	for _, tc := range []struct {
		name    string
		preview string
	}{
		{"nothing captured yet", ""},
		{"a capture shorter than the column", "one line"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m.workspace.preview = tc.preview
			m.View()
			y0, _ := m.bodyYRange()
			updated, _ := m.handleMouse(tea.MouseMsg{
				X: m.paneOriginX(), Y: y0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
			})
			m = updated.(*Model)
			if m.mode != modeFocus {
				t.Fatalf("a click in the session's own column must not leave focus, mode = %v", m.mode)
			}
		})
	}
}

// Full screen focus paints no rail, so the list frame's hits must not
// outlive it and hand a click a row nobody pointed at.
func TestClickInFullScreenFocusStaysFocused(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "beta")
	m.View()

	m.prefs.fullLayout = true
	m.selectSessionRow(t, "alpha")
	updated, _ := m.focusSelected()
	m = updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("test setup: focus alpha, mode = %v, err = %q", m.mode, m.errBar.text)
	}
	m.View()

	y0, _ := m.bodyYRange()
	updated, _ = m.handleMouse(tea.MouseMsg{
		X: 2, Y: y0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("a click with no list on screen must not leave focus, mode = %v", m.mode)
	}
	if sess, ok := m.selected(); !ok || sess.Name != "alpha" {
		t.Fatalf("selection should stay on alpha, got %q ok=%v", sess.Name, ok)
	}
}

func TestDoubleClickFocusesTheRowJustSelected(t *testing.T) {
	m := buildModel(t)
	m.prefs.fullLayout = true
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "beta")

	line := paintedRailLines(t, m, "alpha")[0]
	y0, _ := m.bodyYRange()
	press := tea.MouseMsg{X: 2, Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	updated, _ := m.handleMouse(press)
	m = updated.(*Model)
	if m.mode != modeList {
		t.Fatalf("first click should select, mode = %v", m.mode)
	}
	updated, _ = m.handleMouse(press)
	m = updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("double click should focus alpha, mode = %v, err = %q", m.mode, m.errBar.text)
	}
}

func TestDoubleClickFocusesWhenEnterAttaches(t *testing.T) {
	m := buildModel(t)
	m.prefs.fullLayout = true
	m.prefs.focusOnEnter = false
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")

	line := paintedRailLines(t, m, "alpha")[0]
	y0, _ := m.bodyYRange()
	press := tea.MouseMsg{X: 2, Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	updated, _ := m.handleMouse(press)
	m = updated.(*Model)
	updated, _ = m.handleMouse(press)
	m = updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("double click should focus even when Enter attaches, mode = %v, err = %q", m.mode, m.errBar.text)
	}
}

func TestSlowSecondClickDoesNotFocus(t *testing.T) {
	m := buildModel(t)
	m.prefs.fullLayout = true
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")

	line := paintedRailLines(t, m, "alpha")[0]
	y0, _ := m.bodyYRange()
	press := tea.MouseMsg{X: 2, Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	updated, _ := m.handleMouse(press)
	m = updated.(*Model)
	m.rail.listClickAt = time.Now().Add(-multiClickWindow - time.Millisecond)
	updated, _ = m.handleMouse(press)
	m = updated.(*Model)
	if m.mode != modeList {
		t.Fatalf("a slow second click should not focus, mode = %v", m.mode)
	}
}

func TestClickOnSelectedGroupTogglesCollapse(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("work", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "alpha", dir, "work")
	m.selectGroupRow(t, "work")
	if m.rail.collapsed["work"] {
		t.Fatal("test setup: work should start open")
	}

	line := paintedGroupLines(t, m, "work")[0]
	y0, _ := m.bodyYRange()
	press := tea.MouseMsg{X: 2, Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	updated, _ := m.handleMouse(press)
	m = updated.(*Model)
	updated, _ = m.handleMouse(press)
	m = updated.(*Model)
	if !m.rail.collapsed["work"] {
		t.Fatal("double click on the selected group should fold it")
	}

	line = paintedGroupLines(t, m, "work")[0]
	y0, _ = m.bodyYRange()
	press.Y = y0 + line
	updated, _ = m.handleMouse(press)
	m = updated.(*Model)
	updated, _ = m.handleMouse(press)
	m = updated.(*Model)
	if m.rail.collapsed["work"] {
		t.Fatal("a second double click should unfold it")
	}
}

// The rail's last painted column carries row text, so it belongs to the
// row under it. The divider is resolved before the row is, so a hit target
// that reached back over that column would cost every row its right edge.
func TestClickOnRailLastColumnSelectsRow(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "beta")

	line := paintedRailLines(t, m, "alpha")[0]
	y0, _ := m.bodyYRange()
	updated, _ := m.handleMouse(tea.MouseMsg{
		X: m.dividerX() - 1, Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	if m.split.dragging || m.split.resizeMode {
		t.Fatal("the rail's own last column must not grab the divider")
	}
	if sess, ok := m.selected(); !ok || sess.Name != "alpha" {
		t.Fatalf("click on the rail's last column should select alpha, got %q ok=%v", sess.Name, ok)
	}
}

// A click past the divider, in the content column, must not steal the
// selection: the rail is what click-to-select owns.
func TestClickInContentColumnDoesNotSelect(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "beta")
	before := m.rail.cursor

	m.View()
	y0, _ := m.bodyYRange()
	updated, cmd := m.handleMouse(tea.MouseMsg{
		X: m.dividerX() + 5, Y: y0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	if m.rail.cursor != before || cmd != nil {
		t.Fatal("a click in the content column should not move the cursor")
	}
}

// A press directly on the divider arms the drag on the spot: it must not
// need `|` pressed first. It arms dragging alone, leaving resizeMode — the
// keyboard's own gate — off, and it moves nothing until the pointer does.
func TestDividerPressArmsDragWithoutResizeMode(t *testing.T) {
	m := &Model{
		mode:   modeList,
		width:  100,
		height: 40,
		split:  splitState{ratio: defaultSplitRatio}, services: services{listKeys: keybind.DefaultList()},
	}
	if m.split.resizeMode {
		t.Fatal("test setup: resize mode should start off")
	}
	div := m.dividerX()
	y0, _ := m.bodyYRange()
	updated, _ := m.handleMouse(tea.MouseMsg{
		X: div, Y: y0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	if !m.split.dragging {
		t.Fatal("a press on the divider should arm the drag on its own")
	}
	if m.split.resizeMode {
		t.Fatal("a mouse drag must not take the keyboard with it")
	}
	if left, _ := m.splitWidths(); left != div {
		t.Fatalf("the press alone should move nothing, left = %d want %d", left, div)
	}
	updated, _ = m.handleMouse(tea.MouseMsg{
		X: 40, Y: y0, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	if left, _ := m.splitWidths(); left != 40 {
		t.Fatalf("motion should set left=40, got %d", left)
	}
	updated, _ = m.handleMouse(tea.MouseMsg{
		X: 40, Y: y0, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	if m.split.dragging || m.split.resizeMode {
		t.Fatal("release should end the drag it started without the keyboard")
	}
}

// A press and release on the seam with nothing in between is a click, not a
// resize: it must not persist a ratio nobody dragged to, nor reflow every
// live pane for a frame that never changed.
func TestDividerClickWithoutMotionCommitsNothing(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	m := &Model{

		mode:   modeList,
		width:  100,
		height: 40,
		split:  splitState{ratio: defaultSplitRatio}, services: services{listKeys: keybind.DefaultList(),
			store: st},
	}
	div := m.dividerX()
	y0, _ := m.bodyYRange()
	for _, action := range []tea.MouseAction{tea.MouseActionPress, tea.MouseActionRelease} {
		updated, _ := m.handleMouse(tea.MouseMsg{
			X: div, Y: y0, Action: action, Button: tea.MouseButtonLeft,
		})
		m = updated.(*Model)
	}
	if m.split.dragging || m.split.resizeMode {
		t.Fatal("the click should have ended the drag it armed")
	}
	if m.split.ratio != defaultSplitRatio {
		t.Fatalf("ratio = %v want it untouched at %v", m.split.ratio, defaultSplitRatio)
	}
	if raw, err := st.Setting(splitRatioSetting); err != nil || raw != "" {
		t.Fatalf("a click with no drag persisted %q (err %v)", raw, err)
	}
}

// A press whose release never lands — dragging out of the window — must not
// strand the list: the keyboard stays live, and the next key ends the drag.
func TestKeyEndsADragWhoseReleaseNeverLands(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")

	y0, _ := m.bodyYRange()
	updated, _ := m.handleMouse(tea.MouseMsg{
		X: m.dividerX(), Y: y0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	if !m.split.dragging {
		t.Fatal("test setup: the press should have armed the drag")
	}
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(*Model)
	if m.split.dragging || m.split.resizeMode {
		t.Fatal("a key should end a drag left open by a lost release")
	}
	if sess, ok := m.selected(); !ok || sess.Name != "beta" {
		t.Fatalf("the key should have been handled normally, selection = %q ok=%v", sess.Name, ok)
	}
}

// A row click after a lost release must not be read as the end of the drag
// the release belongs to. The divider would follow the click column and
// persist there, so the stale drag ends before the press is resolved.
func TestRowClickAfterALostReleaseLeavesTheDividerAlone(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	m.View()

	y0, _ := m.bodyYRange()
	updated, _ := m.handleMouse(tea.MouseMsg{
		X: m.dividerX(), Y: y0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	updated, _ = m.handleMouse(tea.MouseMsg{
		X: m.dividerX() + 8, Y: y0, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	if !m.split.dragging || !m.split.moved {
		t.Fatal("test setup: the press and motion should have armed a live drag")
	}
	dragged := m.split.ratio

	// The release never arrives. The next thing the mouse does is an
	// ordinary click on a row, well left of the divider.
	line := paintedRailLines(t, m, "beta")[0]
	updated, _ = m.handleMouse(tea.MouseMsg{
		X: 2, Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	updated, _ = m.handleMouse(tea.MouseMsg{
		X: 2, Y: y0 + line, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)

	if m.split.dragging || m.split.moved {
		t.Fatal("the press should have ended the drag the lost release left open")
	}
	if m.split.ratio != dragged {
		t.Fatalf("ratio = %v, the click moved the divider off %v", m.split.ratio, dragged)
	}
	if sess, ok := m.selected(); !ok || sess.Name != "beta" {
		t.Fatalf("the click should have selected the row under it, got %q ok=%v", sess.Name, ok)
	}
}

// A click that misses the divider while resize mode is armed from the
// keyboard must not fall through to row selection: resize mode owns every
// press until it exits.
func TestPressOffDividerWhileArmedDoesNotSelectRow(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "beta")
	before := m.rail.cursor

	m.View()
	updated, _ := m.enterResizeMode()
	m = updated.(*Model)
	y0, _ := m.bodyYRange()
	updated, _ = m.handleMouse(tea.MouseMsg{
		X: 2, Y: y0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	if m.rail.cursor != before {
		t.Fatal("a miss while resize mode is armed should not select a row")
	}
	if !m.split.resizeMode {
		t.Fatal("resize mode should stay armed, waiting for the divider")
	}
}

// The full screen layout has no seam or content column: the whole width
// is rail, and the quick bar can dock below it. Both still have to line up
// with railHits the way the split layout does.
func TestClickSelectsRowInFullLayout(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "beta")
	m.prefs.fullLayout = true

	line := paintedRailLines(t, m, "alpha")[0]
	y0, _ := m.bodyYRange()
	updated, cmd := m.handleMouse(tea.MouseMsg{
		X: m.width - 2, Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	sess, ok := m.selected()
	if !ok || sess.Name != "alpha" {
		t.Fatalf("click should select alpha, got %q ok=%v", sess.Name, ok)
	}
	if cmd == nil {
		t.Fatal("selecting a different row should schedule a preview")
	}
}

// splitWidths still returns a ratio-based column in full layout even though
// no divider is painted there; a click at that phantom column must select
// the rail row under it, not arm a drag over a seam that does not exist.
func TestClickAtDividerXInFullLayoutSelectsRow(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "beta")
	m.prefs.fullLayout = true

	line := paintedRailLines(t, m, "alpha")[0]
	y0, _ := m.bodyYRange()
	updated, _ := m.handleMouse(tea.MouseMsg{
		X: m.dividerX(), Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	if m.split.resizeMode || m.split.dragging {
		t.Fatal("full layout has no divider to drag")
	}
	if sess, ok := m.selected(); !ok || sess.Name != "alpha" {
		t.Fatalf("click at dividerX should select the row under it, got %q ok=%v", sess.Name, ok)
	}
}

// A comfortable entry paints two or three lines; a click on any of them
// should select the entry, not whatever railHits index that physical line
// would be under a compact row.
func TestClickSelectsRowAcrossComfortableLines(t *testing.T) {
	m := buildModel(t)
	m.prefs.comfortableRows = true
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")

	alphaLines := paintedRailLines(t, m, "alpha")
	if len(alphaLines) < 2 {
		t.Fatalf("test setup: comfortable alpha should paint 2+ lines, got %d", len(alphaLines))
	}
	m.selectSessionRow(t, "beta")
	alphaLines = paintedRailLines(t, m, "alpha")

	y0, _ := m.bodyYRange()
	last := alphaLines[len(alphaLines)-1]
	updated, _ := m.handleMouse(tea.MouseMsg{
		X: 2, Y: y0 + last, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	if sess, ok := m.selected(); !ok || sess.Name != "alpha" {
		t.Fatalf("clicking alpha's later line should still select alpha, got %q ok=%v", sess.Name, ok)
	}
}

// A "N more" counter answers for the entry it is hiding: clicking it steps
// the window onto that entry rather than handing back the same frame. The
// rail's other chrome — the search field, the filter badges, the padding
// and the meters — still picks nothing.
func TestClickOnMoreCounterSelectsTheRowItHides(t *testing.T) {
	m := buildModel(t)
	for _, name := range []string{"one", "two", "three", "four", "five", "six", "seven"} {
		createSession(t, m, name, t.TempDir(), "")
	}
	m.selectSessionRow(t, "one")
	// A rail too short for seven sessions, so the window has to keep a
	// counter for the ones it is leaving out.
	m.height = 12
	frame := splitLines(m.View())

	y0, _ := m.bodyYRange()
	counter, target := -1, -1
	for i, row := range m.rail.railHits {
		if row >= 0 && strings.Contains(frame[y0+i], "more") {
			counter, target = i, row
		}
	}
	if counter < 0 {
		t.Fatal("test setup: a short rail should paint a counter")
	}
	for i, row := range m.rail.railHits {
		if i != counter && row == target {
			t.Fatalf("test setup: row %d is painted at line %d, so it is not hidden", target, i)
		}
	}

	updated, cmd := m.handleMouse(tea.MouseMsg{
		X: 2, Y: y0 + counter, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	if m.rail.cursor != target {
		t.Fatalf("clicking the counter should select row %d, cursor = %d", target, m.rail.cursor)
	}
	if cmd == nil {
		t.Fatal("selecting a different row should schedule a preview")
	}
}

// A counter only scrolls: its row is off screen, so a click there neither
// focuses that row on release nor finds a handle or a menu button on it.
func TestMoreCounterOnlyScrolls(t *testing.T) {
	m := buildModel(t)
	for _, name := range []string{"one", "two", "three", "four", "five", "six", "seven"} {
		createSession(t, m, name, t.TempDir(), "")
	}
	m.height = 12
	m.View()
	if m.rail.railWidth == 0 {
		t.Fatal("test setup: the frame should record the rail width")
	}
	for _, x := range []int{2, 4, m.rail.railWidth} {
		m.selectSessionRow(t, "one")
		frame := splitLines(m.View())
		y0, _ := m.bodyYRange()
		counter := -1
		for i, row := range m.rail.railHits {
			if row >= 0 && strings.Contains(frame[y0+i], "more") {
				counter = i
			}
		}
		if counter < 0 {
			t.Fatal("test setup: a short rail should paint a counter")
		}
		at := tea.MouseMsg{X: x, Y: y0 + counter, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
		updated, _ := m.handleMouse(at)
		m = updated.(*Model)
		at.Action = tea.MouseActionRelease
		updated, _ = m.handleMouse(at)
		m = updated.(*Model)
		if m.mode != modeList || m.rail.menu.active || m.rail.reorder.active {
			t.Fatalf("a click on the counter at x=%d should only scroll, mode = %v menu = %v lifted = %v", x, m.mode, m.rail.menu.active, m.rail.reorder.active)
		}
	}
}

// Chrome the rail paints for its own sake carries no row, so a click there
// picks nothing rather than misattributing to whichever row happens to sit
// at that index.
func TestClickOnRailChromeDoesNotSelect(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "beta")
	m.rail.searching = true
	before := m.rail.cursor
	m.View()

	line := -1
	for i, row := range m.rail.railHits {
		if row < 0 {
			line = i
			break
		}
	}
	if line < 0 {
		t.Fatal("test setup: the search field should paint chrome lines")
	}
	y0, _ := m.bodyYRange()
	updated, cmd := m.handleMouse(tea.MouseMsg{
		X: 2, Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	if m.rail.cursor != before || cmd != nil {
		t.Fatal("clicking a chrome line should not move the cursor")
	}
}

// Wheel events must be consumed by the app so the host terminal cannot
// scroll the TUI away, and in the list moves the session cursor the same
// way an arrow key would (#110).
func TestWheelMovesListCursor(t *testing.T) {
	m := &Model{
		mode: modeList,

		width:  80,
		height: 24, services: services{listKeys: keybind.DefaultList()}, rail: railState{cursor: 0,
			rows: []treeRow{{}, {}}},
	}
	updated, cmd := m.handleMouse(tea.MouseMsg{
		Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress,
	})
	m = updated.(*Model)
	if m.rail.cursor != 1 {
		t.Fatalf("wheel down: cursor = %d want 1", m.rail.cursor)
	}
	if cmd == nil {
		t.Fatal("wheel down should schedule the preview settle like moveCursor")
	}
	updated, _ = m.handleMouse(tea.MouseMsg{
		Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress,
	})
	m = updated.(*Model)
	if m.rail.cursor != 0 {
		t.Fatalf("wheel up: cursor = %d want 0", m.rail.cursor)
	}
}

// The wheel keeps moving the selection while the search field or the quick
// prompt is open. Search otherwise narrows the list to rows none of the
// pointer or the keyboard can reach, and the quick bar retargets on up/down
// the way its own footer advertises.
func TestWheelMovesCursorWhileSearchingOrPrompting(t *testing.T) {
	for name, m := range map[string]*Model{
		"searching": {mode: modeList, width: 80, height: 24, rail: railState{searching: true, cursor: 0, rows: []treeRow{{}, {}}}},
		"quick bar": {mode: modeList, quick: quickState{active: true}, width: 80, height: 24, rail: railState{cursor: 0, rows: []treeRow{{}, {}}}},
	} {
		t.Run(name, func(t *testing.T) {
			updated, _ := m.handleMouse(tea.MouseMsg{
				Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress,
			})
			m := updated.(*Model)
			if m.rail.cursor != 1 {
				t.Fatalf("wheel should have moved the cursor to 1, got %d", m.rail.cursor)
			}
		})
	}
}

// Search keeps click-to-select for the same reason it keeps the wheel.
func TestClickSelectsRowWhileSearching(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "beta")
	m.rail.searching = true

	line := paintedRailLines(t, m, "alpha")[0]
	y0, _ := m.bodyYRange()
	at := tea.MouseMsg{X: 2, Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	updated, _ := m.handleMouse(at)
	m = updated.(*Model)
	at.Action = tea.MouseActionRelease
	updated, _ = m.handleMouse(at)
	m = updated.(*Model)
	if sess, ok := m.selected(); m.mode != modeList || !ok || sess.Name != "alpha" {
		t.Fatalf("click should select alpha and stay in the list, mode = %v got %q", m.mode, sess.Name)
	}
}

// Entering a session ends the list's click run: the press that comes back
// here would otherwise pair with the one that opened it and focus again.
func TestClickLeavingFocusDoesNotRefocus(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")

	line := paintedRailLines(t, m, "alpha")[0]
	y0, _ := m.bodyYRange()
	press := tea.MouseMsg{X: 2, Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}

	updated, _ := m.handleMouse(press)
	m = updated.(*Model)
	updated, _ = m.focusSelected()
	m = updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("test setup: focus alpha, mode = %v, err = %q", m.mode, m.errBar.text)
	}
	m.View()

	updated, _ = m.handleMouse(press)
	m = updated.(*Model)
	press.Action = tea.MouseActionRelease
	updated, _ = m.handleMouse(press)
	m = updated.(*Model)
	if m.mode != modeList {
		t.Fatalf("the click that leaves focus must not focus again, mode = %v", m.mode)
	}
}

// A press search swallows opens no run, so the first press after it closes
// is a plain select rather than the second half of a full layout pair.
func TestClickWhileSearchingOpensNoClickRun(t *testing.T) {
	m := buildModel(t)
	m.prefs.fullLayout = true
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	m.rail.searching = true

	line := paintedRailLines(t, m, "alpha")[0]
	y0, _ := m.bodyYRange()
	press := tea.MouseMsg{X: 2, Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	updated, _ := m.handleMouse(press)
	m = updated.(*Model)

	m.rail.searching = false
	updated, _ = m.handleMouse(press)
	m = updated.(*Model)
	if m.mode != modeList {
		t.Fatalf("one press after search closes must not focus, mode = %v", m.mode)
	}
}

// Search owns Enter, so neither layout's focus gesture fires while it is
// open: it must not steal the key the field is waiting for.
func TestClicksDoNotFocusWhileSearching(t *testing.T) {
	for _, full := range []bool{false, true} {
		t.Run(fmt.Sprintf("full=%v", full), func(t *testing.T) {
			m := buildModel(t)
			m.prefs.fullLayout = full
			createSession(t, m, "alpha", t.TempDir(), "")
			m.selectSessionRow(t, "alpha")
			m.rail.searching = true

			line := paintedRailLines(t, m, "alpha")[0]
			y0, _ := m.bodyYRange()
			at := tea.MouseMsg{X: 2, Y: y0 + line, Button: tea.MouseButtonLeft}
			for i := 0; i < 2; i++ {
				for _, action := range []tea.MouseAction{tea.MouseActionPress, tea.MouseActionRelease} {
					at.Action = action
					updated, _ := m.handleMouse(at)
					m = updated.(*Model)
				}
			}
			if m.mode != modeList {
				t.Fatalf("clicks must not focus while searching, mode = %v", m.mode)
			}
		})
	}
}

// Neither does a press on the divider arm a drag while search is open: the
// mouse arms resize under the same conditions enterResizeMode does. The
// quick bar leaves the rail to the mouse, so the divider drags there.
func TestDividerPressWhileSearchingOrPrompting(t *testing.T) {
	for name, tc := range map[string]struct {
		m    *Model
		drag bool
	}{
		"searching": {&Model{mode: modeList, width: 100, height: 40, split: splitState{ratio: defaultSplitRatio}, services: services{listKeys: keybind.DefaultList()}, rail: railState{searching: true}}, false},
		"quick bar": {&Model{mode: modeList, quick: quickState{active: true}, width: 100, height: 40, split: splitState{ratio: defaultSplitRatio}, services: services{listKeys: keybind.DefaultList()}}, true},
	} {
		t.Run(name, func(t *testing.T) {
			y0, _ := tc.m.bodyYRange()
			updated, _ := tc.m.handleMouse(tea.MouseMsg{
				X: tc.m.dividerX(), Y: y0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
			})
			if got := updated.(*Model).split.dragging; got != tc.drag {
				t.Fatalf("divider dragging = %v, want %v", got, tc.drag)
			}
		})
	}
}

// One flick of the wheel is several notches, so the wheel clamps where the
// keyboard wraps: going off the end must not fling the selection to the
// far one and aim every key after it somewhere the user never looked.
func TestWheelClampsAtBothEnds(t *testing.T) {
	m := &Model{mode: modeList, width: 80, height: 24, services: services{listKeys: keybind.DefaultList()}, rail: railState{cursor: 2, rows: []treeRow{{}, {}, {}}}}
	for i := 0; i < 3; i++ {
		updated, _ := m.handleMouse(tea.MouseMsg{
			Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress,
		})
		m = updated.(*Model)
	}
	if m.rail.cursor != 2 {
		t.Fatalf("wheel down off the bottom: cursor = %d want it held at 2", m.rail.cursor)
	}
	m.rail.cursor = 0
	for i := 0; i < 3; i++ {
		updated, _ := m.handleMouse(tea.MouseMsg{
			Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress,
		})
		m = updated.(*Model)
	}
	if m.rail.cursor != 0 {
		t.Fatalf("wheel up off the top: cursor = %d want it held at 0", m.rail.cursor)
	}
	// The keyboard still wraps: clamping is the wheel's alone.
	m.moveCursor(-1)
	if m.rail.cursor != 2 {
		t.Fatalf("arrow up should still wrap to 2, cursor = %d", m.rail.cursor)
	}
}

func TestWheelSwallowedInResizeMode(t *testing.T) {
	m := &Model{
		mode:  modeList,
		split: splitState{resizeMode: true},

		width:  80,
		height: 24, services: services{listKeys: keybind.DefaultList()}, rail: railState{cursor: 0,
			rows: []treeRow{{}, {}}},
	}
	updated, _ := m.handleMouse(tea.MouseMsg{
		Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress,
	})
	m = updated.(*Model)
	if m.rail.cursor != 0 {
		t.Fatalf("resize mode should swallow wheel, cursor = %d", m.rail.cursor)
	}
}

// messagesCell finds the column and screen row the word "messages" was
// painted on in the last frame: the card's legend in the split, the badge
// in full screen.
func messagesCell(t *testing.T, m *Model) (x, y int) {
	t.Helper()
	for row, line := range strings.Split(ansi.Strip(m.View()), "\n") {
		if col := strings.Index(line, "messages"); col >= 0 {
			return col, row
		}
	}
	t.Fatal("test setup: the frame painted no messages card or badge")
	return 0, 0
}

func leftPress(m *Model, x, y int) *Model {
	updated, _ := m.handleMouse(tea.MouseMsg{
		X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	return updated.(*Model)
}

func TestClickOnMessagesCardOpensNotices(t *testing.T) {
	m := buildModel(t)
	m.width, m.height = 120, 34
	x, y := messagesCell(t, m)
	if m = leftPress(m, x, y); m.mode != modeNotices {
		t.Fatalf("click on the card should open messages, mode = %v", m.mode)
	}
}

func TestClickOnMetersBesideTheCardDoesNothing(t *testing.T) {
	m := buildModel(t)
	m.width, m.height = 120, 34
	_, y := messagesCell(t, m)
	if m = leftPress(m, 2, y); m.mode != modeList {
		t.Fatalf("click on the meters should stay on the list, mode = %v", m.mode)
	}
}

func TestClickOnFullScreenBadgeOpensNotices(t *testing.T) {
	m := buildModel(t)
	m.width, m.height = 120, 34
	m.prefs.fullLayout = true
	x, y := messagesCell(t, m)
	if m = leftPress(m, x, y); m.mode != modeNotices {
		t.Fatalf("click on the badge should open messages, mode = %v", m.mode)
	}
	m.mode = modeList
	if m = leftPress(m, 2, y); m.mode != modeList {
		t.Fatalf("click on the readings should stay on the list, mode = %v", m.mode)
	}
}

func TestClickOnMessagesCardWhileSearchingKeepsTheField(t *testing.T) {
	m := buildModel(t)
	m.width, m.height = 120, 34
	m.rail.searching = true
	x, y := messagesCell(t, m)
	if m = leftPress(m, x, y); m.mode != modeList || !m.rail.searching {
		t.Fatalf("search owns the click, mode = %v searching = %v", m.mode, m.rail.searching)
	}
}

func TestClickOnMessagesCardWhileFocusedIsLeftToFocus(t *testing.T) {
	m := buildModel(t)
	m.width, m.height = 200, 34
	createSession(t, m, "alpha", t.TempDir(), "")
	x, y := messagesCell(t, m)
	m.selectSessionRow(t, "alpha")
	updated, _ := m.focusSelected()
	m = updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("test setup: focus alpha, mode = %v, err = %q", m.mode, m.errBar.text)
	}
	m.View()
	if m = leftPress(m, x, y); m.mode == modeNotices {
		t.Fatal("a click while focused must not open messages")
	}
}

func TestSingleClickFocusesOnRelease(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")

	line := paintedRailLines(t, m, "alpha")[0]
	y0, _ := m.bodyYRange()
	at := tea.MouseMsg{X: 2, Y: y0 + line, Button: tea.MouseButtonLeft}
	at.Action = tea.MouseActionPress
	updated, _ := m.handleMouse(at)
	m = updated.(*Model)
	if m.mode != modeList {
		t.Fatalf("the press alone should only select, mode = %v", m.mode)
	}
	at.Action = tea.MouseActionRelease
	updated, _ = m.handleMouse(at)
	m = updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("release should focus, mode = %v, err = %q", m.mode, m.errBar.text)
	}
	if sess, ok := m.selected(); !ok || sess.Name != "alpha" {
		t.Fatalf("should focus alpha, got %q ok=%v", sess.Name, ok)
	}
}

func TestSingleClickFocusesWhenEnterAttaches(t *testing.T) {
	m := buildModel(t)
	m.prefs.focusOnEnter = false
	createSession(t, m, "alpha", t.TempDir(), "")

	line := paintedRailLines(t, m, "alpha")[0]
	y0, _ := m.bodyYRange()
	at := tea.MouseMsg{X: 2, Y: y0 + line, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	updated, _ := m.handleMouse(at)
	m = updated.(*Model)
	at.Action = tea.MouseActionRelease
	updated, _ = m.handleMouse(at)
	m = updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("a click should focus even when Enter attaches, mode = %v, err = %q", m.mode, m.errBar.text)
	}
}

func TestSingleClickReleasedOnAnotherRowDoesNotFocus(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")

	y0, _ := m.bodyYRange()
	alpha := paintedRailLines(t, m, "alpha")[0]
	beta := paintedRailLines(t, m, "beta")[0]
	updated, _ := m.handleMouse(tea.MouseMsg{X: 2, Y: y0 + alpha, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(*Model)
	updated, _ = m.handleMouse(tea.MouseMsg{X: 2, Y: y0 + beta, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
	m = updated.(*Model)
	updated, _ = m.handleMouse(tea.MouseMsg{X: 2, Y: y0 + alpha, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	m = updated.(*Model)
	if m.mode != modeList {
		t.Fatalf("a press dragged off its row should not focus, mode = %v", m.mode)
	}
}

func TestSingleClickInFullLayoutOnlySelects(t *testing.T) {
	m := buildModel(t)
	m.prefs.fullLayout = true
	createSession(t, m, "alpha", t.TempDir(), "")

	line := paintedRailLines(t, m, "alpha")[0]
	y0, _ := m.bodyYRange()
	at := tea.MouseMsg{X: 2, Y: y0 + line, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	updated, _ := m.handleMouse(at)
	m = updated.(*Model)
	at.Action = tea.MouseActionRelease
	updated, _ = m.handleMouse(at)
	m = updated.(*Model)
	if m.mode != modeList {
		t.Fatalf("full layout keeps click as select, mode = %v", m.mode)
	}
}

func TestClicksWorkWhileTheQuickBarIsOpen(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.openQuickMode()
	if !m.quick.active {
		t.Fatal("test setup: quick bar should open")
	}
	y0, _ := m.bodyYRange()
	beta := y0 + paintedRailLines(t, m, "beta")[0]

	updated, _ := m.handleMouse(tea.MouseMsg{X: m.rail.railWidth, Y: beta, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(*Model)
	if !m.rail.menu.active || !m.quick.active {
		t.Fatalf("%s should open the menu over the open quick bar, menu = %v quick = %v", rowMenuGlyph, m.rail.menu.active, m.quick.active)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.rail.menu.active || !m.quick.active {
		t.Fatalf("esc should close the menu first and leave the bar, menu = %v quick = %v", m.rail.menu.active, m.quick.active)
	}

	m = liftByHandle(t, m, "beta")
	if m.viewFooter() == "" || !strings.Contains(ansi.Strip(m.viewFooter()), "Reorder") {
		t.Fatal("a lifted row should own the footer over the quick bar")
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

	at := tea.MouseMsg{X: 8, Y: y0 + paintedRailLines(t, m, "alpha")[0], Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	updated, _ = m.handleMouse(at)
	m = updated.(*Model)
	at.Action = tea.MouseActionRelease
	updated, _ = m.handleMouse(at)
	m = updated.(*Model)
	if m.mode != modeFocus || m.quick.active {
		t.Fatalf("a row click should focus and close the bar, mode = %v quick = %v", m.mode, m.quick.active)
	}
}
