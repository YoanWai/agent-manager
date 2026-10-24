package ui

import (
	"errors"
	"testing"

	"github.com/YoanWai/agent-manager/internal/keybind"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uiinstall "github.com/YoanWai/agent-manager/internal/ui/launchhint/install"
	tea "github.com/charmbracelet/bubbletea"
)

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
	m := &Model{layout: layoutState{split: splitState{ratio: defaultSplitRatio}, width: 120, height: 40}, services: services{listKeys: keybind.DefaultList()}}
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
	if !m.onList() || !m.layout.split.resizeMode {
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
	t.Parallel()
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
	t.Parallel()
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
	m := &Model{layout: layoutState{width: 100, height: 40, split: splitState{ratio: 0.34}}, services: services{listKeys: keybind.DefaultList()}}
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
	t.Parallel()
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
	t.Parallel()
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
	if !m.effects.Quitting || cmd == nil {
		t.Fatal("accepted ctrl+c quit did not drain the captured split save")
	}
	m.applyCmd(t, cmd)
	if got := loadSplitRatio(st); got != 0.42 {
		t.Fatalf("reloaded ratio = %v want 0.42", got)
	}
}

func TestInstallerRefusedQuitKeepsResizeInteraction(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.layout.width, m.layout.height, m.layout.split.ratio = 100, 40, 0.34
	updated, _ := m.enterResizeMode()
	m = updated.(*Model)
	m.nudgeSplit(8)
	m.install.Pending = &uiinstall.Pending{Name: "install-test"}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = updated.(*Model)
	if cmd != nil || m.effects.Quitting {
		t.Fatal("installer gate accepted quit")
	}
	if !m.layout.split.resizeMode || m.layout.split.ratio != 0.42 {
		t.Fatalf("refused quit changed resize interaction: screen = %v ratio=%v", m.layout.split.resizeMode, m.layout.split.ratio)
	}
	if raw, err := m.services.store.Setting(splitRatioSetting); err != nil || raw != "" {
		t.Fatalf("refused quit persisted ratio %q (err %v)", raw, err)
	}
}

func TestInstallerRefusedQuitKeepsMouseDrag(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.layout.width, m.layout.height, m.layout.split.ratio = 100, 40, 0.42
	m.layout.split.dragging = true
	m.layout.split.moved = true
	m.push(newResizeSurface(m))
	m.install.Pending = &uiinstall.Pending{Name: "install-test"}

	updated, cmd := m.handleKey(key("q"))
	m = updated.(*Model)
	if cmd != nil || m.effects.Quitting {
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
	t.Parallel()
	m := buildModel(t)
	st := m.services.store

	m.layout.split.ratio = 0.41
	m.persistSplitRatio()
	m.layout.split.ratio = 0.52
	m.persistSplitRatio()
	if raw, err := st.Setting(splitRatioSetting); err != nil || raw != "" {
		t.Fatalf("Update path wrote split ratio before the worker ran: %q, %v", raw, err)
	}
	if len(m.effects.Main.Pending) != 2 {
		t.Fatalf("queued saves = %d, want 2", len(m.effects.Main.Pending))
	}
	first := m.effects.Main.Pending[0].Request.(splitSaveRequest)
	second := m.effects.Main.Pending[1].Request.(splitSaveRequest)
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
	m.applySplitSave(&uieffects.Job{ID: 1}, errors.New("save split ratio: database is locked"))
	if m.errBar.text == "" {
		t.Fatal("latest failed save did not surface its error")
	}

	m.layout.split.saveID = 2
	m.applySplitSave(&uieffects.Job{ID: 2}, nil)
	if m.errBar.text != "" || m.layout.split.saveError != "" {
		t.Fatalf("newer successful save left stale warning %q", m.errBar.text)
	}

	m.errBar.text = "another operation failed"
	m.layout.split.saveID = 4
	m.applySplitSave(&uieffects.Job{ID: 3}, errors.New("older split failure"))
	m.applySplitSave(&uieffects.Job{ID: 4}, nil)
	if m.errBar.text != "another operation failed" {
		t.Fatalf("split completion erased unrelated status %q", m.errBar.text)
	}
}

func TestEnterResizeBlockedWhenSearchingOrQuick(t *testing.T) {
	m := &Model{layout: layoutState{width: 100, height: 40, split: splitState{ratio: defaultSplitRatio}}, services: services{listKeys: keybind.DefaultList()}, rail: railModelSearching(nil, 0, "")}
	updated, cmd := m.enterResizeMode()
	m = updated.(*Model)
	if m.layout.split.resizeMode || cmd != nil {
		t.Fatal("searching should block resize mode")
	}
	m.rail.SetSearch(m.rail.Search(), false)
	pushQuick(m)
	updated, cmd = m.enterResizeMode()
	m = updated.(*Model)
	if m.layout.split.resizeMode || cmd != nil {
		t.Fatal("quick prompt should block resize mode")
	}
}

func TestNewLoadsPersistedSplitRatio(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if err := m.services.store.SetSetting(splitRatioSetting, "0.45"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	loaded := reloadModel(t, m)
	if loaded.layout.split.ratio != 0.45 {
		t.Fatalf("New splitRatio = %v want 0.45", loaded.layout.split.ratio)
	}
}
