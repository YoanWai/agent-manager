package ui

import (
	"path/filepath"
	"strconv"
	"testing"

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
	m := &Model{width: 100, splitRatio: 0.4}
	left, right := m.splitWidths()
	if left != 40 || right != 60 {
		t.Fatalf("splitWidths = %d,%d want 40,60", left, right)
	}
	// Default ratio when unset.
	m.splitRatio = 0
	left, right = m.splitWidths()
	if left != 34 || right != 66 {
		t.Fatalf("default split = %d,%d want 34,66", left, right)
	}
}

func TestSetSplitFromXClampsAndUpdatesRatio(t *testing.T) {
	m := &Model{width: 100, splitRatio: defaultSplitRatio}
	m.setSplitFromX(50)
	if m.splitRatio != 0.5 {
		t.Fatalf("ratio = %v want 0.5", m.splitRatio)
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
	m := &Model{mode: modeList, splitRatio: defaultSplitRatio, width: 120, height: 40}
	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'|'}})
	m = updated.(*Model)
	if !m.resizeMode {
		t.Fatal("| should enter resize mode")
	}
	if cmd != nil {
		// Mouse is always on at the program level; resize only flips a flag.
		t.Fatal("enter should not toggle mouse reporting")
	}

	// Other keys are swallowed while armed.
	updated, cmd = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = updated.(*Model)
	if m.mode != modeList || !m.resizeMode {
		t.Fatal("resize mode should swallow n")
	}
	if cmd != nil {
		t.Fatal("swallowed key should return no cmd")
	}

	updated, cmd = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(*Model)
	if m.resizeMode {
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

	m := &Model{
		store:      st,
		mode:       modeList,
		width:      100,
		height:     40,
		splitRatio: 0.34,
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
	if m.resizeMode {
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

func TestArrowCancelRestoresRatio(t *testing.T) {
	m := &Model{mode: modeList, width: 100, height: 40, splitRatio: 0.34}
	updated, _ := m.enterResizeMode()
	m = updated.(*Model)
	m.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	m.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(*Model)
	if m.resizeMode {
		t.Fatal("esc should exit")
	}
	if left, _ := m.splitWidths(); left != 34 {
		t.Fatalf("esc should restore left=34, got %d", left)
	}
}

func TestDragReleasePersistsAndExits(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	m := &Model{
		store:      st,
		mode:       modeList,
		width:      100,
		height:     40,
		splitRatio: defaultSplitRatio,
	}
	updated, _ := m.enterResizeMode()
	m = updated.(*Model)

	div := m.dividerX()
	// Body starts at listChromeRows; any y inside the body range works.
	updated, _ = m.handleMouse(tea.MouseMsg{
		X: div, Y: 5, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	if !m.splitDragging {
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
	if m.splitDragging || m.resizeMode {
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
	m.splitRatio = defaultSplitRatio
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
		mode:       modeList,
		width:      100,
		height:     40,
		splitRatio: 0.34,
		resizeMode: true,
	}
	div := m.dividerX()
	updated, _ := m.handleMouse(tea.MouseMsg{
		X: div, Y: 0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	if m.splitDragging {
		t.Fatal("press on header row must not start drag")
	}
	y0, y1 := m.bodyYRange()
	if y0 != listChromeRows {
		t.Fatalf("body start = %d want %d", y0, listChromeRows)
	}
	updated, _ = m.handleMouse(tea.MouseMsg{
		X: div, Y: y1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	if m.splitDragging {
		t.Fatal("press on exclusive body end must not start drag")
	}
}

func TestEnterResizeBlockedWhenSearchingOrQuick(t *testing.T) {
	m := &Model{mode: modeList, width: 100, height: 40, splitRatio: defaultSplitRatio, searching: true}
	updated, cmd := m.enterResizeMode()
	m = updated.(*Model)
	if m.resizeMode || cmd != nil {
		t.Fatal("searching should block resize mode")
	}
	m.searching = false
	m.quick.active = true
	updated, cmd = m.enterResizeMode()
	m = updated.(*Model)
	if m.resizeMode || cmd != nil {
		t.Fatal("quick prompt should block resize mode")
	}
}

func TestBodyYRangeMatchesListChrome(t *testing.T) {
	m := &Model{width: 120, height: 40, splitRatio: defaultSplitRatio, mode: modeList}
	start, end := m.bodyYRange()
	if start != listChromeRows {
		t.Fatalf("start = %d want listChromeRows=%d", start, listChromeRows)
	}
	wantH := m.height - 4 - lipgloss.Height(m.viewFooter())
	if wantH < 3 {
		wantH = 3
	}
	if m.listBodyHeight() != wantH {
		t.Fatalf("listBodyHeight = %d want %d", m.listBodyHeight(), wantH)
	}
	if end != listChromeRows+wantH {
		t.Fatalf("end = %d want %d", end, listChromeRows+wantH)
	}
}

func TestDragCancelRestoresRatio(t *testing.T) {
	m := &Model{
		mode:       modeList,
		width:      100,
		height:     40,
		splitRatio: 0.34,
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
	if m.resizeMode || m.splitDragging {
		t.Fatal("cancel should clear resize state")
	}
	if left, _ := m.splitWidths(); left != 34 {
		t.Fatalf("cancel should restore left=34, got %d", left)
	}
}

func TestPressOffDividerDoesNotDrag(t *testing.T) {
	m := &Model{
		mode:       modeList,
		width:      100,
		height:     40,
		splitRatio: 0.34,
		resizeMode: true,
	}
	updated, _ := m.handleMouse(tea.MouseMsg{
		X: 5, Y: 5, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	m = updated.(*Model)
	if m.splitDragging {
		t.Fatal("press far from divider should not start drag")
	}
}

func TestNewLoadsPersistedSplitRatio(t *testing.T) {
	m := buildModel(t)
	if err := m.store.SetSetting(splitRatioSetting, "0.45"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	loaded := New(m.cfg, m.store, m.tmux, m.poller.engine, m.hooks, "dev")
	if loaded.splitRatio != 0.45 {
		t.Fatalf("New splitRatio = %v want 0.45", loaded.splitRatio)
	}
}

// Wheel events must be consumed by the app (so the host terminal cannot
// scroll the TUI away) and advance the list cursor outside resize mode.
func TestWheelScrollMovesListCursor(t *testing.T) {
	m := &Model{
		mode:   modeList,
		cursor: 0,
		rows:   []treeRow{{}, {}},
		width:  80,
		height: 24,
	}
	updated, _ := m.handleMouse(tea.MouseMsg{
		Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress,
	})
	m = updated.(*Model)
	if m.cursor != 1 {
		t.Fatalf("wheel down cursor = %d want 1", m.cursor)
	}
	updated, _ = m.handleMouse(tea.MouseMsg{
		Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress,
	})
	m = updated.(*Model)
	if m.cursor != 0 {
		t.Fatalf("wheel up cursor = %d want 0", m.cursor)
	}
}

func TestWheelSwallowedInResizeMode(t *testing.T) {
	m := &Model{
		mode:       modeList,
		resizeMode: true,
		cursor:     0,
		rows:       []treeRow{{}, {}},
		width:      80,
		height:     24,
	}
	updated, _ := m.handleMouse(tea.MouseMsg{
		Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress,
	})
	m = updated.(*Model)
	if m.cursor != 0 {
		t.Fatalf("resize mode should swallow wheel, cursor = %d", m.cursor)
	}
}
