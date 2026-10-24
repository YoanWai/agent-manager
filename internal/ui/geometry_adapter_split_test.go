package ui

import (
	"errors"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/keybind"
	uigeometry "github.com/YoanWai/agent-manager/internal/ui/geometry"
	uigeometrypanes "github.com/YoanWai/agent-manager/internal/ui/geometry/panes"
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
	ratio := uigeometry.DefaultSplitRatio
	wantLeft := uigeometry.ClampSplitLeft(int(ratio*100), 100)
	if left != wantLeft || right != 100-wantLeft {
		t.Fatalf("default split = %d,%d want %d,%d", left, right, wantLeft, 100-wantLeft)
	}
}

func TestSetSplitFromXClampsAndUpdatesRatio(t *testing.T) {
	m := &Model{layout: layoutState{width: 100, split: splitState{ratio: uigeometry.DefaultSplitRatio}}, services: services{listKeys: keybind.DefaultList()}}
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
	if left != uigeometry.MinSplitSide || right != 100-uigeometry.MinSplitSide {
		t.Fatalf("clamped left split = %d,%d", left, right)
	}
}

func TestResizeModeKeyArmsDrag(t *testing.T) {
	m := &Model{layout: layoutState{split: splitState{ratio: uigeometry.DefaultSplitRatio}, width: 120, height: 40}, services: services{listKeys: keybind.DefaultList()}}
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
	raw, err := st.Setting(uigeometry.SplitRatioSetting)
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
	if got := uigeometrypanes.Load(st); got != 0.42 {
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
	if got := uigeometrypanes.Load(st); got != 0.42 {
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
	if got := uigeometrypanes.Load(st); got != 0.42 {
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
	m.ledger.install.Pending = &uiinstall.Pending{Name: "install-test"}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = updated.(*Model)
	if cmd != nil || m.effects.Quitting {
		t.Fatal("installer gate accepted quit")
	}
	if !m.layout.split.resizeMode || m.layout.split.ratio != 0.42 {
		t.Fatalf("refused quit changed resize interaction: screen = %v ratio=%v", m.layout.split.resizeMode, m.layout.split.ratio)
	}
	if raw, err := m.services.store.Setting(uigeometry.SplitRatioSetting); err != nil || raw != "" {
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
	m.ledger.install.Pending = &uiinstall.Pending{Name: "install-test"}

	updated, cmd := m.handleKey(key("q"))
	m = updated.(*Model)
	if cmd != nil || m.effects.Quitting {
		t.Fatal("installer gate accepted quit")
	}
	if !m.layout.split.dragging || !m.layout.split.moved || m.layout.split.ratio != 0.42 {
		t.Fatalf("refused quit changed mouse drag: dragging=%v moved=%v ratio=%v", m.layout.split.dragging, m.layout.split.moved, m.layout.split.ratio)
	}
	if raw, err := m.services.store.Setting(uigeometry.SplitRatioSetting); err != nil || raw != "" {
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
	if raw, err := st.Setting(uigeometry.SplitRatioSetting); err != nil || raw != "" {
		t.Fatalf("Update path wrote split ratio before the worker ran: %q, %v", raw, err)
	}
	if len(m.effects.Main.Pending) != 2 {
		t.Fatalf("queued saves = %d, want 2", len(m.effects.Main.Pending))
	}
	first := m.effects.Main.Pending[0].Request.(uigeometrypanes.SplitSaveRequest)
	second := m.effects.Main.Pending[1].Request.(uigeometrypanes.SplitSaveRequest)
	if first.value != "0.4100" || second.value != "0.5200" {
		t.Fatalf("captured order = %q then %q", first.value, second.value)
	}

	m.drainEffects(t)
	if got := uigeometrypanes.Load(st); got != 0.52 {
		t.Fatalf("reloaded ratio = %v, want latest accepted 0.52", got)
	}
}

func TestNewerSplitSaveSuccessClearsOnlySplitFailure(t *testing.T) {
	m := &Model{layout: layoutState{panes: uigeometrypanes.Panes{SaveID: 1}}}
	m.layout.panes.ApplySplit(paneSizing{m}, 1, errors.New("save split ratio: database is locked"))
	if m.errBar.text == "" {
		t.Fatal("latest failed save did not surface its error")
	}

	m.layout.panes.SaveID = 2
	m.layout.panes.ApplySplit(paneSizing{m}, 2, nil)
	if m.errBar.text != "" || m.layout.panes.SaveError != "" {
		t.Fatalf("newer successful save left stale warning %q", m.errBar.text)
	}

	m.errBar.text = "another operation failed"
	m.layout.panes.SaveID = 4
	m.layout.panes.ApplySplit(paneSizing{m}, 3, errors.New("older split failure"))
	m.layout.panes.ApplySplit(paneSizing{m}, 4, nil)
	if m.errBar.text != "another operation failed" {
		t.Fatalf("split completion erased unrelated status %q", m.errBar.text)
	}
}

func TestEnterResizeBlockedWhenSearchingOrQuick(t *testing.T) {
	m := &Model{layout: layoutState{width: 100, height: 40, split: splitState{ratio: uigeometry.DefaultSplitRatio}}, services: services{listKeys: keybind.DefaultList()}, rail: railModelSearching(nil, 0, "")}
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
	if err := m.services.store.SetSetting(uigeometry.SplitRatioSetting, "0.45"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	loaded := reloadModel(t, m)
	if loaded.layout.split.ratio != 0.45 {
		t.Fatalf("New splitRatio = %v want 0.45", loaded.layout.split.ratio)
	}
}

func windowSize(t *testing.T, id string) (int, int) {
	t.Helper()
	out, err := tmuxCmd(t, "display-message", "-p", "-t", "am_"+id,
		"#{window_width} #{window_height}").CombinedOutput()
	if err != nil {
		t.Fatalf("display-message: %v: %s", err, out)
	}
	parts := strings.Fields(string(out))
	if len(parts) != 2 {
		t.Fatalf("unexpected size output %q", out)
	}
	w, _ := strconv.Atoi(parts[0])
	h, _ := strconv.Atoi(parts[1])
	return w, h
}

// Sessions left over from a previous manager run keep that run's window
// size; the first refresh after startup must bring a wrong-width window to
// the preview panel so its captures fit without a terminal resize.
func TestFirstRefreshResizesExistingSessions(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "leftover", t.TempDir(), "")
	id := m.sessionRows()[0].ID

	// Drift the window as if an older manager had sized it to the terminal;
	// a fresh manager also starts with no geometry cached for it.
	if _, err := tmuxCmd(t, "resize-window", "-t", "am_"+id, "-x", "191", "-y", "55").CombinedOutput(); err != nil {
		t.Fatalf("resize-window: %v", err)
	}

	m.startup.sessionsSized = false
	m.layout.panes.LastSizes = nil
	m.applyCmd(t, m.refreshCmd())
	if w, _ := windowSize(t, id); w != m.previewPaneWidth() {
		t.Fatalf("after first refresh, window width = %d, want %d", w, m.previewPaneWidth())
	}

	// Later refreshes leave sizes alone (attach keeps its own resync path).
	if _, err := tmuxCmd(t, "resize-window", "-t", "am_"+id, "-x", "100", "-y", "30").CombinedOutput(); err != nil {
		t.Fatalf("resize-window: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	if w, _ := windowSize(t, id); w != 100 {
		t.Fatalf("later refresh should not resize, window width = %d, want 100", w)
	}
}

// A WindowSizeMsg carrying the current size (as a tmux-attach resume does)
// skips the per-session tmux resize; a real size change still applies it.
func TestUnchangedWindowSizeSkipsResize(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "sized", t.TempDir(), "")
	id := m.sessionRows()[0].ID

	// Drift the session window away from the manager size behind its back.
	if _, err := tmuxCmd(t, "resize-window", "-t", "am_"+id, "-x", "100", "-y", "30").CombinedOutput(); err != nil {
		t.Fatalf("resize-window: %v", err)
	}

	// Same size as the model: the resume case, which must not touch sessions.
	updated, _ := m.Update(tea.WindowSizeMsg{Width: m.layout.width, Height: m.layout.height})
	m.drainEffects(t)
	*m = *updated.(*Model)
	if w, h := windowSize(t, id); w != 100 || h != 30 {
		t.Fatalf("unchanged size should skip resize, session is %dx%d, want 100x30", w, h)
	}

	// A real resize propagates the preview panel box to the session.
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 150, Height: 45})
	m.drainEffects(t)
	m.drainEffects(t)
	*m = *updated.(*Model)
	wantW, wantH := m.previewPaneWidth(), m.previewPaneHeight()
	if w, h := windowSize(t, id); w != wantW || h != wantH {
		t.Fatalf("changed size should resize session, got %dx%d, want %dx%d", w, h, wantW, wantH)
	}
}

// A revive that runs outside the manager, from the CLI or the MCP tool,
// puts the row on a window this process never sized. The geometry it
// cached for the window that died would otherwise read as already matching
// the box, leaving the replacement at whatever it was born with. The
// replacement is pinned to the exact box, taller or shorter, since a pane
// born a moment ago has no scrollback to keep.
func TestRelaunchedSessionIsRepinnedToThePreviewPanel(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	createSession(t, m, "revived", dir, "")
	id := m.sessionRows()[0].ID
	m.applyCmd(t, m.refreshCmd())
	if w, _ := windowSize(t, id); w != m.previewPaneWidth() {
		t.Fatalf("before the relaunch, window width = %d, want %d", w, m.previewPaneWidth())
	}

	// What a revive with no manager to ask does: the sized window is gone
	// and the replacement carries tmux's own default, or a box some other
	// manager run recorded.
	wantW, wantH := m.previewPaneWidth(), m.previewPaneHeight()
	for _, born := range [][2]int{{0, 0}, {wantW + 40, wantH + 20}} {
		if err := m.services.tmux.Kill(id); err != nil {
			t.Fatalf("kill: %v", err)
		}
		if err := m.services.tmux.Create(id, dir, "", nil, born[0], born[1]); err != nil {
			t.Fatalf("create: %v", err)
		}
		if w, h := windowSize(t, id); w == wantW && h == wantH {
			t.Fatalf("relaunched window already at the box %dx%d, nothing to prove", w, h)
		}
		m.applyCmd(t, m.refreshCmd())
		if w, h := windowSize(t, id); w != wantW || h != wantH {
			t.Fatalf("born %dx%d: after the refresh, window = %dx%d, want %dx%d", born[0], born[1], w, h, wantW, wantH)
		}
	}
}

// The launch paths that run without a manager read the box out of the
// store, so the manager has to keep it current there.
func TestRefreshPublishesThePaneSize(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "sized", t.TempDir(), "")
	m.applyCmd(t, m.refreshCmd())
	width, height, err := m.services.store.PaneSize()
	if err != nil {
		t.Fatalf("pane size: %v", err)
	}
	if width != m.previewPaneWidth() || height != m.previewPaneHeight() {
		t.Fatalf("published %dx%d, want %dx%d", width, height, m.previewPaneWidth(), m.previewPaneHeight())
	}

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 150, Height: 45})
	m.drainEffects(t)
	m.drainEffects(t)
	*m = *updated.(*Model)
	width, height, err = m.services.store.PaneSize()
	if err != nil {
		t.Fatalf("pane size after resize: %v", err)
	}
	if width != m.previewPaneWidth() || height != m.previewPaneHeight() {
		t.Fatalf("after a resize published %dx%d, want %dx%d", width, height, m.previewPaneWidth(), m.previewPaneHeight())
	}
}

