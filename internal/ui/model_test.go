package ui

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestPreviewSettleDropsStaleGen(t *testing.T) {
	m := &Model{mode: modeList, width: 120, height: 40, previewGen: 3}
	updated, cmd := m.Update(previewSettleMsg{gen: 2})
	m = updated.(*Model)
	if cmd != nil {
		t.Fatal("stale settle should not schedule previewCmd")
	}
	_, cmd = m.Update(previewSettleMsg{gen: 3})
	if cmd != nil {
		t.Fatal("settle without a session row should not capture")
	}
}

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
	m := &Model{mode: modeList, mouseDisabled: true}
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

func TestPreviewCadenceIsIndependentFromStartupAnimation(t *testing.T) {
	tests := []struct {
		name string
		rows []treeRow
		want time.Duration
	}{
		{"selected starting", []treeRow{{sess: store.Session{Status: status.Starting}}}, previewIntervalLive},
		{"selected working", []treeRow{{sess: store.Session{Status: status.Working}}, {sess: store.Session{Status: status.Starting}}}, previewIntervalLive},
		{"selected idle", []treeRow{{sess: store.Session{Status: status.Idle}}, {sess: store.Session{Status: status.Starting}}}, previewIntervalCalm},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Model{rows: tt.rows}
			if got := m.previewInterval(); got != tt.want {
				t.Fatalf("preview interval = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStartupTickRunsWhileBooting(t *testing.T) {
	m := &Model{booting: true}
	if cmd := m.startStartupTick(); cmd == nil || !m.startupAnimating {
		t.Fatal("boot should start the loader tick")
	}
	m.booting = false
	_, cmd := m.Update(startupTickMsg{})
	if cmd != nil || m.startupAnimating {
		t.Fatal("loader tick kept running after boot settled")
	}
}

func TestFirstRefreshClearsBootLoader(t *testing.T) {
	m := &Model{booting: true, collapsed: map[string]bool{}}
	updated, _ := m.Update(refreshMsg{listedAt: time.Now()})
	got := updated.(*Model)
	if got.booting {
		t.Fatal("the first poller pass should end boot")
	}
}

func TestStartupTickRunsOnlyWhileAStartingRowIsVisible(t *testing.T) {
	m := &Model{}
	if cmd := m.startStartupTick(); cmd != nil {
		t.Fatal("startup tick began without a starting row")
	}
	m.rows = []treeRow{{sess: store.Session{Status: status.Starting}}}
	if cmd := m.startStartupTick(); cmd == nil || !m.startupAnimating {
		t.Fatal("starting row did not begin the startup tick")
	}
	if cmd := m.startStartupTick(); cmd != nil {
		t.Fatal("an active startup tick was scheduled twice")
	}
	m.rows[0].sess.Status = status.Idle
	_, cmd := m.Update(startupTickMsg{})
	if cmd != nil || m.startupAnimating {
		t.Fatal("startup tick kept running after the starting row settled")
	}
}

func TestStartupTickRunsWhileReviewLoads(t *testing.T) {
	m := &Model{mode: modeDiff, diff: diffState{active: true, loading: true}}
	if cmd := m.startStartupTick(); cmd == nil || !m.startupAnimating {
		t.Fatal("a loading review should start the loader tick")
	}
	m.diff.loading = false
	_, cmd := m.Update(startupTickMsg{})
	if cmd != nil || m.startupAnimating {
		t.Fatal("loader tick kept running after the review load settled")
	}
	m.diff.set.Files = []diff.FileDiff{{File: git.ChangedFile{Path: "main.go"}}}
	if cmd := m.startStartupTick(); cmd == nil || !m.startupAnimating {
		t.Fatal("an unloaded selected file should start the loader tick")
	}
	m.diff.set.Files[0] = diff.BuildFile(nil, nil, git.ChangedFile{Path: "main.go"}, git.FileStat{})
	_, cmd = m.Update(startupTickMsg{})
	if cmd != nil || m.startupAnimating {
		t.Fatal("loader tick kept running after the selected file loaded")
	}
}

func TestMoveCursorDebouncesPreview(t *testing.T) {
	m := &Model{
		mode:   modeList,
		width:  120,
		height: 40,
		rows: []treeRow{
			{sess: store.Session{ID: "a", Name: "a"}},
			{sess: store.Session{ID: "b", Name: "b"}},
		},
		cursor: 0,
	}
	cmd := m.moveCursor(1)
	if m.cursor != 1 {
		t.Fatalf("cursor = %d want 1", m.cursor)
	}
	if m.previewGen != 1 {
		t.Fatalf("previewGen = %d want 1", m.previewGen)
	}
	if cmd == nil {
		t.Fatal("move should schedule a settle tick")
	}
	// tea.Tick cmds sleep; invoke the settle path directly for the gen check.
	msg := previewSettleMsg{gen: 1}
	// A second move bumps gen; the first settle is now stale.
	m.moveCursor(-1)
	if m.previewGen != 2 {
		t.Fatalf("previewGen = %d want 2", m.previewGen)
	}
	updated, next := m.Update(msg)
	m = updated.(*Model)
	if next != nil {
		t.Fatal("stale settle after second move must not capture")
	}
	// Fresh settle for the current gen with a session should schedule previewCmd.
	_, next = m.Update(previewSettleMsg{gen: m.previewGen})
	if next == nil {
		t.Fatal("current settle should schedule a capture")
	}
}

// The preview has to follow the pane on its own, without the cursor being
// touched: a manager whose preview only refreshes on selection is a
// screenshot, not a monitor.
func TestPreviewFollowsPaneWithoutCursorMoves(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "watcher", t.TempDir(), "")
	m.selectSessionRow(t, "watcher")
	sess := m.sessionRows()[0]

	waitForPane := func(marker string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			// Only the poll cycle runs here; nothing selects or moves.
			m.applyCmd(t, m.refreshCmd())
			if strings.Contains(ansi.Strip(m.preview), marker) {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("preview never picked up %q, has:\n%s", marker, ansi.Strip(m.preview))
			}
			time.Sleep(50 * time.Millisecond)
		}
	}

	if err := m.tmux.SendText(sess.ID, "first-line"); err != nil {
		t.Fatalf("send text: %v", err)
	}
	waitForPane("first-line")

	if err := m.tmux.SendText(sess.ID, "second-line"); err != nil {
		t.Fatalf("send text: %v", err)
	}
	waitForPane("second-line")

	// The frame has to carry it too, not just the model field.
	if view := ansi.Strip(m.View()); !strings.Contains(view, "second-line") {
		t.Fatalf("frame missing the newest pane content:\n%s", view)
	}
}

func windowSize(t *testing.T, id string) (int, int) {
	t.Helper()
	out, err := tmuxCmd("display-message", "-p", "-t", "am_"+id,
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
	m := buildModel(t)
	createSession(t, m, "leftover", t.TempDir(), "")
	id := m.sessionRows()[0].ID

	// Drift the window as if an older manager had sized it to the terminal;
	// a fresh manager also starts with no geometry cached for it.
	if _, err := tmuxCmd("resize-window", "-t", "am_"+id, "-x", "191", "-y", "55").CombinedOutput(); err != nil {
		t.Fatalf("resize-window: %v", err)
	}

	m.sessionsSized = false
	m.pane.geom = nil
	m.applyCmd(t, m.refreshCmd())
	if w, _ := windowSize(t, id); w != m.previewPaneWidth() {
		t.Fatalf("after first refresh, window width = %d, want %d", w, m.previewPaneWidth())
	}

	// Later refreshes leave sizes alone (attach keeps its own resync path).
	if _, err := tmuxCmd("resize-window", "-t", "am_"+id, "-x", "100", "-y", "30").CombinedOutput(); err != nil {
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
	m := buildModel(t)
	createSession(t, m, "sized", t.TempDir(), "")
	id := m.sessionRows()[0].ID

	// Drift the session window away from the manager size behind its back.
	if _, err := tmuxCmd("resize-window", "-t", "am_"+id, "-x", "100", "-y", "30").CombinedOutput(); err != nil {
		t.Fatalf("resize-window: %v", err)
	}

	// Same size as the model: the resume case, which must not touch sessions.
	updated, _ := m.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
	*m = *updated.(*Model)
	if w, h := windowSize(t, id); w != 100 || h != 30 {
		t.Fatalf("unchanged size should skip resize, session is %dx%d, want 100x30", w, h)
	}

	// A real resize propagates the preview panel box to the session.
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 150, Height: 45})
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
		if err := m.tmux.Kill(id); err != nil {
			t.Fatalf("kill: %v", err)
		}
		if err := m.tmux.Create(id, dir, "", nil, born[0], born[1]); err != nil {
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
	m := buildModel(t)
	createSession(t, m, "sized", t.TempDir(), "")
	m.applyCmd(t, m.refreshCmd())
	width, height, err := m.store.PaneSize()
	if err != nil {
		t.Fatalf("pane size: %v", err)
	}
	if width != m.previewPaneWidth() || height != m.previewPaneHeight() {
		t.Fatalf("published %dx%d, want %dx%d", width, height, m.previewPaneWidth(), m.previewPaneHeight())
	}

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 150, Height: 45})
	*m = *updated.(*Model)
	width, height, err = m.store.PaneSize()
	if err != nil {
		t.Fatalf("pane size after resize: %v", err)
	}
	if width != m.previewPaneWidth() || height != m.previewPaneHeight() {
		t.Fatalf("after a resize published %dx%d, want %dx%d", width, height, m.previewPaneWidth(), m.previewPaneHeight())
	}
}

