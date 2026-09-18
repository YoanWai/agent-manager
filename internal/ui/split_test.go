package ui

import (
	"github.com/YoanWai/agent-manager/internal/keybind"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type memSettings map[string]string

func (m memSettings) Setting(key string) (string, error) {
	return m[key], nil
}

func TestLoadSplitRatio(t *testing.T) {
	if got := loadSplitRatio(memSettings{}); got != defaultSplitRatio {
		t.Fatalf("empty setting: got %v want %v", got, defaultSplitRatio)
	}
	if got := loadSplitRatio(memSettings{splitRatioSetting: "0.5"}); got != 0.5 {
		t.Fatalf("stored 0.5: got %v", got)
	}
	if got := loadSplitRatio(memSettings{splitRatioSetting: "nope"}); got != defaultSplitRatio {
		t.Fatalf("garbage should fall back, got %v", got)
	}
	if got := loadSplitRatio(memSettings{splitRatioSetting: "1.5"}); got != defaultSplitRatio {
		t.Fatalf("out of range should fall back, got %v", got)
	}
	if got := loadSplitRatio(memSettings{splitRatioSetting: "0"}); got != defaultSplitRatio {
		t.Fatalf("zero should fall back, got %v", got)
	}
}

func TestClampSplitLeft(t *testing.T) {
	if got := clampSplitLeft(10, 100); got != minSplitSide {
		t.Fatalf("below min left: got %d want %d", got, minSplitSide)
	}
	if got := clampSplitLeft(90, 100); got != 100-minSplitSide {
		t.Fatalf("below min right: got %d want %d", got, 100-minSplitSide)
	}
	if got := clampSplitLeft(40, 100); got != 40 {
		t.Fatalf("in range: got %d want 40", got)
	}
	// Narrow terminal cannot honor both floors; keep both sides visible.
	if got := clampSplitLeft(0, 50); got != 1 {
		t.Fatalf("narrow zero: got %d want 1", got)
	}
	if got := clampSplitLeft(50, 50); got != 49 {
		t.Fatalf("narrow full: got %d want 49", got)
	}
}

func TestSplitWidthsUsesRatio(t *testing.T) {
	m := &Model{listKeys: keybind.DefaultList(), width: 100, split: splitState{ratio: 0.4}}
	left, right := m.splitWidths()
	if left != 40 || right != 60 {
		t.Fatalf("splitWidths = %d,%d want 40,60", left, right)
	}
	// Default ratio when unset, floored by the minimum side.
	m.split.ratio = 0
	left, right = m.splitWidths()
	ratio := defaultSplitRatio
	wantLeft := clampSplitLeft(int(ratio*100), 100)
	if left != wantLeft || right != 100-wantLeft {
		t.Fatalf("default split = %d,%d want %d,%d", left, right, wantLeft, 100-wantLeft)
	}
}

func TestSetSplitFromXClampsAndUpdatesRatio(t *testing.T) {
	m := &Model{listKeys: keybind.DefaultList(), width: 100, split: splitState{ratio: defaultSplitRatio}}
	m.setSplitFromX(50)
	if m.split.ratio != 0.5 {
		t.Fatalf("ratio = %v want 0.5", m.split.ratio)
	}
	left, _ := m.splitWidths()
	if left != 50 {
		t.Fatalf("left = %d want 50", left)
	}
	m.setSplitFromX(5)
	left, right := m.splitWidths()
	if left != minSplitSide || right != 100-minSplitSide {
		t.Fatalf("clamped left split = %d,%d", left, right)
	}
}

func TestResizeModeKeyArmsDrag(t *testing.T) {
	m := &Model{listKeys: keybind.DefaultList(), mode: modeList, split: splitState{ratio: defaultSplitRatio}, width: 120, height: 40}
	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'|'}})
	m = updated.(*Model)
	if !m.split.resizeMode {
		t.Fatal("| should enter resize mode")
	}
	if cmd != nil {
		t.Fatal("enter should not toggle mouse reporting")
	}

	// Other keys are swallowed while armed.
	updated, cmd = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = updated.(*Model)
	if m.mode != modeList || !m.split.resizeMode {
		t.Fatal("resize mode should swallow n")
	}
	if cmd != nil {
		t.Fatal("swallowed key should return no cmd")
	}

	updated, cmd = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(*Model)
	if m.split.resizeMode {
		t.Fatal("esc should leave resize mode")
	}
	if cmd != nil {
		t.Fatal("exit should not toggle mouse reporting")
	}
}

