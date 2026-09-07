package ui

import (
	"github.com/YoanWai/agent-manager/internal/keybind"
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

// A click on a session row selects it, reading the geometry entryLines
// recorded while painting rather than re-deriving it (#110).
func TestClickSelectsRow(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "beta")

	railWidth, _ := m.splitWidths()
	m.railLines(railWidth-1, m.listBodyHeight())

	line := -1
	for i, row := range m.railHits {
		if row >= 0 && m.rows[row].sess.Name == "alpha" {
			line = i
			break
		}
	}
	if line < 0 {
		t.Fatal("test setup: alpha's row not found in railHits")
	}
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
}

// A click past the divider, in the content column, must not steal the
// selection: the rail is what click-to-select owns.
func TestClickInContentColumnDoesNotSelect(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "beta")
	before := m.cursor

	railWidth, _ := m.splitWidths()
	m.railLines(railWidth-1, m.listBodyHeight())
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
// need `|` pressed first.
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
	if !m.split.dragging || !m.split.resizeMode {
		t.Fatal("a press on the divider should arm the drag on its own")
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

// A click that misses the divider while resize mode is armed from the
// keyboard must not fall through to row selection: resize mode owns every
// press until it exits.
func TestPressOffDividerWhileArmedDoesNotSelectRow(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "beta")
	before := m.cursor

	railWidth, _ := m.splitWidths()
	m.railLines(railWidth-1, m.listBodyHeight())
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
	m.View()

	line := -1
	for i, row := range m.railHits {
		if row >= 0 && m.rows[row].sess.Name == "alpha" {
			line = i
			break
		}
	}
	if line < 0 {
		t.Fatal("test setup: alpha's row not found in railHits")
	}
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

// A comfortable entry paints two or three lines; a click on any of them
// should select the entry, not whatever railHits index that physical line
// would be under a compact row.
func TestClickSelectsRowAcrossComfortableLines(t *testing.T) {
	m := buildModel(t)
	m.comfortableRows = true
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")

	railWidth, _ := m.splitWidths()
	m.railLines(railWidth-1, m.listBodyHeight())

	var alphaLines []int
	for i, row := range m.railHits {
		if row >= 0 && m.rows[row].sess.Name == "alpha" {
			alphaLines = append(alphaLines, i)
		}
	}
	if len(alphaLines) < 2 {
		t.Fatalf("test setup: comfortable alpha should paint 2+ lines, got %d", len(alphaLines))
	}
	m.selectSessionRow(t, "beta")
	m.railLines(railWidth-1, m.listBodyHeight())

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

// The "N more" scroll indicators are chrome, not rows: a click there picks
// nothing rather than misattributing to whichever row happens to sit at
// that index.
func TestClickOnMoreIndicatorDoesNotSelect(t *testing.T) {
	m := buildModel(t)
	for _, name := range []string{"one", "two", "three", "four", "five"} {
		createSession(t, m, name, t.TempDir(), "")
	}
	m.selectSessionRow(t, "one")
	before := m.cursor

	railWidth, _ := m.splitWidths()
	m.railLines(railWidth-1, 3)

	line := -1
	for i, row := range m.railHits {
		if row < 0 {
			line = i
			break
		}
	}
	if line < 0 {
		t.Fatal("test setup: no chrome line found with a short rail")
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

// The wheel still does nothing while the search field or the quick prompt
// is capturing input: those own the keyboard's up/down too.
func TestWheelSwallowedWhileTypingInList(t *testing.T) {
	for name, m := range map[string]*Model{
		"searching": {mode: modeList, searching: true, cursor: 0, rows: []treeRow{{}, {}}, width: 80, height: 24},
		"quick bar": {mode: modeList, quick: quickState{active: true}, cursor: 0, rows: []treeRow{{}, {}}, width: 80, height: 24},
	} {
		t.Run(name, func(t *testing.T) {
			updated, cmd := m.handleMouse(tea.MouseMsg{
				Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress,
			})
			m := updated.(*Model)
			if m.cursor != 0 {
				t.Fatalf("wheel moved the cursor to %d", m.cursor)
			}
			if cmd != nil {
				t.Fatal("wheel scheduled work")
			}
		})
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