func TestRebuildRowsNestsChildrenUnderParent(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	agent := m.sessionRows()[0]
	child := store.Session{
		ID: "sh1", Name: "term-one", Tool: "terminal", Cwd: dir,
		Group: "backend", ParentID: agent.ID, Status: status.Idle,
	}
	if err := m.store.CreateSession(child); err != nil {
		t.Fatalf("child: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())

	var names []string
	var depths []int
	for _, row := range m.rows {
		if row.isGroup {
			continue
		}
		names = append(names, row.sess.Name)
		depths = append(depths, row.depth)
	}
	if len(names) < 2 || names[0] != "coder" || names[1] != "term-one" {
		t.Fatalf("order = %v", names)
	}
	if depths[1] != depths[0]+1 {
		t.Fatalf("depths = %v", depths)
	}
}

func TestSearchMatchingChildKeepsParent(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	agent := m.sessionRows()[0]
	if err := m.store.CreateSession(store.Session{
		ID: "sh1", Name: "ssh-prod", Tool: "terminal", Cwd: dir,
		Group: "backend", ParentID: agent.ID, Status: status.Idle,
	}); err != nil {
		t.Fatalf("child: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.search = "ssh-prod"
	m.rebuildRows()
	names := []string{}
	for _, row := range m.rows {
		if !row.isGroup {
			names = append(names, row.sess.Name)
		}
	}
	if !strings.Contains(strings.Join(names, ","), "coder") || !strings.Contains(strings.Join(names, ","), "ssh-prod") {
		t.Fatalf("search dropped parent: %v", names)
	}
}

func TestSearchCarriedParentKeepsStoreOrder(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "carrier", dir, "backend")
	carrier := m.sessionRows()[0]
	if err := m.store.CreateSession(store.Session{
		ID: "sh1", Name: "ssh-prod", Tool: "terminal", Cwd: dir,
		Group: "backend", ParentID: carrier.ID, Status: status.Idle,
	}); err != nil {
		t.Fatalf("child: %v", err)
	}
	createSession(t, m, "ssh-runner", dir, "backend")
	m.applyCmd(t, m.refreshCmd())
	m.search = "ssh"
	m.rebuildRows()
	names := []string{}
	for _, row := range m.rows {
		if !row.isGroup {
			names = append(names, row.sess.Name)
		}
	}
	want := []string{"carrier", "ssh-prod", "ssh-runner"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", names, want)
	}
}

func TestOrphanParentIDPaintsUnnested(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	if err := m.store.CreateSession(store.Session{
		ID: "gone", Name: "parent", Tool: "claude", Cwd: dir,
		Group: "backend", Status: status.Idle,
	}); err != nil {
		t.Fatalf("parent: %v", err)
	}
	if err := m.store.CreateSession(store.Session{
		ID: "sh1", Name: "loose", Tool: "terminal", Cwd: dir,
		Group: "backend", ParentID: "gone", Status: status.Idle,
	}); err != nil {
		t.Fatalf("orphan: %v", err)
	}
	if err := m.store.Delete("gone"); err != nil {
		t.Fatalf("delete parent: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	for _, row := range m.rows {
		if !row.isGroup && row.sess.Name == "loose" && row.depth == 1 {
			return
		}
	}
	t.Fatalf("orphan should sit un-nested in backend: %+v", m.rows)
}

func TestArchiveViewShowsNestedShellWhenParentLive(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	agent := m.sessionRows()[0]
	if err := m.store.CreateSession(store.Session{
		ID: "sh1", Name: "old-term", Tool: "terminal", Cwd: dir,
		Group: "backend", ParentID: agent.ID, Status: status.Idle,
	}); err != nil {
		t.Fatalf("child: %v", err)
	}
	if err := m.store.SetArchived("sh1", true); err != nil {
		t.Fatalf("archive: %v", err)
	}
	m.showArchived = true
	m.applyCmd(t, m.refreshCmd())
	var names []string
	for _, row := range m.rows {
		if !row.isGroup {
			names = append(names, row.sess.Name)
		}
	}
	joined := strings.Join(names, ",")
	if !strings.Contains(joined, "old-term") {
		t.Fatalf("archived nested shell missing: %v", names)
	}
	if strings.Contains(joined, "coder") {
		t.Fatalf("live parent leaked into archive view: %v", names)
	}
}

func TestStatusFilterHoldsNestedIdleShell(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	agent := m.sessionRows()[0]
	if err := m.store.CreateSession(store.Session{
		ID: "sh1", Name: "term-hold", Tool: "terminal", Cwd: dir,
		Group: "backend", ParentID: agent.ID, Status: status.Idle,
	}); err != nil {
		t.Fatalf("child: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectSessionRow(t, "term-hold")
	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	m = updated.(*Model)
	if cmd != nil {
		m.applyCmd(t, cmd)
	}
	if !strings.Contains(strings.Join(sessionNames(m), ","), "term-hold") {
		t.Fatalf("held nested shell dropped: %v", sessionNames(m))
	}
	sess, ok := m.selected()
	if !ok || sess.Name != "term-hold" {
		t.Fatalf("cursor left the held shell: %+v %v", sess, ok)
	}
}

func TestUnlistedParentsLeaveChildrenInStoreOrder(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "first", dir, "backend")
	createSession(t, m, "second", dir, "backend")
	rows := m.sessionRows()
	for i, parent := range rows {
		child := store.Session{
			ID: "sh" + strconv.Itoa(i), Name: "term-" + strconv.Itoa(i), Tool: "terminal",
			Cwd: dir, Group: "backend", ParentID: parent.ID, Status: status.Idle,
		}
		if err := m.store.CreateSession(child); err != nil {
			t.Fatalf("child %d: %v", i, err)
		}
	}
	for _, parent := range rows {
		if err := m.store.SetArchived(parent.ID, true); err != nil {
			t.Fatalf("archive %s: %v", parent.ID, err)
		}
	}
	m.applyCmd(t, m.refreshCmd())
	var names []string
	for _, row := range m.rows {
		if !row.isGroup {
			names = append(names, row.sess.Name)
			if row.depth != 1 {
				t.Fatalf("%s painted nested: depth %d", row.sess.Name, row.depth)
			}
		}
	}
	want := []string{"term-0", "term-1"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", names, want)
	}
}

func TestSearchMatchingArchivedChildDoesNotHoistLiveParent(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	agent := m.sessionRows()[0]
	if err := m.store.CreateSession(store.Session{
		ID: "sh1", Name: "ssh-old", Tool: "terminal", Cwd: dir,
		Group: "backend", ParentID: agent.ID, Status: status.Idle,
	}); err != nil {
		t.Fatalf("child: %v", err)
	}
	if err := m.store.SetArchived("sh1", true); err != nil {
		t.Fatalf("archive: %v", err)
	}
	m.showArchived = true
	m.applyCmd(t, m.refreshCmd())
	m.search = "ssh-old"
	m.rebuildRows()
	var names []string
	for _, row := range m.rows {
		if !row.isGroup {
			names = append(names, row.sess.Name)
		}
	}
	joined := strings.Join(names, ",")
	if !strings.Contains(joined, "ssh-old") {
		t.Fatalf("archived child missing: %v", names)
	}
	if strings.Contains(joined, "coder") {
		t.Fatalf("search hoisted live parent into archive view: %v", names)
	}
}

// An agent that splits its own window takes room from the pane the preview
// draws, on whichever axis it split, and no manager action precedes it for
// the geometry cache to follow. A later refresh has to notice and pin that
// pane back to the box.
func TestRefreshRepinsAnAgentSplitPane(t *testing.T) {
	for _, split := range []struct {
		axis string
		flag string
	}{{"horizontal", "-h"}, {"vertical", "-v"}} {
		t.Run(split.axis, func(t *testing.T) {
			m := buildModel(t)
			createSession(t, m, "split", t.TempDir(), "")
			id := m.sessionRows()[0].ID
			m.applyCmd(t, m.refreshCmd())

			if out, err := tmuxCmd("split-window", split.flag, "-t", "am_"+id, "--", "sh", "-c", "sleep 30").CombinedOutput(); err != nil {
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
	out, err := tmuxCmd("list-panes", "-t", "am_"+id, "-f", "#{==:#{pane_index},0}", "-F", "#{pane_width} #{pane_height}").CombinedOutput()
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

// The rows are marked against the server the poll read panes from, so the
// refresh has to hand that socket to the model it renders from.
func TestRefreshCarriesTheSocketItReadPanesFrom(t *testing.T) {
	for _, socket := range []string{"/tmp/first/agentmgr", "/tmp/second/agentmgr", ""} {
		m := &Model{collapsed: map[string]bool{}, tmuxSocket: "/tmp/stale/agentmgr"}
		m.Update(refreshMsg{tmuxSocket: socket, leadingManager: true, listedAt: time.Now()})
		if m.tmuxSocket != socket {
			t.Fatalf("model socket = %q, want the poll's %q", m.tmuxSocket, socket)
		}
		if !m.leadingManager {
			t.Fatal("the poll's hold on the store should reach the model")
		}
	}
}

// New hands the stored key table to the tmux driver, so a session the
// manager creates is bound and labelled the same way focus reads its keys.
func TestNewHandsTheKeyTableToTmux(t *testing.T) {
	m := buildModel(t)
	stored := keybind.DefaultSession().With(keybind.Detach, bindingOf(t, "f9")).With(keybind.Review, bindingOf(t, "ctrl+g"))
	if err := m.store.SetKeys(stored); err != nil {
		t.Fatalf("SetKeys: %v", err)
	}
	loaded := reloadModel(t, m)
	loaded.width, loaded.height = 120, 40
	t.Cleanup(func() { m.tmux.SetSessionKeys(keybind.DefaultSession()) })
	if got := loaded.keys.Binding(keybind.Editor).Label(); got != "f3" {
		t.Fatalf("an action nobody moved should keep its default, got %q", got)
	}
	createSession(t, loaded, "tablebound", t.TempDir(), "")
	loaded.selectSessionRow(t, "tablebound")
	sess := loaded.rows[loaded.cursor].sess
	t.Cleanup(func() { m.tmux.Kill(sess.ID) })
	right, err := tmuxCmd("display-message", "-p", "-t", "am_"+sess.ID, "#{T:status-right}").CombinedOutput()
	if err != nil {
		t.Fatalf("status-right: %v", err)
	}
	if !strings.Contains(string(right), "Ctrl+g = review") || !strings.Contains(string(right), "F9") {
		t.Fatalf("session footer should carry the stored keys, got %q", right)
	}
}

func TestStartupPreservesExistingPaneHeight(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "tall", t.TempDir(), "")
	id := m.sessionRows()[0].ID
	if _, err := tmuxCmd("resize-window", "-t", "am_"+id, "-x", "120", "-y", "80").CombinedOutput(); err != nil {
		t.Fatal(err)
	}
	loaded := reloadModel(t, m)
	loaded.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	loaded.applyCmd(t, loaded.refreshCmd())
	if _, h := windowSize(t, id); h < 80 {
		t.Fatalf("reopen shrank an existing pane from 80 to %d rows", h)
	}
}

func TestStartupErrorStaysVisibleUntilFirstRefresh(t *testing.T) {
	m := buildModel(t)
	m.booting = true
	m.Update(errMsg{errors.New("startup poll failed")})
	if !m.booting {
		t.Fatal("an error before the first refresh must not finish boot")
	}
	if !strings.Contains(ansi.Strip(m.View()), "startup poll failed") {
		t.Fatal("startup error is hidden behind the boot loader")
	}
	m.Update(refreshMsg{listedAt: time.Now()})
	if m.booting {
		t.Fatal("the first successful refresh must finish boot")
	}
}

// focusRequestModel lays out a root session and one filed two groups deep,
// with both groups folded so the deep one has no row until it is unfolded.
func focusRequestModel(t *testing.T) (*Model, store.Session) {
	t.Helper()
	m := buildModel(t)
	if err := m.store.CreateGroup("backend/api", ""); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "top", t.TempDir(), "")
	createSession(t, m, "deep", t.TempDir(), "backend/api")
	var deep store.Session
	for _, sess := range m.sessions {
		if sess.Name == "deep" {
			deep = sess
		}
	}
	if deep.ID == "" {
		t.Fatal("deep session missing")
	}
	m.collapsed["backend"] = true
	m.collapsed["backend/api"] = true
	m.rebuildRows()
	m.selectSessionRow(t, "top")
	return m, deep
}

// serveFocusRequest hands the model a request the way the focus endpoint
// does and returns the answer the endpoint would write back.
func (m *Model) serveFocusRequest(t *testing.T, id string, enter bool) error {
	t.Helper()
	answer := make(chan error, 1)
	updated, _ := m.Update(focusRequestMsg{
		request: sessioncmd.FocusRequest{ID: id, Enter: enter},
		answer:  answer,
		expires: time.Now().Add(time.Minute),
	})
	*m = *updated.(*Model)
	select {
	case err := <-answer:
		return err
	default:
		t.Fatal("the request was not answered")
		return nil
	}
}

// A focus request lands on the session from wherever the user is, closing
// the layers that hold no typed input the way esc would and unfolding the
// groups that hide the row, then opens it the way enter does.
func TestFocusRequestUnfoldsAndEntersFromEveryStartingState(t *testing.T) {
	for _, start := range []struct {
		name  string
		setup func(t *testing.T, m *Model)
	}{
		{"list", func(t *testing.T, m *Model) {}},
		{"search hiding the row", func(t *testing.T, m *Model) {
			m.searching = true
			m.search = "top"
			m.rebuildRows()
		}},
		{"help with a query typed", func(t *testing.T, m *Model) {
			m.openHelp()
			m.help.searching = true
			m.help.query = "attach"
		}},
		{"focused on another session", func(t *testing.T, m *Model) {
			m.focusSelected()
			if m.mode != modeFocus {
				t.Fatalf("setup: mode = %v, want focus", m.mode)
			}
		}},
	} {
		t.Run(start.name, func(t *testing.T) {
			m, deep := focusRequestModel(t)
			start.setup(t, m)
			if err := m.serveFocusRequest(t, deep.ID, true); err != nil {
				t.Fatalf("focus answered %v", err)
			}
			if m.mode != modeFocus {
				t.Fatalf("mode = %v, err = %q; want focus", m.mode, m.errBar.text)
			}
			if sess, ok := m.selected(); !ok || sess.ID != deep.ID {
				t.Fatalf("cursor is on %+v, want deep", sess)
			}
			if m.collapsed["backend"] || m.collapsed["backend/api"] {
				t.Fatalf("groups above the session are still folded: %v", m.collapsed)
			}
			if m.search != "" || m.searching {
				t.Fatalf("search = %q (field open %v), want it cleared", m.search, m.searching)
			}
		})
	}
}

// Without enter the request only selects, so a focused pane is left for the
// list even when it is the session asked for: staying would keep every key
// going to that agent while the request reported a selection.
func TestSelectRequestLeavesFocusAndStaysOnTheList(t *testing.T) {
	for _, start := range []string{"top", "deep"} {
		t.Run("focused on "+start, func(t *testing.T) {
			m, deep := focusRequestModel(t)
			if start == "deep" {
				if err := m.serveFocusRequest(t, deep.ID, true); err != nil {
					t.Fatalf("setup: focus answered %v", err)
				}
			} else {
				m.focusSelected()
			}
			if m.mode != modeFocus {
				t.Fatalf("setup: mode = %v, want focus", m.mode)
			}
			if err := m.serveFocusRequest(t, deep.ID, false); err != nil {
				t.Fatalf("select answered %v", err)
			}
			if m.mode != modeList {
				t.Fatalf("mode = %v, want the list", m.mode)
			}
			if sess, ok := m.selected(); !ok || sess.ID != deep.ID {
				t.Fatalf("cursor is on %+v, want deep", sess)
			}
		})
	}
}

// A dialog may hold input the user has not finished, so a request never
// closes it: the cursor moves under it, as a clicked banner always did. A
// focus is answered with why the session was not opened over it.
func TestFocusRequestKeepsADialogHoldingInput(t *testing.T) {
	for _, enter := range []bool{false, true} {
		t.Run(map[bool]string{false: "select", true: "focus"}[enter], func(t *testing.T) {
			m, deep := focusRequestModel(t)
			m.openForm()
			m.form.name.SetValue("half typed")
			err := m.serveFocusRequest(t, deep.ID, enter)
			if enter != (err != nil && strings.Contains(err.Error(), "dialog")) {
				t.Fatalf("answer = %v, want the dialog named only for a focus", err)
			}
			if m.mode != modeForm || m.form.name.Value() != "half typed" {
				t.Fatalf("mode = %v, name = %q; want the form kept with its text", m.mode, m.form.name.Value())
			}
			if sess, ok := m.selected(); !ok || sess.ID != deep.ID {
				t.Fatalf("cursor is on %+v, want deep under the form", sess)
			}
		})
	}
}

// A status filter that hides the session is dropped rather than leaving the
// request with nowhere to land.
func TestFocusRequestDropsAStatusFilterHidingTheRow(t *testing.T) {
	m, deep := focusRequestModel(t)
	m.statusFilter = statusFilterCycle[1]
	delete(m.collapsed, "backend")
	delete(m.collapsed, "backend/api")
	m.rebuildRows()
	for _, sess := range m.sessionRows() {
		if sess.ID == deep.ID {
			t.Fatalf("setup: filter %v still shows the idle session", m.statusFilter)
		}
	}
	if err := m.serveFocusRequest(t, deep.ID, false); err != nil {
		t.Fatalf("select answered %v", err)
	}
	if sess, ok := m.selected(); !ok || sess.ID != deep.ID {
		t.Fatalf("cursor is on %+v, want deep", sess)
	}
}

// A request the program could not take in time has already been answered
// as timed out, so it must not move the list when it finally arrives.
func TestExpiredFocusRequestIsDropped(t *testing.T) {
	m, deep := focusRequestModel(t)
	answer := make(chan error, 1)
	updated, _ := m.Update(focusRequestMsg{
		request: sessioncmd.FocusRequest{ID: deep.ID, Enter: true},
		answer:  answer,
		expires: time.Now().Add(-time.Second),
	})
	m = updated.(*Model)
	if sess, ok := m.selected(); !ok || sess.Name != "top" || m.mode != modeList {
		t.Fatalf("an expired request moved the list: cursor %+v, mode %v", sess, m.mode)
	}
	if len(answer) != 0 {
		t.Fatal("an expired request was answered a second time")
	}
}