func TestArrowNudgeAndPipeCommits(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	m := &Model{listKeys: keybind.DefaultList(),
		store:  st,
		mode:   modeList,
		width:  100,
		height: 40,
		split:  splitState{ratio: 0.34},
	}
	updated, _ := m.enterResizeMode()
	m = updated.(*Model)
	before, _ := m.splitWidths()
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	m = updated.(*Model)
	after, _ := m.splitWidths()
	if after != before+1 {
		t.Fatalf("right arrow left width = %d want %d", after, before+1)
	}
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	m = updated.(*Model)
	if left, _ := m.splitWidths(); left != before {
		t.Fatalf("left arrow should undo nudge, left=%d want %d", left, before)
	}
	// Nudge once more, then | commits.
	m.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'|'}})
	m = updated.(*Model)
	if m.split.resizeMode {
		t.Fatal("| should commit and exit resize mode")
	}
	if cmd != nil {
		t.Fatal("commit should not toggle mouse reporting")
	}
	raw, err := st.Setting(splitRatioSetting)
	if err != nil || raw == "" {
		t.Fatalf("committed ratio missing: %v %q", err, raw)
	}
	if left, _ := m.splitWidths(); left != before+1 {
		t.Fatalf("committed left = %d want %d", left, before+1)
	}
}

func TestEnterCommitsResize(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	m := &Model{listKeys: keybind.DefaultList(),
		store:  st,
		mode:   modeList,
		width:  100,
		height: 40,
		split:  splitState{ratio: 0.34},
	}
	updated, _ := m.enterResizeMode()
	m = updated.(*Model)
	m.nudgeSplit(8)

	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	if m.split.resizeMode || m.split.dragging {
		t.Fatal("enter should commit and leave resize mode")
	}
	if cmd != nil {
		t.Fatal("enter commit should not return a command")
	}
	if got := loadSplitRatio(st); got != 0.42 {
		t.Fatalf("reloaded ratio = %v want 0.42", got)
	}
}

func TestArrowCancelRestoresRatio(t *testing.T) {
	m := &Model{listKeys: keybind.DefaultList(), mode: modeList, width: 100, height: 40, split: splitState{ratio: 0.34}}
	updated, _ := m.enterResizeMode()
	m = updated.(*Model)
	m.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	m.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(*Model)
	if m.split.resizeMode {
		t.Fatal("esc should exit")
	}
	if left, _ := m.splitWidths(); left != 34 {
		t.Fatalf("esc should restore left=34, got %d", left)
	}
}