// An agent that splits its own window takes room from the pane the preview
// draws, on whichever axis it split, and no manager action precedes it for
// the geometry cache to follow. A later refresh has to notice and pin that
// pane back to the box.
func TestRefreshRepinsAnAgentSplitPane(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("psmux resize-pane on a detached window scales against the last client area; the manager does not pin split panes there")
	}
	for _, split := range []struct {
		axis string
		flag string
	}{{"horizontal", "-h"}, {"vertical", "-v"}} {
		t.Run(split.axis, func(t *testing.T) {
			m := buildModel(t)
			createSession(t, m, "split", t.TempDir(), "")
			id := m.sessionRows()[0].ID
			m.applyCmd(t, m.refreshCmd())

			if out, err := tmuxCmd(t, "split-window", split.flag, "-t", "am_"+id, "--", "sh", "-c", "sleep 30").CombinedOutput(); err != nil {
				t.Fatalf("split-window: %v: %s", err, out)
			}
			if width, height := agentPaneSize(t, id); width == m.previewPaneWidth() && height == m.previewPaneHeight() {
				t.Fatalf("the split should have taken room from the agent pane, pane = %dx%d", width, height)
			}

			m.applyCmd(t, m.refreshCmd())

			width, height := agentPaneSize(t, id)
			if width != m.previewPaneWidth() || height != m.previewPaneHeight() {
				t.Fatalf("after refresh, agent pane = %dx%d, want the preview box %dx%d",
					width, height, m.previewPaneWidth(), m.previewPaneHeight())
			}
		})
	}
}

