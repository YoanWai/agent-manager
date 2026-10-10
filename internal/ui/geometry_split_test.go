package ui

import (
	"errors"
	"testing"

	"github.com/YoanWai/agent-manager/internal/keybind"
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
	m := &Model{layout: layoutState{width: 100, split: splitState{ratio: 0.4}}, services: services{listKeys: keybind.DefaultList()}}
	left, right := m.splitWidths()
	if left != 40 || right != 60 {
		t.Fatalf("splitWidths = %d,%d want 40,60", left, right)
	}
	// Default ratio when unset, floored by the minimum side.
	m.layout.split.ratio = 0
	left, right = m.splitWidths()
	ratio := defaultSplitRatio
	wantLeft := clampSplitLeft(int(ratio*100), 100)
	if left != wantLeft || right != 100-wantLeft {
		t.Fatalf("default split = %d,%d want %d,%d", left, right, wantLeft, 100-wantLeft)
	}
}

func TestSetSplitFromXClampsAndUpdatesRatio(t *testing.T) {
	m := &Model{layout: layoutState{width: 100, split: splitState{ratio: defaultSplitRatio}}, services: services{listKeys: keybind.DefaultList()}}
	m.setSplitFromX(50)
	if m.layout.split.ratio != 0.5 {
		t.Fatalf("ratio = %v want 0.5", m.layout.split.ratio)
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
	m := &Model{mode: modeList, layout: layoutState{split: splitState{ratio: defaultSplitRatio}, width: 120, height: 40}, services: services{listKeys: keybind.DefaultList()}}
	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'|'}})
	m.drainEffects(t)
	m = updated.(*Model)
	if !m.layout.split.resizeMode {
		t.Fatal("| should enter resize mode")
	}
	if cmd != nil {
		t.Fatal("enter should not toggle mouse reporting")
	}

	// Other keys are swallowed while armed.
	updated, cmd = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m.drainEffects(t)
	m = updated.(*Model)
	if m.mode != modeList || !m.layout.split.resizeMode {
		t.Fatal("resize mode should swallow n")
	}
	if cmd != nil {
		t.Fatal("swallowed key should return no cmd")
	}

	updated, cmd = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	m.drainEffects(t)
	m = updated.(*Model)
	if m.layout.split.resizeMode {
		t.Fatal("esc should leave resize mode")
	}
	if cmd != nil {
		t.Fatal("exit should not toggle mouse reporting")
	}
}