func TestQuitFromResizePersistsRatio(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	m := &Model{listKeys: keybind.DefaultList(),
		store:  st,
		mode:   modeList,
		width:  100,
		height: 40,
		split:  splitState{ratio: 0.34},
	}
	updated, _ := m.enterResizeMode()
	m = updated.(*Model)
	m.nudgeSplit(8)

	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	m = updated.(*Model)
	if m.split.resizeMode || m.split.dragging {
		t.Fatal("quit should clear resize state")
	}
	if cmd == nil {
		t.Fatal("quit should return a command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("quit command should produce tea.QuitMsg")
	}
	if got := loadSplitRatio(st); got != 0.42 {
		t.Fatalf("reloaded ratio = %v want 0.42", got)
	}
}

func TestDragReleasePersistsAndExits(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	m := &Model{listKeys: keybind.DefaultList(),
		store:  st,
		mode:   modeList,
		width:  100,
		height: 40,
		split:  splitState{ratio: defaultSplitRatio},
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
	m := &Model{listKeys: keybind.DefaultList(),
		mode:   modeList,
		width:  100,
		height: 40,
		split:  splitState{ratio: 0.34, resizeMode: true},
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

func TestEnterResizeBlockedWhenSearchingOrQuick(t *testing.T) {
	m := &Model{listKeys: keybind.DefaultList(), mode: modeList, width: 100, height: 40, split: splitState{ratio: defaultSplitRatio}, searching: true}
	updated, cmd := m.enterResizeMode()
	m = updated.(*Model)
	if m.split.resizeMode || cmd != nil {
		t.Fatal("searching should block resize mode")
	}
	m.searching = false
	m.quick.active = true
	updated, cmd = m.enterResizeMode()
	m = updated.(*Model)
	if m.split.resizeMode || cmd != nil {
		t.Fatal("quick prompt should block resize mode")
	}
}

func TestBodyYRangeMatchesListChrome(t *testing.T) {
	m := &Model{listKeys: keybind.DefaultList(), width: 120, height: 40, split: splitState{ratio: defaultSplitRatio}, mode: modeList}
	start, end := m.bodyYRange()
	if start != m.listChromeRows() {
		t.Fatalf("start = %d want listChromeRows=%d", start, m.listChromeRows())
	}
	// No transient status is showing, so its row is not reserved.
	wantH := m.height - m.listChromeRows() - 1 - lipgloss.Height(m.viewFooter())
	if wantH < 3 {
		wantH = 3
	}
	if m.listBodyHeight() != wantH {
		t.Fatalf("listBodyHeight = %d want %d", m.listBodyHeight(), wantH)
	}
	if end != m.listChromeRows()+wantH {
		t.Fatalf("end = %d want %d", end, m.listChromeRows()+wantH)
	}
}

func TestDragCancelRestoresRatio(t *testing.T) {
	m := &Model{listKeys: keybind.DefaultList(),
		mode:   modeList,
		width:  100,
		height: 40,
		split:  splitState{ratio: 0.34},
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
	m := &Model{listKeys: keybind.DefaultList(),
		mode:   modeList,
		width:  100,
		height: 40,
		split:  splitState{ratio: 0.34, resizeMode: true},
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
	for i, row := range m.railHits {
		if row >= 0 && !m.rows[row].isGroup && m.rows[row].sess.Name == name {
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
	for i, row := range m.railHits {
		if row >= 0 && m.rows[row].isGroup && m.rows[row].group == path {
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

func TestClickOnSelectedRowDoesNotFocus(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")

	line := paintedRailLines(t, m, "alpha")[0]
	y0, _ := m.bodyYRange()
	updated, _ := m.handleMouse(tea.MouseMsg{
		X: 2, Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	if m.mode != modeList {
		t.Fatalf("one click on the selected session should not focus, mode = %v", m.mode)
	}
}

func TestClickOnListLeavesFocus(t *testing.T) {
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
	updated, _ = m.handleMouse(tea.MouseMsg{
		X: 2, Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	if m.mode != modeList {
		t.Fatalf("click on the list should leave focus, mode = %v", m.mode)
	}
	if sess, ok := m.selected(); !ok || sess.Name != "beta" {
		t.Fatalf("click should select beta, got %q ok=%v", sess.Name, ok)
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
	updated, _ = m.handleMouse(tea.MouseMsg{
		X: 2, Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
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
	if !m.pane.box.ok {
		t.Fatal("test setup: focused pane has no box")
	}
	updated, _ = m.handleMouse(tea.MouseMsg{
		X: m.pane.box.x, Y: m.pane.box.y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	if m.mode != modeFocus {
		t.Fatalf("click in the pane should stay focused, mode = %v", m.mode)
	}
}

// The pointer names the row, not the cursor: a wheel notch, a j or the
// poll can walk the cursor away between the two presses.
func TestDoubleClickFocusesTheRowUnderThePointer(t *testing.T) {
	m := buildModel(t)
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
	dir := t.TempDir()
	for _, group := range []string{"aaa", "zzz"} {
		if err := m.store.CreateGroup(group, dir); err != nil {
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
	before := m.cursor

	// Folding the group above target drops every row under it an index.
	m.collapsed["aaa"] = true
	m.rebuildRows()
	line = paintedRailLines(t, m, "target")[0]
	m.selectSessionRow(t, "target")
	if m.cursor == before {
		t.Fatal("test setup: folding should have renumbered target's row")
	}
	m.cursor = before

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
			m.preview = tc.preview
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

	m.fullLayout = true
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
	m.focusOnEnter = false
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
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")

	line := paintedRailLines(t, m, "alpha")[0]
	y0, _ := m.bodyYRange()
	press := tea.MouseMsg{X: 2, Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	updated, _ := m.handleMouse(press)
	m = updated.(*Model)
	m.listClickAt = time.Now().Add(-multiClickWindow - time.Millisecond)
	updated, _ = m.handleMouse(press)
	m = updated.(*Model)
	if m.mode != modeList {
		t.Fatalf("a slow second click should not focus, mode = %v", m.mode)
	}
}

func TestClickOnSelectedGroupTogglesCollapse(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("work", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "alpha", dir, "work")
	m.selectGroupRow(t, "work")
	if m.collapsed["work"] {
		t.Fatal("test setup: work should start open")
	}

	line := paintedGroupLines(t, m, "work")[0]
	y0, _ := m.bodyYRange()
	press := tea.MouseMsg{X: 2, Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	updated, _ := m.handleMouse(press)
	m = updated.(*Model)
	updated, _ = m.handleMouse(press)
	m = updated.(*Model)
	if !m.collapsed["work"] {
		t.Fatal("double click on the selected group should fold it")
	}

	line = paintedGroupLines(t, m, "work")[0]
	y0, _ = m.bodyYRange()
	press.Y = y0 + line
	updated, _ = m.handleMouse(press)
	m = updated.(*Model)
	updated, _ = m.handleMouse(press)
	m = updated.(*Model)
	if m.collapsed["work"] {
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
	before := m.cursor

	m.View()
	y0, _ := m.bodyYRange()
	updated, cmd := m.handleMouse(tea.MouseMsg{
		X: m.dividerX() + 5, Y: y0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	if m.cursor != before || cmd != nil {
		t.Fatal("a click in the content column should not move the cursor")
	}
}

// A press directly on the divider arms the drag on the spot: it must not
// need `|` pressed first. It arms dragging alone, leaving resizeMode — the
// keyboard's own gate — off, and it moves nothing until the pointer does.
func TestDividerPressArmsDragWithoutResizeMode(t *testing.T) {
	m := &Model{listKeys: keybind.DefaultList(),
		mode:   modeList,
		width:  100,
		height: 40,
		split:  splitState{ratio: defaultSplitRatio},
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

	m := &Model{listKeys: keybind.DefaultList(),
		store:  st,
		mode:   modeList,
		width:  100,
		height: 40,
		split:  splitState{ratio: defaultSplitRatio},
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
	before := m.cursor

	m.View()
	updated, _ := m.enterResizeMode()
	m = updated.(*Model)
	y0, _ := m.bodyYRange()
	updated, _ = m.handleMouse(tea.MouseMsg{
		X: 2, Y: y0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	if m.cursor != before {
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
	m.fullLayout = true

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
	m.fullLayout = true

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
	m.comfortableRows = true
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
	for i, row := range m.railHits {
		if row >= 0 && strings.Contains(frame[y0+i], "more") {
			counter, target = i, row
		}
	}
	if counter < 0 {
		t.Fatal("test setup: a short rail should paint a counter")
	}
	for i, row := range m.railHits {
		if i != counter && row == target {
			t.Fatalf("test setup: row %d is painted at line %d, so it is not hidden", target, i)
		}
	}

	updated, cmd := m.handleMouse(tea.MouseMsg{
		X: 2, Y: y0 + counter, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	if m.cursor != target {
		t.Fatalf("clicking the counter should select row %d, cursor = %d", target, m.cursor)
	}
	if cmd == nil {
		t.Fatal("selecting a different row should schedule a preview")
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
	m.searching = true
	before := m.cursor
	m.View()

	line := -1
	for i, row := range m.railHits {
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
	if m.cursor != before || cmd != nil {
		t.Fatal("clicking a chrome line should not move the cursor")
	}
}

func TestNewLoadsPersistedSplitRatio(t *testing.T) {
	m := buildModel(t)
	if err := m.store.SetSetting(splitRatioSetting, "0.45"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	loaded := New(m.cfg, m.store, m.tmux, m.poller.engine, m.hooks, "dev")
	if loaded.split.ratio != 0.45 {
		t.Fatalf("New splitRatio = %v want 0.45", loaded.split.ratio)
	}
}

// Wheel events must be consumed by the app so the host terminal cannot
// scroll the TUI away, and in the list moves the session cursor the same
// way an arrow key would (#110).
func TestWheelMovesListCursor(t *testing.T) {
	m := &Model{listKeys: keybind.DefaultList(),
		mode:   modeList,
		cursor: 0,
		rows:   []treeRow{{}, {}},
		width:  80,
		height: 24,
	}
	updated, cmd := m.handleMouse(tea.MouseMsg{
		Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress,
	})
	m = updated.(*Model)
	if m.cursor != 1 {
		t.Fatalf("wheel down: cursor = %d want 1", m.cursor)
	}
	if cmd == nil {
		t.Fatal("wheel down should schedule the preview settle like moveCursor")
	}
	updated, _ = m.handleMouse(tea.MouseMsg{
		Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress,
	})
	m = updated.(*Model)
	if m.cursor != 0 {
		t.Fatalf("wheel up: cursor = %d want 0", m.cursor)
	}
}

// The wheel keeps moving the selection while the search field or the quick
// prompt is open. Search otherwise narrows the list to rows none of the
// pointer or the keyboard can reach, and the quick bar retargets on up/down
// the way its own footer advertises.
func TestWheelMovesCursorWhileSearchingOrPrompting(t *testing.T) {
	for name, m := range map[string]*Model{
		"searching": {mode: modeList, searching: true, cursor: 0, rows: []treeRow{{}, {}}, width: 80, height: 24},
		"quick bar": {mode: modeList, quick: quickState{active: true}, cursor: 0, rows: []treeRow{{}, {}}, width: 80, height: 24},
	} {
		t.Run(name, func(t *testing.T) {
			updated, _ := m.handleMouse(tea.MouseMsg{
				Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress,
			})
			m := updated.(*Model)
			if m.cursor != 1 {
				t.Fatalf("wheel should have moved the cursor to 1, got %d", m.cursor)
			}
		})
	}
}

// A click retargets the quick prompt the same way its up/down do. Search
// keeps click-to-select for the same reason it keeps the wheel.
func TestClickSelectsRowWhileSearchingOrPrompting(t *testing.T) {
	for _, name := range []string{"searching", "quick bar"} {
		t.Run(name, func(t *testing.T) {
			m := buildModel(t)
			createSession(t, m, "alpha", t.TempDir(), "")
			createSession(t, m, "beta", t.TempDir(), "")
			m.selectSessionRow(t, "beta")
			if name == "searching" {
				m.searching = true
			} else {
				m.openQuickMode()
			}

			line := paintedRailLines(t, m, "alpha")[0]
			y0, _ := m.bodyYRange()
			updated, _ := m.handleMouse(tea.MouseMsg{
				X: 2, Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
			})
			m = updated.(*Model)
			if sess, ok := m.selected(); !ok || sess.Name != "alpha" {
				t.Fatalf("click should select alpha, got %q ok=%v", sess.Name, ok)
			}
		})
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
	if m.mode != modeList {
		t.Fatalf("the click that leaves focus must not focus again, mode = %v", m.mode)
	}
}

// A press those surfaces swallow opens no run, so the first press after
// they close is a plain select rather than the second half of a pair.
func TestClickWhileSearchingOpensNoClickRun(t *testing.T) {
	for _, name := range []string{"searching", "quick bar"} {
		t.Run(name, func(t *testing.T) {
			m := buildModel(t)
			createSession(t, m, "alpha", t.TempDir(), "")
			m.selectSessionRow(t, "alpha")
			if name == "searching" {
				m.searching = true
			} else {
				m.openQuickMode()
			}

			line := paintedRailLines(t, m, "alpha")[0]
			y0, _ := m.bodyYRange()
			press := tea.MouseMsg{X: 2, Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
			updated, _ := m.handleMouse(press)
			m = updated.(*Model)

			m.searching, m.quick.active = false, false
			updated, _ = m.handleMouse(press)
			m = updated.(*Model)
			if m.mode != modeList {
				t.Fatalf("one press after %s closes must not focus, mode = %v", name, m.mode)
			}
		})
	}
}

// Search and the quick bar own Enter, so a double click stays a select:
// it must not steal the key those surfaces are waiting for.
func TestDoubleClickDoesNotFocusWhileSearchingOrPrompting(t *testing.T) {
	for _, name := range []string{"searching", "quick bar"} {
		t.Run(name, func(t *testing.T) {
			m := buildModel(t)
			createSession(t, m, "alpha", t.TempDir(), "")
			m.selectSessionRow(t, "alpha")
			if name == "searching" {
				m.searching = true
			} else {
				m.openQuickMode()
			}

			line := paintedRailLines(t, m, "alpha")[0]
			y0, _ := m.bodyYRange()
			press := tea.MouseMsg{X: 2, Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
			updated, _ := m.handleMouse(press)
			m = updated.(*Model)
			updated, _ = m.handleMouse(press)
			m = updated.(*Model)
			if m.mode != modeList {
				t.Fatalf("double click must not focus while %s, mode = %v", name, m.mode)
			}
			if sess, ok := m.selected(); !ok || sess.Name != "alpha" {
				t.Fatalf("selection should stay on alpha, got %q ok=%v", sess.Name, ok)
			}
		})
	}
}

// Neither does a press on the divider arm a drag while they are open: the
// mouse arms resize under the same conditions enterResizeMode does.
func TestDividerPressBlockedWhileSearchingOrPrompting(t *testing.T) {
	for name, m := range map[string]*Model{
		"searching": {listKeys: keybind.DefaultList(), mode: modeList, searching: true, width: 100, height: 40, split: splitState{ratio: defaultSplitRatio}},
		"quick bar": {listKeys: keybind.DefaultList(), mode: modeList, quick: quickState{active: true}, width: 100, height: 40, split: splitState{ratio: defaultSplitRatio}},
	} {
		t.Run(name, func(t *testing.T) {
			y0, _ := m.bodyYRange()
			updated, _ := m.handleMouse(tea.MouseMsg{
				X: m.dividerX(), Y: y0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
			})
			m := updated.(*Model)
			if m.split.dragging {
				t.Fatal("the divider should be held while the field owns the rail")
			}
		})
	}
}

// One flick of the wheel is several notches, so the wheel clamps where the
// keyboard wraps: going off the end must not fling the selection to the
// far one and aim every key after it somewhere the user never looked.
func TestWheelClampsAtBothEnds(t *testing.T) {
	m := &Model{listKeys: keybind.DefaultList(), mode: modeList, cursor: 2, rows: []treeRow{{}, {}, {}}, width: 80, height: 24}
	for i := 0; i < 3; i++ {
		updated, _ := m.handleMouse(tea.MouseMsg{
			Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress,
		})
		m = updated.(*Model)
	}
	if m.cursor != 2 {
		t.Fatalf("wheel down off the bottom: cursor = %d want it held at 2", m.cursor)
	}
	m.cursor = 0
	for i := 0; i < 3; i++ {
		updated, _ := m.handleMouse(tea.MouseMsg{
			Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress,
		})
		m = updated.(*Model)
	}
	if m.cursor != 0 {
		t.Fatalf("wheel up off the top: cursor = %d want it held at 0", m.cursor)
	}
	// The keyboard still wraps: clamping is the wheel's alone.
	m.moveCursor(-1)
	if m.cursor != 2 {
		t.Fatalf("arrow up should still wrap to 2, cursor = %d", m.cursor)
	}
}

func TestWheelSwallowedInResizeMode(t *testing.T) {
	m := &Model{listKeys: keybind.DefaultList(),
		mode:   modeList,
		split:  splitState{resizeMode: true},
		cursor: 0,
		rows:   []treeRow{{}, {}},
		width:  80,
		height: 24,
	}
	updated, _ := m.handleMouse(tea.MouseMsg{
		Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress,
	})
	m = updated.(*Model)
	if m.cursor != 0 {
		t.Fatalf("resize mode should swallow wheel, cursor = %d", m.cursor)
	}
}