func agentPaneSize(t *testing.T, id string) (int, int) {
	t.Helper()
	out, err := tmuxCmd(t, "list-panes", "-t", "am_"+id, "-f", "#{==:#{pane_index},0}", "-F", "#{pane_width} #{pane_height}").CombinedOutput()
	if err != nil {
		t.Fatalf("list-panes: %v: %s", err, out)
	}
	fields := strings.Fields(string(out))
	if len(fields) != 2 {
		t.Fatalf("pane size %q", out)
	}
	width, widthErr := strconv.Atoi(fields[0])
	height, heightErr := strconv.Atoi(fields[1])
	if widthErr != nil || heightErr != nil {
		t.Fatalf("pane size %q: %v %v", out, widthErr, heightErr)
	}
	return width, height
}

func TestStartupPreservesExistingPaneHeight(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "tall", t.TempDir(), "")
	id := m.sessionRows()[0].ID
	if _, err := tmuxCmd(t, "resize-window", "-t", "am_"+id, "-x", "120", "-y", "80").CombinedOutput(); err != nil {
		t.Fatal(err)
	}
	loaded, err := New(m.services.cfg, m.services.store, m.services.tmux, m.services.engine, m.services.hooks, "dev")
	if err != nil {
		t.Fatal(err)
	}
	loaded.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	loaded.applyCmd(t, loaded.refreshCmd())
	if _, h := windowSize(t, id); h < 80 {
		t.Fatalf("reopen shrank an existing pane from 80 to %d rows", h)
	}
}

// A detached session must boot at the preview panel's width×height so its
// pane preview fills 1:1, and follow later terminal resizes, rather than
// staying at tmux's 80×24 default until attach.
func TestSessionSizesToPreviewPane(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "sized", t.TempDir(), "")
	id := m.sessionRows()[0].ID
	// Create sizes from pre-selection geometry; re-pin to the live preview box.
	m.resizeSessions()
	m.drainEffects(t)

	wantW, wantH := m.previewPaneWidth(), m.previewPaneHeight()
	if w, h := windowSize(t, id); w != wantW || h != wantH {
		t.Fatalf("new session window = %dx%d, want %dx%d", w, h, wantW, wantH)
	}

	m.Update(tea.WindowSizeMsg{Width: 150, Height: 45})
	m.drainEffects(t)
	wantW, wantH = m.previewPaneWidth(), m.previewPaneHeight()
	if w, h := windowSize(t, id); w != wantW || h != wantH {
		t.Fatalf("after resize, window = %dx%d, want %dx%d", w, h, wantW, wantH)
	}
}