func TestArrowNudgeAndPipeCommits(t *testing.T) {
	m := buildModel(t)
	st := m.services.store
	m.layout.width, m.layout.height, m.layout.split.ratio = 100, 40, 0.34
	updated, _ := m.enterResizeMode()
	m = updated.(*Model)
	before, _ := m.splitWidths()
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	m.drainEffects(t)
	m = updated.(*Model)
	after, _ := m.splitWidths()
	if after != before+1 {
		t.Fatalf("right arrow left width = %d want %d", after, before+1)
	}
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	m.drainEffects(t)
	m = updated.(*Model)
	if left, _ := m.splitWidths(); left != before {
		t.Fatalf("left arrow should undo nudge, left=%d want %d", left, before)
	}
	// Nudge once more, then | commits.
	m.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	m.drainEffects(t)
	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'|'}})
	m.drainEffects(t)
	m = updated.(*Model)
	if m.layout.split.resizeMode {
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
	m := buildModel(t)
	st := m.services.store
	m.layout.width, m.layout.height, m.layout.split.ratio = 100, 40, 0.34
	updated, _ := m.enterResizeMode()
	m = updated.(*Model)
	m.nudgeSplit(8)

	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.drainEffects(t)
	m = updated.(*Model)
	if m.layout.split.resizeMode || m.layout.split.dragging {
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
	m := &Model{mode: modeList, layout: layoutState{width: 100, height: 40, split: splitState{ratio: 0.34}}, services: services{listKeys: keybind.DefaultList()}}
	updated, _ := m.enterResizeMode()
	m = updated.(*Model)
	m.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	m.drainEffects(t)
	m.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	m.drainEffects(t)
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	m.drainEffects(t)
	m = updated.(*Model)
	if m.layout.split.resizeMode {
		t.Fatal("esc should exit")
	}
	if left, _ := m.splitWidths(); left != 34 {
		t.Fatalf("esc should restore left=34, got %d", left)
	}
}

func TestQuitFromResizePersistsRatio(t *testing.T) {
	m := buildModel(t)
	st := m.services.store
	m.layout.width, m.layout.height, m.layout.split.ratio = 100, 40, 0.34
	updated, _ := m.enterResizeMode()
	m = updated.(*Model)
	m.nudgeSplit(8)

	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	m = updated.(*Model)
	if m.layout.split.resizeMode || m.layout.split.dragging {
		t.Fatal("quit should clear resize state")
	}
	if cmd == nil {
		t.Fatal("quit should start the accepted split save")
	}
	m.applyCmd(t, cmd)
	if quit := m.nextEffectCmd(); quit == nil {
		t.Fatal("quit should complete after the accepted split save")
	} else if _, ok := quit().(tea.QuitMsg); !ok {
		t.Fatalf("after save, quit command produced %T", quit())
	}
	if got := loadSplitRatio(st); got != 0.42 {
		t.Fatalf("reloaded ratio = %v want 0.42", got)
	}
}

func TestCtrlCFromResizePersistsRatioAfterAcceptedQuit(t *testing.T) {
	m := buildModel(t)
	st := m.services.store
	m.layout.width, m.layout.height, m.layout.split.ratio = 100, 40, 0.34
	updated, _ := m.enterResizeMode()
	m = updated.(*Model)
	m.nudgeSplit(8)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = updated.(*Model)
	if m.layout.split.resizeMode || m.layout.split.dragging || m.layout.split.moved {
		t.Fatal("accepted ctrl+c quit left resize interaction armed")
	}
	if !m.effects.quitting || cmd == nil {
		t.Fatal("accepted ctrl+c quit did not drain the captured split save")
	}
	m.applyCmd(t, cmd)
	if got := loadSplitRatio(st); got != 0.42 {
		t.Fatalf("reloaded ratio = %v want 0.42", got)
	}
}

func TestInstallerRefusedQuitKeepsResizeInteraction(t *testing.T) {
	m := buildModel(t)
	m.layout.width, m.layout.height, m.layout.split.ratio = 100, 40, 0.34
	updated, _ := m.enterResizeMode()
	m = updated.(*Model)
	m.nudgeSplit(8)
	m.launchHint.install = &pendingInstall{name: "install-test"}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = updated.(*Model)
	if cmd != nil || m.effects.quitting {
		t.Fatal("installer gate accepted quit")
	}
	if !m.layout.split.resizeMode || m.layout.split.ratio != 0.42 {
		t.Fatalf("refused quit changed resize interaction: mode=%v ratio=%v", m.layout.split.resizeMode, m.layout.split.ratio)
	}
	if raw, err := m.services.store.Setting(splitRatioSetting); err != nil || raw != "" {
		t.Fatalf("refused quit persisted ratio %q (err %v)", raw, err)
	}
}

func TestInstallerRefusedQuitKeepsMouseDrag(t *testing.T) {
	m := buildModel(t)
	m.layout.width, m.layout.height, m.layout.split.ratio = 100, 40, 0.42
	m.layout.split.dragging = true
	m.layout.split.moved = true
	m.launchHint.install = &pendingInstall{name: "install-test"}

	updated, cmd := m.handleKey(key("q"))
	m = updated.(*Model)
	if cmd != nil || m.effects.quitting {
		t.Fatal("installer gate accepted quit")
	}
	if !m.layout.split.dragging || !m.layout.split.moved || m.layout.split.ratio != 0.42 {
		t.Fatalf("refused quit changed mouse drag: dragging=%v moved=%v ratio=%v", m.layout.split.dragging, m.layout.split.moved, m.layout.split.ratio)
	}
	if raw, err := m.services.store.Setting(splitRatioSetting); err != nil || raw != "" {
		t.Fatalf("refused quit persisted ratio %q (err %v)", raw, err)
	}
}

func TestSplitPersistenceIsDeferredAndOrdered(t *testing.T) {
	m := buildModel(t)
	st := m.services.store

	m.layout.split.ratio = 0.41
	m.persistSplitRatio()
	m.layout.split.ratio = 0.52
	m.persistSplitRatio()
	if raw, err := st.Setting(splitRatioSetting); err != nil || raw != "" {
		t.Fatalf("Update path wrote split ratio before the worker ran: %q, %v", raw, err)
	}
	if len(m.effects.main.pending) != 2 {
		t.Fatalf("queued saves = %d, want 2", len(m.effects.main.pending))
	}
	first := m.effects.main.pending[0].request.(splitSaveRequest)
	second := m.effects.main.pending[1].request.(splitSaveRequest)
	if first.value != "0.4100" || second.value != "0.5200" {
		t.Fatalf("captured order = %q then %q", first.value, second.value)
	}

	m.drainEffects(t)
	if got := loadSplitRatio(st); got != 0.52 {
		t.Fatalf("reloaded ratio = %v, want latest accepted 0.52", got)
	}
}

func TestNewerSplitSaveSuccessClearsOnlySplitFailure(t *testing.T) {
	m := &Model{layout: layoutState{split: splitState{saveID: 1}}}
	m.applySplitSave(&effectJob{id: 1}, errors.New("save split ratio: database is locked"))
	if m.errBar.text == "" {
		t.Fatal("latest failed save did not surface its error")
	}

	m.layout.split.saveID = 2
	m.applySplitSave(&effectJob{id: 2}, nil)
	if m.errBar.text != "" || m.layout.split.saveError != "" {
		t.Fatalf("newer successful save left stale warning %q", m.errBar.text)
	}

	m.errBar.text = "another operation failed"
	m.layout.split.saveID = 4
	m.applySplitSave(&effectJob{id: 3}, errors.New("older split failure"))
	m.applySplitSave(&effectJob{id: 4}, nil)
	if m.errBar.text != "another operation failed" {
		t.Fatalf("split completion erased unrelated status %q", m.errBar.text)
	}
}

func TestEnterResizeBlockedWhenSearchingOrQuick(t *testing.T) {
	m := &Model{mode: modeList, layout: layoutState{width: 100, height: 40, split: splitState{ratio: defaultSplitRatio}}, services: services{listKeys: keybind.DefaultList()}, rail: railModelSearching(nil, 0, "")}
	updated, cmd := m.enterResizeMode()
	m = updated.(*Model)
	if m.layout.split.resizeMode || cmd != nil {
		t.Fatal("searching should block resize mode")
	}
	m.rail.SetSearch(m.rail.Search(), false)
	m.quick.active = true
	updated, cmd = m.enterResizeMode()
	m = updated.(*Model)
	if m.layout.split.resizeMode || cmd != nil {
		t.Fatal("quick prompt should block resize mode")
	}
}

func TestBodyYRangeMatchesListChrome(t *testing.T) {
	m := &Model{layout: layoutState{width: 120, height: 40, split: splitState{ratio: defaultSplitRatio}}, mode: modeList, services: services{listKeys: keybind.DefaultList()}}
	start, end := m.bodyYRange()
	if start != m.listChromeRows() {
		t.Fatalf("start = %d want listChromeRows=%d", start, m.listChromeRows())
	}
	// No transient status is showing, so its row is not reserved.
	wantH := m.layout.height - m.listChromeRows() - 1 - lipgloss.Height(m.viewFooter())
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

func TestNewLoadsPersistedSplitRatio(t *testing.T) {
	m := buildModel(t)
	if err := m.services.store.SetSetting(splitRatioSetting, "0.45"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	loaded := reloadModel(t, m)
	if loaded.layout.split.ratio != 0.45 {
		t.Fatalf("New splitRatio = %v want 0.45", loaded.layout.split.ratio)
	}
}
