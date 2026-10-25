package ui

import (
	"fmt"
	uilifecycle "github.com/YoanWai/agent-manager/internal/ui/session/lifecycle"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	"github.com/charmbracelet/x/ansi"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/remote"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/testfixture"
	"github.com/YoanWai/agent-manager/internal/tmux"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uifocus "github.com/YoanWai/agent-manager/internal/ui/focus"
	uigeometry "github.com/YoanWai/agent-manager/internal/ui/geometry"
	uihelp "github.com/YoanWai/agent-manager/internal/ui/help"
	uiinstall "github.com/YoanWai/agent-manager/internal/ui/launchhint/install"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	uisurface "github.com/YoanWai/agent-manager/internal/ui/surface"
	uiterminal "github.com/YoanWai/agent-manager/internal/ui/terminal"
	tea "github.com/charmbracelet/bubbletea"
)

// A bare Model must default to mouse reporting on: mouseDisabled is named
// for its off polarity precisely so a zero-valued struct, the shape most
// tests build, does not silently disable the mouse everywhere.
func TestSyncMouseCaptureDefaultsOn(t *testing.T) {
	m := &Model{}
	// Nothing has changed from the zero value's implied "captured" state,
	// so there is nothing to tell the terminal.
	if cmd := m.syncMouseCapture(); cmd != nil {
		t.Fatal("default state should not re-announce mouse capture")
	}
	if m.mouse.released {
		t.Fatal("mouse should stay captured by default")
	}
}

// The mouse-mode setting releases the mouse everywhere except focus mode,
// whose own forwarding predates the setting and stays on regardless.
func TestSyncMouseCaptureRespectsMouseDisabled(t *testing.T) {
	m := &Model{prefs: preferences{mouseDisabled: true}}
	if cmd := m.syncMouseCapture(); cmd == nil {
		t.Fatal("turning the setting off should release the mouse")
	}
	if !m.mouse.released {
		t.Fatal("mouseReleased should follow the setting in list mode")
	}
	m.push(newFocusSurface(m))
	if cmd := m.syncMouseCapture(); cmd == nil {
		t.Fatal("entering focus mode should re-capture regardless of the setting")
	}
	if m.mouse.released {
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
		layout: layoutState{
			width:  100,
			height: 40,
			split:  uigeometry.Gesture{Ratio: uigeometry.DefaultSplitRatio},
		},
		services: services{listKeys: keybind.DefaultList(),
			store: st},
	}
	updated, _ := m.enterResizeMode()
	m = updated.(*Model)
	m.drainEffects(t)

	div := m.dividerX()
	// Body starts at the header's height; any y inside the body range works.
	updated, _ = m.handleMouse(tea.MouseMsg{
		X: div, Y: 5, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	m.drainEffects(t)
	if !m.layout.split.Dragging {
		t.Fatal("press on divider should start drag")
	}

	updated, _ = m.handleMouse(tea.MouseMsg{
		X: 50, Y: 5, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	m.drainEffects(t)
	if left, _ := m.splitWidths(); left != 50 {
		t.Fatalf("motion should set left=50, got %d", left)
	}

	updated, cmd := m.handleMouse(tea.MouseMsg{
		X: 50, Y: 5, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	m.drainEffects(t)
	if m.layout.split.Dragging || m.layout.split.ResizeMode {
		t.Fatal("release should end drag and exit resize mode")
	}
	if cmd != nil {
		t.Fatal("release should not toggle mouse reporting")
	}

	raw, err := st.Setting(uigeometry.SplitRatioSetting)
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
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "split-drag", t.TempDir(), "")
	id := m.sessionRows()[0].ID
	m.layout.split.Ratio = uigeometry.DefaultSplitRatio
	m.applyCmd(t, m.refreshCmd())
	m.drainEffects(t)
	before := windowWidth(t, id)
	if before != m.previewPaneWidth() {
		t.Fatalf("setup width = %d want %d", before, m.previewPaneWidth())
	}

	// Drift the session away so a real resize is observable.
	if _, err := tmuxCmd(t, "resize-window", "-t", "am_"+id, "-x", "100", "-y", "30").CombinedOutput(); err != nil {
		t.Fatalf("resize-window: %v", err)
	}
	if w := windowWidth(t, id); w != 100 {
		t.Fatalf("drifted width = %d want 100", w)
	}

	updated, _ := m.enterResizeMode()
	m = updated.(*Model)
	m.drainEffects(t)
	div := m.dividerX()
	y0, _ := m.bodyYRange()
	updated, _ = m.handleMouse(tea.MouseMsg{
		X: div, Y: y0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	m.drainEffects(t)
	updated, _ = m.handleMouse(tea.MouseMsg{
		X: 50, Y: y0, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	m.drainEffects(t)
	if w := windowWidth(t, id); w != 100 {
		t.Fatalf("motion must not resize tmux, width = %d want 100", w)
	}

	updated, _ = m.handleMouse(tea.MouseMsg{
		X: 50, Y: y0, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	m.drainEffects(t)
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
		layout: layoutState{
			width:  100,
			height: 40,
			split:  uigeometry.Gesture{Ratio: 0.34, ResizeMode: true},
		},
		services: services{listKeys: keybind.DefaultList()},
	}
	div := m.dividerX()
	updated, _ := m.handleMouse(tea.MouseMsg{
		X: div, Y: 0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	m.drainEffects(t)
	if m.layout.split.Dragging {
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
	m.drainEffects(t)
	if m.layout.split.Dragging {
		t.Fatal("press on exclusive body end must not start drag")
	}
}

func TestDragCancelRestoresRatio(t *testing.T) {
	m := &Model{
		layout: layoutState{
			width:  100,
			height: 40,
			split:  uigeometry.Gesture{Ratio: 0.34},
		},
		services: services{listKeys: keybind.DefaultList()},
	}
	updated, _ := m.enterResizeMode()
	m = updated.(*Model)
	m.drainEffects(t)
	div := m.dividerX()
	updated, _ = m.handleMouse(tea.MouseMsg{
		X: div, Y: 5, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	m.drainEffects(t)
	updated, _ = m.handleMouse(tea.MouseMsg{
		X: 55, Y: 5, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	m.drainEffects(t)
	if left, _ := m.splitWidths(); left != 55 {
		t.Fatalf("pre-cancel left = %d want 55", left)
	}

	updated, _ = m.exitResizeMode(false)
	m = updated.(*Model)
	m.drainEffects(t)
	if m.layout.split.ResizeMode || m.layout.split.Dragging {
		t.Fatal("cancel should clear resize state")
	}
	if left, _ := m.splitWidths(); left != 34 {
		t.Fatalf("cancel should restore left=34, got %d", left)
	}
}

func TestPressOffDividerDoesNotDrag(t *testing.T) {
	m := &Model{
		layout: layoutState{
			width:  100,
			height: 40,
			split:  uigeometry.Gesture{Ratio: 0.34, ResizeMode: true},
		},
		services: services{listKeys: keybind.DefaultList()},
	}
	updated, _ := m.handleMouse(tea.MouseMsg{
		X: 5, Y: 5, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	m.drainEffects(t)
	if m.layout.split.Dragging {
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
	preparedView(m)
	var lines []int
	for i, row := range railHitRows(m) {
		if row >= 0 && !railRows(m)[row].isGroup && railRows(m)[row].sess.Name == name {
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
	preparedView(m)
	var lines []int
	for i, row := range railHitRows(m) {
		if row >= 0 && railRows(m)[row].isGroup && railRows(m)[row].group == path {
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
	t.Parallel()
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
	m.drainEffects(t)
	sess, ok := m.selected()
	if !ok || sess.Name != "alpha" {
		t.Fatalf("click should select alpha, got %q ok=%v", sess.Name, ok)
	}
	if cmd == nil {
		t.Fatal("selecting a different row should schedule a preview")
	}
	if !m.onList() {
		t.Fatalf("first click should select, not focus, screen = %v", m.screenID())
	}
}

func TestClickOnAnotherRowFromFocusFocusesIt(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	updated, _ := m.focusSelected()
	m = updated.(*Model)
	m.drainEffects(t)
	if !m.focused() {
		t.Fatalf("test setup: focus alpha, screen = %v, err = %q", m.screenID(), m.errBar.text)
	}

	line := paintedRailLines(t, m, "beta")[0]
	y0, _ := m.bodyYRange()
	at := tea.MouseMsg{X: 2, Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	updated, _ = m.handleMouse(at)
	m.drainEffects(t)
	m = updated.(*Model)
	m.drainEffects(t)
	if !m.onList() {
		t.Fatalf("the press should leave focus, screen = %v", m.screenID())
	}
	at.Action = tea.MouseActionRelease
	updated, _ = m.handleMouse(at)
	m.drainEffects(t)
	m = updated.(*Model)
	m.drainEffects(t)
	if sess, ok := m.selected(); !m.focused() || !ok || sess.Name != "beta" {
		t.Fatalf("the release should focus beta, screen = %v got %q", m.screenID(), sess.Name)
	}
}

func TestClickOnFocusedSessionRowLeavesFocus(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	updated, _ := m.focusSelected()
	m = updated.(*Model)
	m.drainEffects(t)
	if !m.focused() {
		t.Fatalf("test setup: focus alpha, screen = %v, err = %q", m.screenID(), m.errBar.text)
	}

	line := paintedRailLines(t, m, "alpha")[0]
	y0, _ := m.bodyYRange()
	at := tea.MouseMsg{X: 2, Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	updated, _ = m.handleMouse(at)
	m.drainEffects(t)
	m = updated.(*Model)
	m.drainEffects(t)
	at.Action = tea.MouseActionRelease
	updated, _ = m.handleMouse(at)
	m.drainEffects(t)
	m = updated.(*Model)
	m.drainEffects(t)
	if !m.onList() {
		t.Fatalf("click on the focused row should leave focus, screen = %v", m.screenID())
	}
	if sess, ok := m.selected(); !ok || sess.Name != "alpha" {
		t.Fatalf("selection should stay on alpha, got %q ok=%v", sess.Name, ok)
	}
}

func TestClickInFocusedPaneStaysFocused(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	waitForPreview(t, m)
	updated, _ := m.focusSelected()
	m = updated.(*Model)
	m.drainEffects(t)
	if !m.focused() {
		t.Fatalf("test setup: focus alpha, screen = %v, err = %q", m.screenID(), m.errBar.text)
	}
	// Empty cat panes have no rendered hit box; give this click test visible content.
	m.workspace.preview = "focused pane"
	preparedView(m)
	box := m.focus.pane.FrameBox()
	if !box.Valid {
		t.Fatal("test setup: focused pane has no box")
	}
	updated, _ = m.handleMouse(tea.MouseMsg{
		X: box.X, Y: box.Y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	m.drainEffects(t)
	if !m.focused() {
		t.Fatalf("click in the pane should stay focused, screen = %v", m.screenID())
	}
}

// Double click is the full layout's focus gesture. The pointer names the
// row, not the cursor: a wheel notch, a j or the poll can walk the cursor
// away between the two presses.
func TestDoubleClickFocusesTheRowUnderThePointer(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.prefs.fullLayout = true
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")

	line := paintedRailLines(t, m, "alpha")[0]
	y0, _ := m.bodyYRange()
	press := tea.MouseMsg{X: 2, Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	updated, _ := m.handleMouse(press)
	m.drainEffects(t)
	m = updated.(*Model)
	m.drainEffects(t)
	m.selectSessionRow(t, "beta")

	updated, _ = m.handleMouse(press)
	m.drainEffects(t)
	m = updated.(*Model)
	m.drainEffects(t)
	if !m.focused() {
		t.Fatalf("double click should focus, screen = %v, err = %q", m.screenID(), m.errBar.text)
	}
	if sess, ok := m.selected(); !ok || sess.Name != "alpha" {
		t.Fatalf("double click should focus the row it landed on, got %q ok=%v", sess.Name, ok)
	}
}

// The pair is matched on the row's identity: a rebuild between the presses
// renumbers m.rows, so an index that meant this row can mean another.
func TestDoubleClickPairsAcrossARebuild(t *testing.T) {
	t.Parallel()
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
	m.drainEffects(t)
	before := m.rail.Cursor()

	// Folding the group above target drops every row under it an index.
	m.rail.SetCollapsed("aaa", true)
	m.rebuildRows()
	line = paintedRailLines(t, m, "target")[0]
	m.selectSessionRow(t, "target")
	if m.rail.Cursor() == before {
		t.Fatal("test setup: folding should have renumbered target's row")
	}
	setRailCursor(m, before)

	updated, _ = m.handleMouse(tea.MouseMsg{
		X: 2, Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	m.drainEffects(t)
	if !m.focused() {
		t.Fatalf("the pair should survive a rebuild, screen = %v, err = %q", m.screenID(), m.errBar.text)
	}
	if sess, ok := m.selected(); !ok || sess.Name != "target" {
		t.Fatalf("should focus target, got %q ok=%v", sess.Name, ok)
	}
}

// A session that has painted nothing yet leaves no pane box, and its
// column is still its own: clicking it must not eject the user.
func TestClickInTheFocusedColumnStaysFocused(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	updated, _ := m.focusSelected()
	m = updated.(*Model)
	m.drainEffects(t)
	if !m.focused() {
		t.Fatalf("test setup: focus alpha, screen = %v, err = %q", m.screenID(), m.errBar.text)
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
			preparedView(m)
			y0, _ := m.bodyYRange()
			updated, _ := m.handleMouse(tea.MouseMsg{
				X: m.focusPaneOriginX(), Y: y0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
			})
			m = updated.(*Model)
			m.drainEffects(t)
			if !m.focused() {
				t.Fatalf("a click in the session's own column must not leave focus, screen = %v", m.screenID())
			}
		})
	}
}

// Full screen focus paints no rail, so the list frame's hits must not
// outlive it and hand a click a row nobody pointed at.
func TestClickInFullScreenFocusStaysFocused(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "beta")
	preparedView(m)

	m.prefs.fullLayout = true
	m.selectSessionRow(t, "alpha")
	updated, _ := m.focusSelected()
	m = updated.(*Model)
	m.drainEffects(t)
	if !m.focused() {
		t.Fatalf("test setup: focus alpha, screen = %v, err = %q", m.screenID(), m.errBar.text)
	}
	preparedView(m)

	y0, _ := m.bodyYRange()
	updated, _ = m.handleMouse(tea.MouseMsg{
		X: 2, Y: y0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	m.drainEffects(t)
	if !m.focused() {
		t.Fatalf("a click with no list on screen must not leave focus, screen = %v", m.screenID())
	}
	if sess, ok := m.selected(); !ok || sess.Name != "alpha" {
		t.Fatalf("selection should stay on alpha, got %q ok=%v", sess.Name, ok)
	}
}

func TestDoubleClickFocusesTheRowJustSelected(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.prefs.fullLayout = true
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "beta")

	line := paintedRailLines(t, m, "alpha")[0]
	y0, _ := m.bodyYRange()
	press := tea.MouseMsg{X: 2, Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	updated, _ := m.handleMouse(press)
	m.drainEffects(t)
	m = updated.(*Model)
	m.drainEffects(t)
	if !m.onList() {
		t.Fatalf("first click should select, screen = %v", m.screenID())
	}
	updated, _ = m.handleMouse(press)
	m.drainEffects(t)
	m = updated.(*Model)
	m.drainEffects(t)
	if !m.focused() {
		t.Fatalf("double click should focus alpha, screen = %v, err = %q", m.screenID(), m.errBar.text)
	}
}

func TestDoubleClickFocusesWhenEnterAttaches(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.prefs.fullLayout = true
	m.prefs.focusOnEnter = false
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")

	line := paintedRailLines(t, m, "alpha")[0]
	y0, _ := m.bodyYRange()
	press := tea.MouseMsg{X: 2, Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	updated, _ := m.handleMouse(press)
	m.drainEffects(t)
	m = updated.(*Model)
	m.drainEffects(t)
	updated, _ = m.handleMouse(press)
	m.drainEffects(t)
	m = updated.(*Model)
	m.drainEffects(t)
	if !m.focused() {
		t.Fatalf("double click should focus even when Enter attaches, screen = %v, err = %q", m.screenID(), m.errBar.text)
	}
}

func TestSlowSecondClickDoesNotFocus(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.prefs.fullLayout = true
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")

	line := paintedRailLines(t, m, "alpha")[0]
	y0, _ := m.bodyYRange()
	press := tea.MouseMsg{X: 2, Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	ctx := m.railMouseContext()
	ctx.Now = time.Now().Add(-time.Second)
	m.applyRailDecision(m.rail.Mouse(press, m.layout.displayedRail, ctx))
	m.drainEffects(t)
	updated, _ := m.handleMouse(press)
	m.drainEffects(t)
	m = updated.(*Model)
	m.drainEffects(t)
	if !m.onList() {
		t.Fatalf("a slow second click should not focus, screen = %v", m.screenID())
	}
}

func TestClickOnSelectedGroupTogglesCollapse(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("work", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "alpha", dir, "work")
	m.selectGroupRow(t, "work")
	if m.rail.IsCollapsed("work") {
		t.Fatal("test setup: work should start open")
	}

	line := paintedGroupLines(t, m, "work")[0]
	y0, _ := m.bodyYRange()
	press := tea.MouseMsg{X: 2, Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	updated, _ := m.handleMouse(press)
	m.drainEffects(t)
	m = updated.(*Model)
	m.drainEffects(t)
	updated, _ = m.handleMouse(press)
	m.drainEffects(t)
	m = updated.(*Model)
	m.drainEffects(t)
	if !m.rail.IsCollapsed("work") {
		t.Fatal("double click on the selected group should fold it")
	}

	line = paintedGroupLines(t, m, "work")[0]
	y0, _ = m.bodyYRange()
	press.Y = y0 + line
	updated, _ = m.handleMouse(press)
	m.drainEffects(t)
	m = updated.(*Model)
	m.drainEffects(t)
	updated, _ = m.handleMouse(press)
	m.drainEffects(t)
	m = updated.(*Model)
	m.drainEffects(t)
	if m.rail.IsCollapsed("work") {
		t.Fatal("a second double click should unfold it")
	}
}

// The rail's last painted column carries row text, so it belongs to the
// row under it. The divider is resolved before the row is, so a hit target
// that reached back over that column would cost every row its right edge.
func TestClickOnRailLastColumnSelectsRow(t *testing.T) {
	t.Parallel()
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
	m.drainEffects(t)
	if m.layout.split.Dragging || m.layout.split.ResizeMode {
		t.Fatal("the rail's own last column must not grab the divider")
	}
	if sess, ok := m.selected(); !ok || sess.Name != "alpha" {
		t.Fatalf("click on the rail's last column should select alpha, got %q ok=%v", sess.Name, ok)
	}
}

// A click past the divider, in the content column, must not steal the
// selection: the rail is what click-to-select owns.
func TestClickInContentColumnFocusesTheSelectedSession(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "beta")
	before := m.rail.Cursor()

	preparedView(m)
	y0, _ := m.bodyYRange()
	updated, _ := m.handleMouse(tea.MouseMsg{
		X: m.dividerX() + 5, Y: y0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	m.drainEffects(t)
	if m.rail.Cursor() != before {
		t.Fatal("a click in the content column should not move the cursor")
	}
	if sess, ok := m.selected(); !m.focused() || !ok || sess.Name != "beta" {
		t.Fatalf("a click in the content column should focus beta, screen = %v", m.screenID())
	}
}

// A press directly on the divider arms the drag on the spot: it must not
// need `|` pressed first. It arms dragging alone, leaving resizeMode — the
// keyboard's own gate — off, and it moves nothing until the pointer does.
func TestDividerPressArmsDragWithoutResizeMode(t *testing.T) {
	m := &Model{
		layout: layoutState{
			width:  100,
			height: 40,
			split:  uigeometry.Gesture{Ratio: uigeometry.DefaultSplitRatio},
		},
		services: services{listKeys: keybind.DefaultList()},
	}
	if m.layout.split.ResizeMode {
		t.Fatal("test setup: resize mode should start off")
	}
	div := m.dividerX()
	y0, _ := m.bodyYRange()
	updated, _ := m.handleMouse(tea.MouseMsg{
		X: div, Y: y0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	m.drainEffects(t)
	if !m.layout.split.Dragging {
		t.Fatal("a press on the divider should arm the drag on its own")
	}
	if m.layout.split.ResizeMode {
		t.Fatal("a mouse drag must not take the keyboard with it")
	}
	if left, _ := m.splitWidths(); left != div {
		t.Fatalf("the press alone should move nothing, left = %d want %d", left, div)
	}
	updated, _ = m.handleMouse(tea.MouseMsg{
		X: 40, Y: y0, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	m.drainEffects(t)
	if left, _ := m.splitWidths(); left != 40 {
		t.Fatalf("motion should set left=40, got %d", left)
	}
	updated, _ = m.handleMouse(tea.MouseMsg{
		X: 40, Y: y0, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	m.drainEffects(t)
	if m.layout.split.Dragging || m.layout.split.ResizeMode {
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
		layout: layoutState{
			width:  100,
			height: 40,
			split:  uigeometry.Gesture{Ratio: uigeometry.DefaultSplitRatio},
		},
		services: services{listKeys: keybind.DefaultList(),
			store: st},
	}
	div := m.dividerX()
	y0, _ := m.bodyYRange()
	for _, action := range []tea.MouseAction{tea.MouseActionPress, tea.MouseActionRelease} {
		updated, _ := m.handleMouse(tea.MouseMsg{
			X: div, Y: y0, Action: action, Button: tea.MouseButtonLeft,
		})
		m = updated.(*Model)
		m.drainEffects(t)
	}
	if m.layout.split.Dragging || m.layout.split.ResizeMode {
		t.Fatal("the click should have ended the drag it armed")
	}
	if m.layout.split.Ratio != uigeometry.DefaultSplitRatio {
		t.Fatalf("ratio = %v want it untouched at %v", m.layout.split.Ratio, uigeometry.DefaultSplitRatio)
	}
	if raw, err := st.Setting(uigeometry.SplitRatioSetting); err != nil || raw != "" {
		t.Fatalf("a click with no drag persisted %q (err %v)", raw, err)
	}
}

// A press whose release never lands — dragging out of the window — must not
// strand the list: the keyboard stays live, and the next key ends the drag.
func TestKeyEndsADragWhoseReleaseNeverLands(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")

	y0, _ := m.bodyYRange()
	updated, _ := m.handleMouse(tea.MouseMsg{
		X: m.dividerX(), Y: y0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	m.drainEffects(t)
	if !m.layout.split.Dragging {
		t.Fatal("test setup: the press should have armed the drag")
	}
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	m.drainEffects(t)
	m = updated.(*Model)
	m.drainEffects(t)
	if m.layout.split.Dragging || m.layout.split.ResizeMode {
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
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	preparedView(m)

	y0, _ := m.bodyYRange()
	updated, _ := m.handleMouse(tea.MouseMsg{
		X: m.dividerX(), Y: y0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	m.drainEffects(t)
	updated, _ = m.handleMouse(tea.MouseMsg{
		X: m.dividerX() + 8, Y: y0, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	m.drainEffects(t)
	if !m.layout.split.Dragging || !m.layout.split.Moved {
		t.Fatal("test setup: the press and motion should have armed a live drag")
	}
	dragged := m.layout.split.Ratio

	// The release never arrives. The next thing the mouse does is an
	// ordinary click on a row, well left of the divider.
	line := paintedRailLines(t, m, "beta")[0]
	updated, _ = m.handleMouse(tea.MouseMsg{
		X: 2, Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	m.drainEffects(t)
	updated, _ = m.handleMouse(tea.MouseMsg{
		X: 2, Y: y0 + line, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	m.drainEffects(t)

	if m.layout.split.Dragging || m.layout.split.Moved {
		t.Fatal("the press should have ended the drag the lost release left open")
	}
	if m.layout.split.Ratio != dragged {
		t.Fatalf("ratio = %v, the click moved the divider off %v", m.layout.split.Ratio, dragged)
	}
	if sess, ok := m.selected(); !ok || sess.Name != "beta" {
		t.Fatalf("the click should have selected the row under it, got %q ok=%v", sess.Name, ok)
	}
}

// A click that misses the divider while resize mode is armed from the
// keyboard must not fall through to row selection: resize mode owns every
// press until it exits.
func TestPressOffDividerWhileArmedDoesNotSelectRow(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "beta")
	before := m.rail.Cursor()

	preparedView(m)
	updated, _ := m.enterResizeMode()
	m = updated.(*Model)
	m.drainEffects(t)
	y0, _ := m.bodyYRange()
	updated, _ = m.handleMouse(tea.MouseMsg{
		X: 2, Y: y0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	m.drainEffects(t)
	if m.rail.Cursor() != before {
		t.Fatal("a miss while resize mode is armed should not select a row")
	}
	if !m.layout.split.ResizeMode {
		t.Fatal("resize mode should stay armed, waiting for the divider")
	}
}

// The full screen layout has no seam or content column: the whole width
// is rail, and the quick bar can dock below it. Both still have to line up
// with railHits the way the split layout does.
func TestClickSelectsRowInFullLayout(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "beta")
	m.prefs.fullLayout = true

	line := paintedRailLines(t, m, "alpha")[0]
	y0, _ := m.bodyYRange()
	updated, cmd := m.handleMouse(tea.MouseMsg{
		X: m.layout.width - 2, Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	m.drainEffects(t)
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
	t.Parallel()
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
	m.drainEffects(t)
	if m.layout.split.ResizeMode || m.layout.split.Dragging {
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
	t.Parallel()
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
	m.drainEffects(t)
	if sess, ok := m.selected(); !ok || sess.Name != "alpha" {
		t.Fatalf("clicking alpha's later line should still select alpha, got %q ok=%v", sess.Name, ok)
	}
}

// A "N more" counter answers for the entry it is hiding: clicking it steps
// the window onto that entry rather than handing back the same frame. The
// rail's other chrome — the search field, the filter badges, the padding
// and the meters — still picks nothing.
func TestClickOnMoreCounterSelectsTheRowItHides(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	for _, name := range []string{"one", "two", "three", "four", "five", "six", "seven"} {
		createSession(t, m, name, t.TempDir(), "")
	}
	m.selectSessionRow(t, "one")
	// A rail too short for seven sessions, so the window has to keep a
	// counter for the ones it is leaving out.
	m.layout.height = 12
	frame := uitheme.SplitLines(preparedView(m))

	y0, _ := m.bodyYRange()
	counter, target := -1, -1
	for i, row := range railHitRows(m) {
		if row >= 0 && strings.Contains(frame[y0+i], "more") {
			counter, target = i, row
		}
	}
	if counter < 0 {
		t.Fatal("test setup: a short rail should paint a counter")
	}
	for i, row := range railHitRows(m) {
		if i != counter && row == target {
			t.Fatalf("test setup: row %d is painted at line %d, so it is not hidden", target, i)
		}
	}

	updated, cmd := m.handleMouse(tea.MouseMsg{
		X: 2, Y: y0 + counter, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	m.drainEffects(t)
	if m.rail.Cursor() != target {
		t.Fatalf("clicking the counter should select row %d, cursor = %d", target, m.rail.Cursor())
	}
	if cmd == nil {
		t.Fatal("selecting a different row should schedule a preview")
	}
}

// A counter only scrolls: its row is off screen, so a click there neither
// focuses that row on release nor finds a handle or a menu button on it.
func TestMoreCounterOnlyScrolls(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	for _, name := range []string{"one", "two", "three", "four", "five", "six", "seven"} {
		createSession(t, m, name, t.TempDir(), "")
	}
	m.layout.height = 12
	preparedView(m)
	if m.layout.displayedRail.Width == 0 {
		t.Fatal("test setup: the frame should record the rail width")
	}
	for _, x := range []int{2, 4, m.layout.displayedRail.Width} {
		m.selectSessionRow(t, "one")
		frame := uitheme.SplitLines(preparedView(m))
		y0, _ := m.bodyYRange()
		counter := -1
		for i, row := range railHitRows(m) {
			if row >= 0 && strings.Contains(frame[y0+i], "more") {
				counter = i
			}
		}
		if counter < 0 {
			t.Fatal("test setup: a short rail should paint a counter")
		}
		at := tea.MouseMsg{X: x, Y: y0 + counter, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
		updated, _ := m.handleMouse(at)
		m.drainEffects(t)
		m = updated.(*Model)
		m.drainEffects(t)
		at.Action = tea.MouseActionRelease
		updated, _ = m.handleMouse(at)
		m.drainEffects(t)
		m = updated.(*Model)
		m.drainEffects(t)
		if !m.onList() || m.rail.MenuOpen() || m.rail.Reordering() {
			t.Fatalf("a click on the counter at x=%d should only scroll, screen = %v menu = %v lifted = %v", x, m.screenID(), m.rail.MenuOpen(), m.rail.Reordering())
		}
	}
}

// A trackpad flick that is still arriving when ctrl+q leaves focus must not
// walk the list selection, down to its slow last notch; once it has
// settled, a deliberate scroll moves the selection as before (#727).
func TestLeavingFocusSwallowsTheFlickStillArriving(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	for _, name := range []string{"one", "two", "three", "four"} {
		createSession(t, m, name, t.TempDir(), "")
	}
	m.selectSessionRow(t, "two")

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	m.drainEffects(t)
	if !m.focused() {
		t.Fatalf("enter did not focus, screen = %v", m.screenID())
	}
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlQ})
	m = updated.(*Model)
	m.drainEffects(t)
	if !m.onList() {
		t.Fatalf("ctrl+q did not leave focus, screen = %v", m.screenID())
	}
	before := m.rail.Cursor()

	wheel := tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress}
	// Leaving focus stamped the clock; the notches that follow run on the mouse
	// context's clock too, so a slow runner cannot stretch the gap past the
	// settle gap between the stamp and the first notch.
	if m.mouse.flickAt.IsZero() {
		t.Fatal("leaving focus should stamp the flick clock")
	}
	for i := 0; i < 2; i++ {
		updated, _ = m.listWheel(wheel, uirail.MouseContext{Now: m.mouse.flickAt.Add(10 * time.Millisecond)})
		m = updated.(*Model)
	}
	if m.rail.Cursor() != before {
		t.Fatalf("a notch right after leaving focus moved the cursor from %d to %d", before, m.rail.Cursor())
	}

	// The clock is the mouse context's, so the gaps are exact rather than
	// a sleep the scheduler can stretch past the settle gap.
	left := time.Unix(100, 0)
	m.mouse.flickAt = left
	wheelAt := func(now time.Time) {
		updated, _ := m.listWheel(wheel, uirail.MouseContext{Now: now})
		m = updated.(*Model)
	}
	for _, gap := range []time.Duration{30 * time.Millisecond, 40 * time.Millisecond, 220 * time.Millisecond} {
		wheelAt(m.mouse.flickAt.Add(gap))
		if m.rail.Cursor() != before {
			t.Fatalf("a notch %v after the previous one moved the cursor from %d to %d", gap, before, m.rail.Cursor())
		}
	}
	wheelAt(m.mouse.flickAt.Add(wheelSettleGap + time.Millisecond))
	if m.rail.Cursor() != before+1 {
		t.Fatalf("a scroll once the flick has settled should move the cursor, got %d want %d", m.rail.Cursor(), before+1)
	}
	if !m.mouse.flickAt.IsZero() {
		t.Fatal("a settled flick should stop stamping notches")
	}
	wheelAt(left.Add(time.Hour))
	if m.rail.Cursor() != before+2 {
		t.Fatalf("a later scroll should move the cursor, got %d want %d", m.rail.Cursor(), before+2)
	}
}

// Chrome the rail paints for its own sake carries no row, so a click there
// picks nothing rather than misattributing to whichever row happens to sit
// at that index.
func TestClickOnRailChromeDoesNotSelect(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "beta")
	m.rail.SetSearch(m.rail.Search(), true)
	before := m.rail.Cursor()
	preparedView(m)

	line := -1
	for i, row := range railHitRows(m) {
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
	m.drainEffects(t)
	if m.rail.Cursor() != before || cmd != nil {
		t.Fatal("clicking a chrome line should not move the cursor")
	}
}

// Wheel events must be consumed by the app so the host terminal cannot
// scroll the TUI away, and in the list moves the session cursor the same
// way an arrow key would (#110).
func TestWheelMovesListCursor(t *testing.T) {
	m := &Model{
		layout: layoutState{
			width:  80,
			height: 24,
		},
		services:  services{listKeys: keybind.DefaultList()},
		workspace: workspace{sessions: []store.Session{{ID: "fixture-b", Name: "fixture"}}},
		rail:      railModelCount(2, 0, false),
	}
	updated, cmd := m.handleMouse(tea.MouseMsg{
		Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress,
	})
	m = updated.(*Model)
	m.drainEffects(t)
	if m.rail.Cursor() != 1 {
		t.Fatalf("wheel down: cursor = %d want 1", m.rail.Cursor())
	}
	if cmd == nil {
		t.Fatal("wheel down should schedule the preview settle like moveCursor")
	}
	updated, _ = m.handleMouse(tea.MouseMsg{
		Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress,
	})
	m = updated.(*Model)
	m.drainEffects(t)
	if m.rail.Cursor() != 0 {
		t.Fatalf("wheel up: cursor = %d want 0", m.rail.Cursor())
	}
}

// The wheel keeps moving the selection while the search field or the quick
// prompt is open. Search otherwise narrows the list to rows none of the
// pointer or the keyboard can reach, and the quick bar retargets on up/down
// the way its own footer advertises.
func TestWheelMovesCursorWhileSearchingOrPrompting(t *testing.T) {
	for name, m := range map[string]*Model{
		"searching": {layout: layoutState{width: 80, height: 24}, rail: railModelCount(2, 0, true)},
		"quick bar": {layout: layoutState{width: 80, height: 24}, rail: railModelCount(2, 0, false)},
	} {
		t.Run(name, func(t *testing.T) {
			if name == "quick bar" {
				pushQuick(m)
			}
			updated, _ := m.handleMouse(tea.MouseMsg{
				Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress,
			})
			m := updated.(*Model)
			if m.rail.Cursor() != 1 {
				t.Fatalf("wheel should have moved the cursor to 1, got %d", m.rail.Cursor())
			}
		})
	}
}

// Search keeps click-to-select for the same reason it keeps the wheel.
func TestClickSelectsRowWhileSearching(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "beta")
	m.rail.SetSearch(m.rail.Search(), true)

	line := paintedRailLines(t, m, "alpha")[0]
	y0, _ := m.bodyYRange()
	at := tea.MouseMsg{X: 2, Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	updated, _ := m.handleMouse(at)
	m.drainEffects(t)
	m = updated.(*Model)
	m.drainEffects(t)
	at.Action = tea.MouseActionRelease
	updated, _ = m.handleMouse(at)
	m.drainEffects(t)
	m = updated.(*Model)
	m.drainEffects(t)
	if sess, ok := m.selected(); !m.onList() || !ok || sess.Name != "alpha" {
		t.Fatalf("click should select alpha and stay in the list, screen = %v got %q", m.screenID(), sess.Name)
	}
}

// Entering a session ends the list's click run: the press that comes back
// here would otherwise pair with the one that opened it and focus again.
func TestClickLeavingFocusDoesNotRefocus(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")

	line := paintedRailLines(t, m, "alpha")[0]
	y0, _ := m.bodyYRange()
	press := tea.MouseMsg{X: 2, Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}

	updated, _ := m.handleMouse(press)
	m.drainEffects(t)
	m = updated.(*Model)
	m.drainEffects(t)
	updated, _ = m.focusSelected()
	m = updated.(*Model)
	m.drainEffects(t)
	if !m.focused() {
		t.Fatalf("test setup: focus alpha, screen = %v, err = %q", m.screenID(), m.errBar.text)
	}
	preparedView(m)

	updated, _ = m.handleMouse(press)
	m.drainEffects(t)
	m = updated.(*Model)
	m.drainEffects(t)
	press.Action = tea.MouseActionRelease
	updated, _ = m.handleMouse(press)
	m.drainEffects(t)
	m = updated.(*Model)
	m.drainEffects(t)
	if !m.onList() {
		t.Fatalf("the click that leaves focus must not focus again, screen = %v", m.screenID())
	}
}

// A press search swallows opens no run, so the first press after it closes
// is a plain select rather than the second half of a full layout pair.
func TestClickWhileSearchingOpensNoClickRun(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.prefs.fullLayout = true
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	m.rail.SetSearch(m.rail.Search(), true)

	line := paintedRailLines(t, m, "alpha")[0]
	y0, _ := m.bodyYRange()
	press := tea.MouseMsg{X: 2, Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	updated, _ := m.handleMouse(press)
	m.drainEffects(t)
	m = updated.(*Model)
	m.drainEffects(t)

	m.rail.SetSearch(m.rail.Search(), false)
	updated, _ = m.handleMouse(press)
	m.drainEffects(t)
	m = updated.(*Model)
	m.drainEffects(t)
	if !m.onList() {
		t.Fatalf("one press after search closes must not focus, screen = %v", m.screenID())
	}
}

// Search owns Enter, so neither layout's focus gesture fires while it is
// open: it must not steal the key the field is waiting for.
func TestClicksDoNotFocusWhileSearching(t *testing.T) {
	t.Parallel()
	for _, full := range []bool{false, true} {
		t.Run(fmt.Sprintf("full=%v", full), func(t *testing.T) {
			m := buildModel(t)
			m.prefs.fullLayout = full
			createSession(t, m, "alpha", t.TempDir(), "")
			m.selectSessionRow(t, "alpha")
			m.rail.SetSearch(m.rail.Search(), true)

			line := paintedRailLines(t, m, "alpha")[0]
			y0, _ := m.bodyYRange()
			at := tea.MouseMsg{X: 2, Y: y0 + line, Button: tea.MouseButtonLeft}
			for i := 0; i < 2; i++ {
				for _, action := range []tea.MouseAction{tea.MouseActionPress, tea.MouseActionRelease} {
					at.Action = action
					updated, _ := m.handleMouse(at)
					m.drainEffects(t)
					m = updated.(*Model)
					m.drainEffects(t)
				}
			}
			if !m.onList() {
				t.Fatalf("clicks must not focus while searching, screen = %v", m.screenID())
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
		"searching": {&Model{layout: layoutState{width: 100, height: 40, split: uigeometry.Gesture{Ratio: uigeometry.DefaultSplitRatio}}, services: services{listKeys: keybind.DefaultList()}, rail: railModelCount(0, 0, true)}, false},
		"quick bar": {&Model{layout: layoutState{width: 100, height: 40, split: uigeometry.Gesture{Ratio: uigeometry.DefaultSplitRatio}}, services: services{listKeys: keybind.DefaultList()}}, true},
	} {
		t.Run(name, func(t *testing.T) {
			if name == "quick bar" {
				pushQuick(tc.m)
			}
			y0, _ := tc.m.bodyYRange()
			updated, _ := tc.m.handleMouse(tea.MouseMsg{
				X: tc.m.dividerX(), Y: y0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
			})
			if got := updated.(*Model).layout.split.Dragging; got != tc.drag {
				t.Fatalf("divider dragging = %v, want %v", got, tc.drag)
			}
		})
	}
}

// One flick of the wheel is several notches, so the wheel clamps where the
// keyboard wraps: going off the end must not fling the selection to the
// far one and aim every key after it somewhere the user never looked.
func TestWheelClampsAtBothEnds(t *testing.T) {
	m := &Model{layout: layoutState{width: 80, height: 24}, services: services{listKeys: keybind.DefaultList()}, rail: railModelCount(3, 2, false)}
	for i := 0; i < 3; i++ {
		updated, _ := m.handleMouse(tea.MouseMsg{
			Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress,
		})
		m = updated.(*Model)
		m.drainEffects(t)
	}
	if m.rail.Cursor() != 2 {
		t.Fatalf("wheel down off the bottom: cursor = %d want it held at 2", m.rail.Cursor())
	}
	setRailCursor(m, 0)
	for i := 0; i < 3; i++ {
		updated, _ := m.handleMouse(tea.MouseMsg{
			Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress,
		})
		m = updated.(*Model)
		m.drainEffects(t)
	}
	if m.rail.Cursor() != 0 {
		t.Fatalf("wheel up off the top: cursor = %d want it held at 0", m.rail.Cursor())
	}
	// The keyboard still wraps: clamping is the wheel's alone.
	m.moveCursor(-1)
	if m.rail.Cursor() != 2 {
		t.Fatalf("arrow up should still wrap to 2, cursor = %d", m.rail.Cursor())
	}
}

func TestWheelSwallowedInResizeMode(t *testing.T) {
	m := &Model{
		layout: layoutState{
			split:  uigeometry.Gesture{ResizeMode: true},
			width:  80,
			height: 24,
		},
		services: services{listKeys: keybind.DefaultList()},
		rail:     railModelCount(2, 0, false),
	}
	updated, _ := m.handleMouse(tea.MouseMsg{
		Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress,
	})
	m = updated.(*Model)
	m.drainEffects(t)
	if m.rail.Cursor() != 0 {
		t.Fatalf("resize mode should swallow wheel, cursor = %d", m.rail.Cursor())
	}
}

// messagesCell finds the column and screen row the word "unread" was
// painted on in the last frame: the card's legend in the split, the badge
// in full screen.
func messagesCell(t *testing.T, m *Model) (x, y int) {
	t.Helper()
	for row, line := range strings.Split(ansi.Strip(preparedView(m)), "\n") {
		if col := strings.Index(line, "unread"); col >= 0 {
			return ansi.StringWidth(line[:col]), row
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

func leftRelease(m *Model, x, y int) *Model {
	m, _ = leftReleaseCmd(m, x, y)
	return m
}

func leftReleaseCmd(m *Model, x, y int) (*Model, tea.Cmd) {
	updated, cmd := m.handleMouse(tea.MouseMsg{
		X: x, Y: y, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft,
	})
	return updated.(*Model), cmd
}

// leftClick presses and releases the left button over x, y.
func leftClick(m *Model, x, y int) *Model {
	return leftRelease(leftPress(m, x, y), x, y)
}

func leftClickCmd(m *Model, x, y int) (*Model, tea.Cmd) {
	return leftReleaseCmd(leftPress(m, x, y), x, y)
}

// wheel turns the wheel one notch over x, y.
func wheel(m *Model, button tea.MouseButton, x, y int) *Model {
	updated, _ := m.handleMouse(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: button})
	return updated.(*Model)
}

// wheelOnCard turns the wheel one notch over the middle of the open card.
func wheelOnCard(m *Model, button tea.MouseButton) *Model {
	preparedView(m)
	card := m.layout.card
	return wheel(m, button, (card.left+card.right)/2, (card.top+card.bottom)/2)
}

// paintedAt is where text starts on the frame row that paints it, the
// lowest such row when fromBottom.
func paintedAt(m *Model, text string, fromBottom bool) (x, y int, ok bool) {
	rows := strings.Split(ansi.Strip(preparedView(m)), "\n")
	for i := range rows {
		row := i
		if fromBottom {
			row = len(rows) - 1 - i
		}
		if col := strings.Index(rows[row], text); col >= 0 {
			return ansi.StringWidth(rows[row][:col]), row, true
		}
	}
	return 0, 0, false
}

// paintedCell is where label starts on the lowest frame row that paints
// it, which is a footer or a card's foot before any body row.
func paintedCell(t *testing.T, m *Model, label string) (x, y int) {
	t.Helper()
	x, y, ok := paintedAt(m, label, true)
	if !ok {
		t.Fatalf("test setup: the frame painted no %q:\n%s", label, ansi.Strip(m.View()))
	}
	return x, y
}

// bodyCell is where text starts on the highest frame row that paints it,
// which is a card's body before its foot.
func bodyCell(t *testing.T, m *Model, text string) (x, y int) {
	t.Helper()
	x, y, ok := paintedAt(m, text, false)
	if !ok {
		t.Fatalf("test setup: the frame painted no %q:\n%s", text, ansi.Strip(m.View()))
	}
	return x, y
}

func paints(m *Model, text string) bool {
	_, _, ok := paintedAt(m, text, false)
	return ok
}

// clickLabel clicks a key a footer or a card's foot painted.
func clickLabel(t *testing.T, m *Model, label string) *Model {
	t.Helper()
	x, y := paintedCell(t, m, label)
	m = leftClick(m, x, y)
	m.drainEffects(t)
	return m
}

// clickBody clicks where a card's body painted text.
func clickBody(t *testing.T, m *Model, text string) (*Model, tea.Cmd) {
	t.Helper()
	x, y := bodyCell(t, m, text)
	return leftClickCmd(m, x, y)
}

func TestClickOnMessagesCardOpensNotices(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	x, y := messagesCell(t, m)
	if m = leftClick(m, x, y); !m.noticesOpen() {
		t.Fatalf("click on the card should open messages, screen = %v", m.screenID())
	}
}

func TestMessagesLegendOpensNoticesByMouseAndKey(t *testing.T) {
	t.Parallel()
	for _, full := range []bool{false, true} {
		for _, width := range []int{30, 60, 120} {
			for _, binding := range []string{"M", "alt+m"} {
				m := buildModel(t)
				m.layout.width, m.layout.height, m.prefs.fullLayout = width, 34, full
				m.prefs.hideStats = true
				m.services.listKeys = m.services.listKeys.With(keybind.Messages, bindingOf(t, binding))
				x, y := messagesCell(t, m)
				if _, ok := m.layout.keyHits.At(x, y); !ok {
					t.Fatalf("full=%v width=%d binding=%s: painted messages entry misses its hit box %+v", full, width, binding, m.layout.keyHits)
				}
				if m = leftClick(m, x, y); !m.noticesOpen() {
					t.Fatalf("click on messages should open notices, screen = %v", m.screenID())
				}
				m.handleKey(key("esc"))
				msg := key(binding)
				if binding == "alt+m" {
					msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}, Alt: true}
				}
				m.handleKey(msg)
				if !m.noticesOpen() {
					t.Fatalf("%s should open notices, screen = %v", binding, m.screenID())
				}
				m.handleKey(key("esc"))
				for _, n := range m.activeNotices() {
					m.dismissNotice(n.ID)
				}
				preparedView(m)
				if footer := ansi.Strip(m.viewFooter()); strings.Contains(footer, "unread") {
					t.Fatalf("with nothing unread the legend must give its room back:\n%s", footer)
				}
				if m = leftClick(m, x, y); !m.onList() {
					t.Fatalf("a click where the entry was must not open Messages, screen = %v", m.screenID())
				}
				m.handleKey(msg)
				if !m.noticesOpen() {
					t.Fatalf("%s should still open an empty Messages, screen = %v", binding, m.screenID())
				}
			}
		}
	}
}

func TestMessagesLegendClickOnlyOpensFromTheList(t *testing.T) {
	for _, change := range []struct {
		name  string
		apply func(*Model)
		ready func(*Model) bool
	}{
		{"search", func(m *Model) { m.rail.SetSearch(m.rail.Search(), true) }, func(m *Model) bool { return m.rail.Searching() }},
		{"quick prompt", func(m *Model) { m.openQuickMode() }, func(m *Model) bool { return (m.quick() != nil) }},
		{"resize", func(m *Model) { m.enterResizeMode() }, func(m *Model) bool { return m.layout.split.ResizeMode }},
		{"messages key off", func(m *Model) { m.services.listKeys = m.services.listKeys.With(keybind.Messages, bindingOf(t)) }, func(*Model) bool { return true }},
		{"settings", func(m *Model) { m.openSettings() }, func(m *Model) bool { return m.settingsOpen() }},
		{"short terminal", func(m *Model) { m.layout.height = 2 }, func(*Model) bool { return true }},
	} {
		t.Run(change.name, func(t *testing.T) {
			m := buildModel(t)
			m.layout.width, m.layout.height = 120, 34
			x, y := messagesCell(t, m)
			change.apply(m)
			if !change.ready(m) {
				t.Fatalf("test setup: %s did not take, screen = %v err = %q", change.name, m.screenID(), m.errBar.text)
			}
			preparedView(m)
			if m = leftClick(m, x, y); m.noticesOpen() {
				t.Fatal("a click where messages was painted opened notices")
			}
		})
	}
}

func TestClickOnMetersBesideTheCardDoesNothing(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	_, y := messagesCell(t, m)
	if m = leftClick(m, 2, y); !m.onList() {
		t.Fatalf("click on the meters should stay on the list, screen = %v", m.screenID())
	}
}

func TestClickOnFullScreenBadgeOpensNotices(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	m.prefs.fullLayout = true
	x, y := messagesCell(t, m)
	if m = leftClick(m, x, y); !m.noticesOpen() {
		t.Fatalf("click on the badge should open messages, screen = %v", m.screenID())
	}
	m.base = baseList
	m.clearSurfaces()
	if m = leftClick(m, 2, y); !m.onList() {
		t.Fatalf("click on the readings should stay on the list, screen = %v", m.screenID())
	}
}

func TestClickOnMessagesCardWhileSearchingKeepsTheField(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	m.rail.SetSearch(m.rail.Search(), true)
	x, y := messagesCell(t, m)
	if m = leftClick(m, x, y); !m.onList() || !m.rail.Searching() {
		t.Fatalf("search owns the click, screen = %v searching = %v", m.screenID(), m.rail.Searching())
	}
}

func TestClickOnMessagesCardWhileFocusedIsLeftToFocus(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.layout.width, m.layout.height = 200, 34
	createSession(t, m, "alpha", t.TempDir(), "")
	x, y := messagesCell(t, m)
	m.selectSessionRow(t, "alpha")
	updated, _ := m.focusSelected()
	m = updated.(*Model)
	m.drainEffects(t)
	if !m.focused() {
		t.Fatalf("test setup: focus alpha, screen = %v, err = %q", m.screenID(), m.errBar.text)
	}
	preparedView(m)
	if m = leftClick(m, x, y); m.noticesOpen() {
		t.Fatal("a click while focused must not open messages")
	}
}

func TestSingleClickFocusesOnRelease(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")

	line := paintedRailLines(t, m, "alpha")[0]
	y0, _ := m.bodyYRange()
	at := tea.MouseMsg{X: 2, Y: y0 + line, Button: tea.MouseButtonLeft}
	at.Action = tea.MouseActionPress
	updated, _ := m.handleMouse(at)
	m.drainEffects(t)
	m = updated.(*Model)
	m.drainEffects(t)
	if !m.onList() {
		t.Fatalf("the press alone should only select, screen = %v", m.screenID())
	}
	at.Action = tea.MouseActionRelease
	updated, _ = m.handleMouse(at)
	m.drainEffects(t)
	m = updated.(*Model)
	m.drainEffects(t)
	if !m.focused() {
		t.Fatalf("release should focus, screen = %v, err = %q", m.screenID(), m.errBar.text)
	}
	if sess, ok := m.selected(); !ok || sess.Name != "alpha" {
		t.Fatalf("should focus alpha, got %q ok=%v", sess.Name, ok)
	}
}

func TestSingleClickFocusesWhenEnterAttaches(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.prefs.focusOnEnter = false
	createSession(t, m, "alpha", t.TempDir(), "")

	line := paintedRailLines(t, m, "alpha")[0]
	y0, _ := m.bodyYRange()
	at := tea.MouseMsg{X: 2, Y: y0 + line, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	updated, _ := m.handleMouse(at)
	m.drainEffects(t)
	m = updated.(*Model)
	m.drainEffects(t)
	at.Action = tea.MouseActionRelease
	updated, _ = m.handleMouse(at)
	m.drainEffects(t)
	m = updated.(*Model)
	m.drainEffects(t)
	if !m.focused() {
		t.Fatalf("a click should focus even when Enter attaches, screen = %v, err = %q", m.screenID(), m.errBar.text)
	}
}

func TestSingleClickReleasedOnAnotherRowDoesNotFocus(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")

	y0, _ := m.bodyYRange()
	alpha := paintedRailLines(t, m, "alpha")[0]
	beta := paintedRailLines(t, m, "beta")[0]
	updated, _ := m.handleMouse(tea.MouseMsg{X: 2, Y: y0 + alpha, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m.drainEffects(t)
	m = updated.(*Model)
	m.drainEffects(t)
	updated, _ = m.handleMouse(tea.MouseMsg{X: 2, Y: y0 + beta, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
	m.drainEffects(t)
	m = updated.(*Model)
	m.drainEffects(t)
	updated, _ = m.handleMouse(tea.MouseMsg{X: 2, Y: y0 + alpha, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	m.drainEffects(t)
	m = updated.(*Model)
	m.drainEffects(t)
	if !m.onList() {
		t.Fatalf("a press dragged off its row should not focus, screen = %v", m.screenID())
	}
}

func TestSingleClickInFullLayoutOnlySelects(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.prefs.fullLayout = true
	createSession(t, m, "alpha", t.TempDir(), "")

	line := paintedRailLines(t, m, "alpha")[0]
	y0, _ := m.bodyYRange()
	at := tea.MouseMsg{X: 2, Y: y0 + line, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	updated, _ := m.handleMouse(at)
	m.drainEffects(t)
	m = updated.(*Model)
	m.drainEffects(t)
	at.Action = tea.MouseActionRelease
	updated, _ = m.handleMouse(at)
	m.drainEffects(t)
	m = updated.(*Model)
	m.drainEffects(t)
	if !m.onList() {
		t.Fatalf("full layout keeps click as select, screen = %v", m.screenID())
	}
}

func TestClicksWorkWhileTheQuickBarIsOpen(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.openQuickMode()
	if m.quick() == nil {
		t.Fatal("test setup: quick bar should open")
	}
	y0, _ := m.bodyYRange()
	beta := y0 + paintedRailLines(t, m, "beta")[0]

	updated, _ := m.handleMouse(tea.MouseMsg{X: m.layout.displayedRail.Width, Y: beta, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m.drainEffects(t)
	m = updated.(*Model)
	m.drainEffects(t)
	if !m.rail.MenuOpen() || (m.quick() == nil) {
		t.Fatalf("%s should open the menu over the open quick bar, menu = %v quick = %v", rowMenuGlyph, m.rail.MenuOpen(), (m.quick() != nil))
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	m.drainEffects(t)
	if m.rail.MenuOpen() || (m.quick() == nil) {
		t.Fatalf("esc should close the menu first and leave the bar, menu = %v quick = %v", m.rail.MenuOpen(), (m.quick() != nil))
	}

	m = liftByHandle(t, m, "beta")
	if m.viewFooter() == "" || !strings.Contains(ansi.Strip(m.viewFooter()), "Reorder") {
		t.Fatal("a lifted row should own the footer over the quick bar")
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.drainEffects(t)

	at := tea.MouseMsg{X: 8, Y: y0 + paintedRailLines(t, m, "alpha")[0], Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	updated, _ = m.handleMouse(at)
	m = updated.(*Model)
	m.drainEffects(t)
	at.Action = tea.MouseActionRelease
	updated, _ = m.handleMouse(at)
	m = updated.(*Model)
	m.drainEffects(t)
	if !m.focused() || (m.quick() != nil) {
		t.Fatalf("a row click should focus and close the bar, screen = %v quick = %v", m.screenID(), (m.quick() != nil))
	}
}

// A corner of the terminal is backdrop under every centered card.
func clickBackdrop(t *testing.T, m *Model) *Model {
	t.Helper()
	preparedView(m)
	if m.layout.card.holds(0, 0) {
		t.Fatalf("test setup: the card covers the corner: %+v", m.layout.card)
	}
	m = leftClick(m, 0, 0)
	m.drainEffects(t)
	return m
}

func TestBackdropClickClosesACardTheWayEscDoes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		open func(*Model)
	}{
		{"help", func(m *Model) { m.openHelp() }},
		{"messages", func(m *Model) { m.openNotices("") }},
		{"settings", func(m *Model) { m.applyCmd(t, m.openSettings()) }},
		{"confirm", func(m *Model) {
			m.openConfirm(confirmTarget{prompt: uidialogs.Confirm{Action: uidialogs.ConfirmKill, Label: "kill alpha? frees its RAM."}, lifecycle: uilifecycle.Target{Action: uilifecycle.Action(uidialogs.ConfirmKill)}})
		}},
		{"move", func(m *Model) { m.openMove() }},
	} {
		m := buildModel(t)
		m.layout.width, m.layout.height = 120, 34
		createSession(t, m, "alpha", t.TempDir(), "")
		m.selectSessionRow(t, "alpha")
		tc.open(m)
		if m.listClear() {
			t.Fatalf("%s: test setup did not open the card, err %q", tc.name, m.errBar.text)
		}
		if m = clickBackdrop(t, m); !m.listClear() {
			t.Errorf("%s: a backdrop click should close the card, screen = %v", tc.name, m.screenID())
		}
		if confirmOpen(m) {
			t.Errorf("%s: a backdrop click on a confirm is its cancel, target = %+v", tc.name, pendingConfirm(m))
		}
	}
}

func TestBackdropClickKeepsATypedDraft(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		open func(*Model)
	}{
		{"new session", func(m *Model) { m.applyCmd(t, m.openForm()) }},
		{"new group", func(m *Model) { m.openGroupForm() }},
		{"fork", func(m *Model) {
			m.push(uidialogs.NewForkSurface(forkRoot{m, store.Session{Name: "alpha"}}, "alpha", "", 1))
		}},
	} {
		m := buildModel(t)
		m.layout.width, m.layout.height = 120, 34
		createSessionOn(t, m, "alpha", "claude", t.TempDir())
		m.selectSessionRow(t, "alpha")
		tc.open(m)
		if len(m.surfaces) != 1 {
			t.Fatalf("%s: test setup put %d surfaces up, err %q", tc.name, len(m.surfaces), m.errBar.text)
		}
		if m = clickBackdrop(t, m); len(m.surfaces) != 1 {
			t.Errorf("%s: a stray click closed the draft", tc.name)
		}
	}
}

func TestBackdropClickKeepsTheSettingsEditorField(t *testing.T) {
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	editorSettings(t, m, "code")
	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyLeft})
	m.pressInSettings(t, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.settingsCard().State().Editor.Typing() {
		t.Fatal("test setup: enter on custom opens the field")
	}
	if m = clickBackdrop(t, m); !m.settingsOpen() || !m.settingsCard().State().Editor.Typing() {
		t.Fatalf("a stray click should keep the typed line open, screen = %v typing = %v", m.screenID(), m.settingsCard().State().Editor.Typing())
	}
}

// A click inside the card that lands on nothing it paints leaves it open.
func TestClickOnACardsOwnBlankStaysOpen(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	m.openHelp()
	preparedView(m)
	if m = leftClick(m, m.layout.card.left+1, m.layout.card.top+1); !helpOpen(m) {
		t.Fatalf("a click on the card's own edge should leave it open, screen = %v", m.screenID())
	}
}

func TestBackdropClickKeepsASavedPromptDraft(t *testing.T) {
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	m.applyCmd(t, m.openSettings())
	m.applyCmd(t, m.runSettingsResult(m.settingsCard().State(), m.settingsCard().State().OpenPrompts()))
	m.pressInSettings(t, runeKey("n"))
	if !m.settingsCard().State().Prompts.Editing() {
		t.Fatal("test setup: n opens the prompt editor")
	}
	if m = clickBackdrop(t, m); !m.settingsOpen() || !m.settingsCard().State().Prompts.Editing() {
		t.Fatalf("a stray click should keep the prompt being typed, screen = %v", m.screenID())
	}
}

// The saved prompts card over a focused session covers the focus footer,
// so only the card's own keys answer a click.
func TestSavedPromptsOverFocusHideTheFooterKeys(t *testing.T) {
	m := focusedModel(t)
	m.openFocusSaved()
	preparedView(m)
	if len(m.layout.keyHits) == 0 {
		t.Fatal("test setup: the card paints its own keys")
	}
	for _, hit := range m.layout.keyHits {
		if !m.layout.card.holds(hit.X0, hit.Y) {
			t.Fatalf("a key at %d,%d answers outside the card %+v", hit.X0, hit.Y, m.layout.card)
		}
	}
}

// A rename typed into its row holds the list still under the mouse.
func TestRenameHoldsTheListUnderTheMouse(t *testing.T) {
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	m.openRename()
	x, y := bodyCell(t, m, "beta")
	m = leftClick(m, x, y)
	m = wheel(m, tea.MouseButtonWheelDown, x, y)
	if row, ok := m.selectedRow(); !renameOpen(m) || !ok || row.sess.Name != "alpha" {
		t.Fatalf("the mouse moved the list under a rename: mode %v row %+v", m.screenID(), row.sess.Name)
	}
}

// A card answers a click when the button comes back up over the spot the
// press landed on, so a press on the backdrop released over the card
// closes nothing.
func TestACardClickRunsOnTheRelease(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	m.openHelp()
	preparedView(m)
	card := m.layout.card
	if m = leftPress(m, 0, 0); !helpOpen(m) {
		t.Fatalf("the press alone closed the card, screen = %v", m.screenID())
	}
	if m = leftRelease(m, card.left+1, card.top+1); !helpOpen(m) {
		t.Fatalf("a release over the card closed it, screen = %v", m.screenID())
	}
	if m = leftClick(m, 0, 0); !m.onList() {
		t.Fatalf("a click on the backdrop should close the card, screen = %v", m.screenID())
	}
}

// mouseOpeners mounts every surface the way the user does, on a list with
// a selected session. A surface type missing here fails, so a new surface
// is driven by the mouse on purpose.
var mouseOpeners = map[string]func(t *testing.T, m *Model){
	"dialogs.ConfirmSurface": func(t *testing.T, m *Model) {
		m.openConfirm(confirmTarget{prompt: uidialogs.Confirm{Action: uidialogs.ConfirmKill, Label: "kill alpha?"}, lifecycle: uilifecycle.Target{Action: uilifecycle.Action(uidialogs.ConfirmKill)}})
	},
	"dialogs.MoveSurface":   func(t *testing.T, m *Model) { m.openMove() },
	"dialogs.RenameSurface": func(t *testing.T, m *Model) { m.openRename() },
	"dialogs.ForkSurface": func(t *testing.T, m *Model) {
		source := railSelectedSession(m)
		m.push(uidialogs.NewForkSurface(forkRoot{m, source}, source.Name, source.Group, 1))
	},
	"dialogs.LaunchHintSurface": func(t *testing.T, m *Model) {
		m.openLaunchHint(uiinstall.Fix{Text: "tool is missing", Command: "true", Binary: "tool"})
	},
	"help.Surface":             func(t *testing.T, m *Model) { m.openHelp() },
	"notices.Surface":          func(t *testing.T, m *Model) { m.Update(key("M")) },
	"settings.Surface":         func(t *testing.T, m *Model) { m.applyCmd(t, m.openSettings()) },
	"dialogs.GroupFormSurface": func(t *testing.T, m *Model) { m.openGroupForm() },
	"connections.Surface":      func(t *testing.T, m *Model) { m.openConnectionDialog(remote.Connection{}) },
	"quick.Surface":            func(t *testing.T, m *Model) { m.applyCmd(t, m.openQuickMode()) },
	"spawn.Surface":            func(t *testing.T, m *Model) { m.applyCmd(t, m.openForm()) },
	"geometry.Surface":         func(t *testing.T, m *Model) { m.enterResizeMode() },
	"focus.Surface":            func(t *testing.T, m *Model) { m.push(newFocusSurface(m)) },
	"focus.Saved": func(t *testing.T, m *Model) {
		m.push(newFocusSurface(m))
		m.push(uifocus.OpenSaved(focusSavedRoot{m}, 0, m.spawnStyles()))
	},
	"review.Surface": func(t *testing.T, m *Model) {
		m.base = baseReview
		m.push(uireview.NewSurface(reviewRoot{m}, []uireview.PickerRow{{Label: "main"}}, "branch", uireview.PickRepo, "", uireview.PickerSource{}, "", uireview.RenderContext{}, ""))
	},
}

// Every surface answers the mouse: a wheel and a click over it neither
// panic nor touch the stack under it.
func TestEverySurfaceAnswersTheMouse(t *testing.T) {
	for _, name := range declaredSurfaces(t) {
		open, ok := mouseOpeners[name]
		if !ok {
			t.Errorf("%s has no opener in mouseOpeners: drive it by the mouse here", name)
			continue
		}
		t.Run(name, func(t *testing.T) {
			m := buildModel(t)
			m.layout.width, m.layout.height = 120, 40
			createSession(t, m, "alpha", t.TempDir(), "")
			m.selectSessionRow(t, "alpha")
			open(t, m)
			top, ok := m.top()
			if !ok || surfaceName(top) != name {
				t.Fatalf("the opener did not put %s on top, stack %d, err %q", name, len(m.surfaces), m.errBar.text)
			}
			below := len(m.surfaces) - 1
			preparedView(m)
			x, y := m.layout.width/2, m.layout.height/2
			for _, msg := range []tea.MouseMsg{
				{X: x, Y: y, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress},
				{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress},
				{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease},
			} {
				m.Update(msg)
				if len(m.surfaces) < below {
					t.Fatalf("a mouse event over %s popped the stack under it", name)
				}
			}
		})
	}
	for name := range mouseOpeners {
		if !contains(declaredSurfaces(t), name) {
			t.Errorf("mouseOpeners names %s, which no longer implements surface", name)
		}
	}
}

// surfaceName names a surface the way declaredSurfaces does: bare in the
// root, qualified by its package in a feature's.
func surfaceName(s uisurface.Surface) string {
	typ := reflect.TypeOf(s).Elem()
	if pkg := path.Base(typ.PkgPath()); pkg != "ui" {
		return pkg + "." + typ.Name()
	}
	return typ.Name()
}

// declaredSurfaces lists the types that declare a Leave(uisurface.Host) method on a
// pointer receiver, in the root and in the feature packages under it: every
// surface does, and nothing else.
func declaredSurfaces(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	dirs := []string{"."}
	for _, entry := range entries {
		if entry.IsDir() && entry.Name() != "testdata" {
			dirs = append(dirs, entry.Name())
		}
	}
	var names []string
	for _, dir := range dirs {
		fset := token.NewFileSet()
		packages, err := parser.ParseDir(fset, dir, func(info fs.FileInfo) bool {
			return !strings.HasSuffix(info.Name(), "_test.go")
		}, 0)
		if err != nil {
			t.Fatal(err)
		}
		for pkg, parsed := range packages {
			for _, file := range parsed.Files {
				for _, decl := range file.Decls {
					fn, ok := decl.(*ast.FuncDecl)
					if !ok || fn.Recv == nil || fn.Name.Name != "Leave" || !takesHost(fn) {
						continue
					}
					if star, ok := fn.Recv.List[0].Type.(*ast.StarExpr); ok {
						if ident, ok := star.X.(*ast.Ident); ok {
							name := ident.Name
							if pkg != "ui" {
								name = pkg + "." + name
							}
							names = append(names, name)
						}
					}
				}
			}
		}
	}
	if len(names) == 0 {
		t.Fatal("found no surfaces")
	}
	return names
}

// takesHost is whether fn's one parameter is the stack's host, which tells a
// surface's Leave from another type's.
func takesHost(fn *ast.FuncDecl) bool {
	params := fn.Type.Params.List
	if len(params) != 1 {
		return false
	}
	if typ, ok := params[0].Type.(*ast.SelectorExpr); ok {
		return typ.Sel.Name == "Host"
	}
	return false
}

func TestSettingsWheelReachesSymbolsAndDocsInShortWindows(t *testing.T) {
	// One row shorter than the symbols line. Save lives in the footer, so the
	// body spends that row on a field.
	for _, size := range [][2]int{{34, 11}, {80, 10}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			m := buildModel(t)
			m.layout.width, m.layout.height = size[0], size[1]
			m.runBatch(t, m.openSettings())
			if paints(m, "symbols") {
				t.Fatal("symbols should start below this viewport")
			}
			for range uisettings.FieldGlyphs {
				m = wheelOnCard(m, tea.MouseButtonWheelDown)
			}
			if m.settingsCard().State().Field != uisettings.FieldGlyphs || !paints(m, "symbols") {
				t.Fatal("wheel did not reveal symbols")
			}
			if m, _ = clickBody(t, m, "symbols"); m.prefs.glyphMode != presentation.GlyphModeBasic {
				t.Fatal("revealed symbols row was not clickable")
			}
			for range uisettings.FieldDocs - uisettings.FieldGlyphs {
				m = wheelOnCard(m, tea.MouseButtonWheelDown)
			}
			if m.settingsCard().State().Field != uisettings.FieldDocs || !paints(m, "docs") {
				t.Fatal("wheel did not reveal docs")
			}
			var opened string
			openBrowser = func(url string) error { opened = url; return nil }
			t.Cleanup(func() { openBrowser = uiterminal.DefaultOpenBrowser })
			m, docs := clickBody(t, m, "docs")
			m.runBatch(t, docs)
			if opened != docsURL {
				t.Fatal("docs click did not follow the scrolled row")
			}
			for range uisettings.FieldDocs - uisettings.FieldGlyphs {
				m = wheelOnCard(m, tea.MouseButtonWheelUp)
			}
			if m.settingsCard().State().Field != uisettings.FieldGlyphs || !paints(m, "symbols") {
				t.Fatal("wheel up did not return to symbols")
			}
		})
	}
}

func TestSettingsWheelLeavesSubpanelsAlone(t *testing.T) {
	for _, panel := range []string{"CLIs", "keys", "editor"} {
		t.Run(panel, func(t *testing.T) {
			m := buildModel(t)
			m.runBatch(t, m.openSettings())
			switch panel {
			case "CLIs":
				m.settingsCard().State().Field = uisettings.FieldCLIs
				m.handleSettingsKey(key("enter"))
			case "keys":
				m.settingsCard().State().Field = uisettings.FieldKeybindings
				m.handleSettingsKey(key("enter"))
			case "editor":
				m.settingsCard().State().Field = uisettings.FieldEditor
				m.settingsCard().State().Editor = uisettings.NewEditorRow("custom --test", nil)
				m.handleSettingsKey(key("enter"))
			}
			field := m.settingsCard().State().Field
			for _, button := range []tea.MouseButton{tea.MouseButtonWheelDown, tea.MouseButtonWheelUp} {
				if m = wheelOnCard(m, button); m.settingsCard().State().Field != field {
					t.Fatal("wheel in a subpanel moved the parent Settings field")
				}
			}
		})
	}
}

// The guard has to be transparent while the application really is tracking
// the mouse, and silent once it has stopped.
func TestGuardedMouseCommandFollowsTheLiveFlag(t *testing.T) {
	if _, err := exec.LookPath(tmux.Binary); err != nil {
		t.Skip(tmux.Binary + " not installed")
	}
	command := `printf '\033[?1003h\033[?1006h'; cat`
	if runtime.GOOS == "windows" {
		command = testfixture.Command("mouse-tool")
	}
	driver, err := tmux.NewWithSocket("amguard")
	if err != nil {
		t.Fatalf("driver: %v", err)
	}
	sessID := "guard-probe"
	ready := filepath.Join(t.TempDir(), "ready")
	if err := driver.Create(sessID, t.TempDir(), command, map[string]string{testfixture.ReadyEnv: ready}, 80, 24); err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = driver.Kill(sessID) })
	waitForMouseFlag(t, driver, sessID, "1")
	if runtime.GOOS == "windows" {
		// The flag is up once the escape is printed, before cat has a raw
		// console to read a report through.
		testfixture.WaitForReady(t, ready)
	}

	_, args := tmux.GuardedMouseReport(tmux.PaneTarget(sessID), "\x1b[<64;7;3M")
	if err := driver.SendCommand(args...); err != nil {
		t.Fatalf("guarded send while tracking: %v", err)
	}
	if !paneGains(t, driver, sessID, "64;7;3M") {
		t.Fatal("a report was dropped while the application was tracking the mouse")
	}

	stopMouseTracking(t, driver, sessID)

	_, args = tmux.GuardedMouseReport(tmux.PaneTarget(sessID), "\x1b[<64;9;4M")
	if err := driver.SendCommand(args...); err != nil {
		t.Fatalf("guarded send after tracking stopped: %v", err)
	}
	if paneGains(t, driver, sessID, "64;9;4M") {
		pane, _ := driver.CapturePane(sessID)
		t.Fatalf("a report reached the pane after the application left mouse mode:\n%s", strings.TrimSpace(pane))
	}
}

// A report forwarded over the control pipe must leave the replies behind it
// on their own commands, whichever branch the guard takes: a capture that
// receives the guard's spare block paints the focused pane blank.
func TestGuardedMouseCommandKeepsControlRepliesAligned(t *testing.T) {
	skipControlMode(t)
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	driver, err := tmux.NewWithSocket("amguard")
	if err != nil {
		t.Fatalf("driver: %v", err)
	}
	sessID := "guard-replies"
	if err := driver.Create(sessID, t.TempDir(),
		`printf '\033[?1003h\033[?1006h'; cat`, map[string]string{}, 80, 24); err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = driver.Kill(sessID) })
	control, err := driver.OpenControl(sessID)
	if err != nil {
		t.Fatalf("control: %v", err)
	}
	t.Cleanup(func() { _ = control.Close() })

	expectAligned := func(state string) {
		t.Helper()
		command, _ := tmux.GuardedMouseReport(tmux.PaneTarget(sessID), "\x1b[<64;7;3M")
		if err := control.SendBlocks(command, tmux.GuardedMouseReplies); err != nil {
			t.Fatalf("guarded send while %s: %v", state, err)
		}
		for _, marker := range []string{"first", "second"} {
			reply, err := control.Command("display-message -p " + marker)
			if err != nil || reply != marker {
				t.Fatalf("while %s, reply = %q (%v), want %q", state, reply, err, marker)
			}
		}
	}
	waitForMouseFlag(t, driver, sessID, "1")
	expectAligned("tracking the mouse")

	stopMouseTracking(t, driver, sessID)
	expectAligned("not tracking the mouse")
}

// cat holds the line until a newline arrives, and only what it echoes
// reaches the pane's terminal to turn mouse reporting off.
func stopMouseTracking(t *testing.T, driver *tmux.Driver, sessID string) {
	t.Helper()
	if err := driver.SendRaw("send-keys -t " + tmux.PaneTarget(sessID) +
		" -H 1b 5b 3f 31 30 30 33 6c 1b 5b 3f 31 30 30 36 6c 0a"); err != nil {
		t.Fatalf("disable: %v", err)
	}
	waitForMouseFlag(t, driver, sessID, "0")
}

func waitForMouseFlag(t *testing.T, driver *tmux.Driver, sessID, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		cmd := exec.Command(tmux.Binary, "-L", driver.SocketName(),
			"display-message", "-p", "-t", tmux.PaneTarget(sessID), "#{mouse_any_flag}")
		if runtime.GOOS == "windows" {
			// The same PSMUX_NO_WARM tmuxCmd sets, so a poll does not
			// start a standby server.
			cmd.Env = append(os.Environ(), "PSMUX_NO_WARM=1")
		}
		out, err := cmd.Output()
		if err == nil && strings.TrimSpace(string(out)) == want {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("mouse_any_flag never reached %q", want)
}

func paneGains(t *testing.T, driver *tmux.Driver, sessID, want string) bool {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if pane, err := driver.CapturePane(sessID); err == nil && strings.Contains(pane, want) {
			return true
		}
		time.Sleep(25 * time.Millisecond)
	}
	return false
}

func TestClickOnListFooterGroupKeyOpensTheCard(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.layout.width, m.layout.height = 320, 40
	createSession(t, m, "alpha", t.TempDir(), "")
	if m = clickLabel(t, m, "g group"); !m.groupFormOpen() {
		t.Fatalf("click on the group key did not open the card (err %q)", m.errBar.text)
	}
}

func TestClickOnListFooterSettingsKeyOpensTheCard(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.layout.width, m.layout.height = 320, 40
	createSession(t, m, "alpha", t.TempDir(), "")
	if m = clickLabel(t, m, "s settings"); !m.settingsOpen() {
		t.Fatalf("click on the settings key did not open the card (err %q)", m.errBar.text)
	}
}

func TestClickOnListFooterKeysOpensTheirModals(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		label string
		open  func(*Model) bool
	}{
		{"n new", formOpen},
		{"g group", func(m *Model) bool { return m.groupFormOpen() }},
		{"? keys", func(m *Model) bool { _, ok := surfaceOf[*uihelp.Surface](m); return ok }},
		{"s settings", func(m *Model) bool { return m.settingsOpen() }},
	} {
		m := buildModel(t)
		m.layout.width, m.layout.height = 320, 40
		createSession(t, m, "alpha", t.TempDir(), "")
		if m = clickLabel(t, m, tc.label); !tc.open(m) {
			t.Errorf("click on %q opened nothing (err %q)", tc.label, m.errBar.text)
		}
	}
}

// A footer names keys the search field would take as typing, so the field
// keeps a click on them.
func TestClickOnListFooterWhileSearchingTypesNothing(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.layout.width, m.layout.height = 200, 40
	x, y := paintedCell(t, m, "n new")
	m.rail.SetSearch(m.rail.Search(), true)
	preparedView(m)
	if m = leftClick(m, x, y); !m.onList() || !m.rail.Searching() || m.rail.Search() != "" {
		t.Fatalf("search owns the click, screen = %v searching = %v query = %q", m.screenID(), m.rail.Searching(), m.rail.Search())
	}
}

func TestClickOnConfirmFootAnswers(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	m.openConfirm(confirmTarget{prompt: uidialogs.Confirm{Action: uidialogs.ConfirmKill, Label: "kill alpha? frees its RAM."}, lifecycle: uilifecycle.Target{Action: uilifecycle.Action(uidialogs.ConfirmKill)}})
	if m = clickLabel(t, m, "n/esc cancel"); !m.onList() || pendingConfirm(m).prompt.Label != "" {
		t.Fatalf("cancel in the foot should close the confirm, screen = %v target = %+v", m.screenID(), pendingConfirm(m))
	}
}

func TestClickOnHelpFootCloses(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	m.openHelp()
	if m = clickLabel(t, m, "esc/q close"); !m.onList() {
		t.Fatalf("close in the foot should leave help, screen = %v", m.screenID())
	}
}

func TestClickOnMessagesFootRunsItsKeys(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	m.openNotices("")
	if len(m.activeNotices()) == 0 {
		t.Fatal("test setup: a fresh model shows the welcome notice")
	}
	for _, key := range []string{"↑↓ pick", "pgup/pgdn"} {
		x, y := paintedCell(t, m, key)
		if _, ok := m.layout.keyHits.At(x, y); ok {
			t.Fatalf("%q moves the cursor; the rows and the wheel answer for it", key)
		}
	}
	if m = clickLabel(t, m, "esc"); !m.onList() {
		t.Fatalf("esc in the foot should close messages, screen = %v", m.screenID())
	}
}

func TestClickOnFocusFootLeavesFocus(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.layout.width, m.layout.height = 200, 40
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	updated, _ := m.focusSelected()
	m = updated.(*Model)
	m.drainEffects(t)
	if !m.focused() {
		t.Fatalf("test setup: focus alpha, screen = %v, err = %q", m.screenID(), m.errBar.text)
	}
	if m = clickLabel(t, m, "mouse back"); !m.onList() {
		t.Fatalf("the back pair in the foot should leave focus, screen = %v", m.screenID())
	}
}

// A key runs when the button comes back up over it, so whatever it opens
// starts after the release. A press that slides off the key runs nothing.
func TestAKeyRunsOnTheRelease(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.layout.width, m.layout.height = 200, 40
	x, y := paintedCell(t, m, "? keys")
	if m = leftPress(m, x, y); !m.onList() {
		t.Fatalf("the press alone ran the key, screen = %v", m.screenID())
	}
	if m = leftRelease(m, 0, y); !m.onList() {
		t.Fatalf("a release off the key ran it, screen = %v", m.screenID())
	}
	if m = leftRelease(m, x, y); !m.onList() {
		t.Fatalf("a second release with no press ran the key, screen = %v", m.screenID())
	}
	if m = leftClick(m, x, y); !helpOpen(m) {
		t.Fatalf("a click on the key should run it, screen = %v", m.screenID())
	}
}

// A key repainted under a held button is not the key the press chose, so
// the release runs nothing.
func TestARepaintedKeyDoesNotRunOnTheRelease(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.layout.width, m.layout.height = 200, 40
	x, y := paintedCell(t, m, "? keys")
	m = leftPress(m, x, y)
	repainted := false
	for i, hit := range m.layout.keyHits {
		if hit.Y == y && x >= hit.X0 && x < hit.X1 {
			m.layout.keyHits[i].Key, repainted = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")}, true
		}
	}
	if !repainted {
		t.Fatal("test setup: no key painted under the press")
	}
	if m = leftRelease(m, x, y); !m.onList() {
		t.Fatalf("the release ran the repainted key, screen = %v", m.screenID())
	}
}

func focusedModel(t *testing.T) *Model {
	t.Helper()
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	updated, _ := m.focusSelected()
	m = updated.(*Model)
	m.drainEffects(t)
	if !m.focused() {
		t.Fatalf("test setup: focus alpha, screen = %v, err = %q", m.screenID(), m.errBar.text)
	}
	return m
}

// A press anywhere in the rail column leaves focus on the press, the blank
// below the last row included, not only on a row.
func TestPressOnTheRailsBlankLeavesFocus(t *testing.T) {
	t.Parallel()
	m := focusedModel(t)
	preparedView(m)
	y0, _ := m.bodyYRange()
	blank := -1
	for line, row := range railHitRows(m) {
		if row < 0 && line > 0 {
			blank = y0 + line
			break
		}
	}
	if blank < 0 {
		t.Fatal("test setup: the rail paints no line without a row")
	}
	if m = leftPress(m, 2, blank); !m.onList() {
		t.Fatalf("a press on the rail's blank should leave focus, screen = %v", m.screenID())
	}
	if sess, ok := m.selected(); !ok || sess.Name != "alpha" {
		t.Fatalf("leaving should keep the cursor on alpha, got %q", sess.Name)
	}
}

func TestPressOnTheDividerKeepsFocus(t *testing.T) {
	t.Parallel()
	m := focusedModel(t)
	preparedView(m)
	y0, _ := m.bodyYRange()
	for _, x := range []int{m.dividerX(), m.dividerX() + uigeometry.SplitHitSlop} {
		if m = leftPress(m, x, y0+1); !m.focused() {
			t.Fatalf("a press on the divider at %d left focus", x)
		}
	}
}

// The preview only focuses a session that focus would open: a group, an
// archived row, the search field and the quick bar keep the press, and
// full screen has no preview beside the list.
func TestPreviewPressFocusesOnlyALiveSession(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		setup func(*Model)
	}{
		{"group", func(m *Model) { m.selectGroupRow(t, "work") }},
		{"search", func(m *Model) { m.rail.SetSearch(m.rail.Search(), true) }},
		{"quick bar", func(m *Model) { m.openQuickMode() }},
		{"full screen", func(m *Model) { m.prefs.fullLayout = true }},
	} {
		m := buildModel(t)
		if err := m.services.store.CreateGroup("work", ""); err != nil {
			t.Fatal(err)
		}
		m.applyCmd(t, m.refreshCmd())
		createSession(t, m, "alpha", t.TempDir(), "work")
		m.selectSessionRow(t, "alpha")
		tc.setup(m)
		preparedView(m)
		y0, _ := m.bodyYRange()
		if m = leftPress(m, m.layout.width-3, y0+1); m.focused() {
			t.Errorf("%s: a press on the preview focused", tc.name)
		}
	}
}
