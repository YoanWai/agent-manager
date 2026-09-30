package ui

import (
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"slices"
	"strings"
	"testing"
)

func TestPortableReorderKeysSwapVisibleSessions(t *testing.T) {
	m := buildModel(t)
	for _, sess := range []store.Session{
		{ID: "a", Name: "keep-alpha", Tool: "claude", Cwd: "/tmp", Status: "idle"},
		{ID: "hidden", Name: "filtered", Tool: "claude", Cwd: "/tmp", Status: "idle"},
		{ID: "c", Name: "keep-charlie", Tool: "claude", Cwd: "/tmp", Status: "idle"},
	} {
		if err := m.services.store.CreateSession(sess); err != nil {
			t.Fatalf("create session %q: %v", sess.ID, err)
		}
	}
	m.rail.SetSearch("keep", m.rail.Searching())
	loadStoredRows(t, m)
	m.selectSessionRow(t, "keep-charlie")

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'K'}})
	m = updated.(*Model)
	if got, want := []string{m.sessionRows()[0].ID, m.sessionRows()[1].ID}, []string{"c", "a"}; !slices.Equal(got, want) {
		t.Fatalf("visible order after K = %v want %v", got, want)
	}
	if got, want := listSessionIDs(t, m.services.store), []string{"c", "hidden", "a"}; !slices.Equal(got, want) {
		t.Fatalf("stored order after K = %v want %v", got, want)
	}

	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'J'}})
	m = updated.(*Model)
	if got, want := listSessionIDs(t, m.services.store), []string{"a", "hidden", "c"}; !slices.Equal(got, want) {
		t.Fatalf("stored order after J = %v want %v", got, want)
	}

	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyShiftUp})
	m = updated.(*Model)
	if got, want := listSessionIDs(t, m.services.store), []string{"c", "hidden", "a"}; !slices.Equal(got, want) {
		t.Fatalf("stored order after shift+up = %v want %v", got, want)
	}
}

func TestReorderGroupSkipsFilteredSibling(t *testing.T) {
	m := buildModel(t)
	for _, group := range []string{"alpha", "hidden", "gamma"} {
		if err := m.services.store.CreateGroup(group, ""); err != nil {
			t.Fatalf("create group %q: %v", group, err)
		}
	}
	for _, sess := range []store.Session{
		{ID: "a", Name: "keep-alpha", Tool: "claude", Cwd: "/tmp", Group: "alpha", Status: "idle"},
		{ID: "hidden", Name: "filtered", Tool: "claude", Cwd: "/tmp", Group: "hidden", Status: "idle"},
		{ID: "g", Name: "keep-gamma", Tool: "claude", Cwd: "/tmp", Group: "gamma", Status: "idle"},
	} {
		if err := m.services.store.CreateSession(sess); err != nil {
			t.Fatalf("create session %q: %v", sess.ID, err)
		}
	}
	m.rail.SetSearch("keep", m.rail.Searching())
	loadStoredRows(t, m)
	m.selectGroupRow(t, "gamma")

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'K'}})
	m = updated.(*Model)
	if got, want := m.groupRowPaths(), []string{"gamma", "alpha"}; !slices.Equal(got, want) {
		t.Fatalf("visible group order after K = %v want %v", got, want)
	}
	groups, err := m.services.store.Groups()
	if err != nil {
		t.Fatalf("list groups: %v", err)
	}
	got := make([]string, len(groups))
	for i, group := range groups {
		got[i] = group.Name
	}
	if want := []string{"gamma", "hidden", "alpha"}; !slices.Equal(got, want) {
		t.Fatalf("stored group order after K = %v want %v", got, want)
	}
}

func TestReorderSyntheticGroupUpdatesImmediately(t *testing.T) {
	m := buildModel(t)
	for _, group := range []string{"alpha/deep", "beta/deep", "gamma/deep"} {
		if err := m.services.store.CreateGroup(group, ""); err != nil {
			t.Fatalf("create group %q: %v", group, err)
		}
	}
	loadStoredRows(t, m)
	m.selectGroupRow(t, "gamma")

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'K'}})
	m = updated.(*Model)
	var roots []string
	for _, group := range m.groupRowPaths() {
		if !strings.Contains(group, "/") {
			roots = append(roots, group)
		}
	}
	if want := []string{"alpha", "gamma", "beta"}; !slices.Equal(roots, want) {
		t.Fatalf("root order after K = %v want %v", roots, want)
	}
}

func TestReorderChildStaysWithItsSiblings(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	m.selectSessionRow(t, "coder")
	first := spawnTerminal(t, m)
	m.selectSessionRow(t, "coder")
	second := spawnTerminal(t, m)
	m.selectSessionRow(t, first.Name)
	updated, _ := m.handleKey(runeKey("J"))
	m = updated.(*Model)
	var kids []string
	for _, row := range railRows(m) {
		if !row.isGroup && row.sess.ParentID != "" {
			kids = append(kids, row.sess.Name)
		}
	}
	if len(kids) != 2 || kids[0] != second.Name || kids[1] != first.Name {
		t.Fatalf("sibling order = %v", kids)
	}
}

func TestReorderChildIgnoresAnotherParentsChild(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	createSession(t, m, "other", dir, "backend")
	m.selectSessionRow(t, "coder")
	mine := spawnTerminal(t, m)
	m.selectSessionRow(t, "other")
	theirs := spawnTerminal(t, m)
	m.selectSessionRow(t, mine.Name)
	updated, _ := m.handleKey(runeKey("J"))
	m = updated.(*Model)
	var names []string
	for _, row := range railRows(m) {
		if !row.isGroup {
			names = append(names, row.sess.Name)
		}
	}
	want := []string{"coder", mine.Name, "other", theirs.Name}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", names, want)
	}
}

func TestReorderAgentSkipsChildren(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	createSession(t, m, "other", dir, "backend")
	m.selectSessionRow(t, "coder")
	shell := spawnTerminal(t, m)
	m.selectSessionRow(t, "coder")
	agent := m.sessionRows()[0]
	updated, _ := m.handleKey(runeKey("J"))
	m = updated.(*Model)
	var names []string
	for _, row := range railRows(m) {
		if !row.isGroup && row.sess.ParentID == "" {
			names = append(names, row.sess.Name)
		}
	}
	if len(names) < 2 || names[0] != "other" || names[1] != "coder" {
		t.Fatalf("un-nested order %v", names)
	}
	got, err := m.services.store.Get(shell.ID)
	if err != nil || got.ParentID != agent.ID {
		t.Fatalf("terminal left its parent: %+v err %v", got, err)
	}
}

func TestHandleLiftsARowStraightFromFocus(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	updated, _ := m.focusSelected()
	m = updated.(*Model)
	m = liftByHandle(t, m, "beta")
	if selected, ok := m.selected(); m.mode != modeList || !m.rail.Reordering() || !ok || selected.Name != "beta" {
		t.Fatalf("one press on beta's handle should leave focus and lift beta, mode = %v selected = %q", m.mode, selected.Name)
	}
}

func liftByHandle(t *testing.T, m *Model, name string) *Model {
	t.Helper()
	y0, _ := m.bodyYRange()
	line := paintedRailLines(t, m, name)[0]
	updated, _ := m.handleMouse(tea.MouseMsg{X: m.displayedRail.Handles[rowKey(railRows(m)[railHitRows(m)[line]])], Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(*Model)
	if !m.rail.Reordering() {
		t.Fatalf("a press on the handle should lift %s", name)
	}
	return m
}

func sessionOrder(m *Model) []string {
	var names []string
	for _, row := range railRows(m) {
		if !row.isGroup {
			names = append(names, row.sess.Name)
		}
	}
	return names
}

func TestEveryMovableRowPaintsItsHandle(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	frame := strings.Split(ansi.Strip(preparedView(m)), "\n")
	for _, name := range []string{"alpha", "beta"} {
		y0, _ := m.bodyYRange()
		row := frame[y0+paintedRailLines(t, m, name)[0]]
		if !strings.Contains(row, reorderGrip+" "+name) {
			t.Fatalf("%s should carry the handle right before its name, got %q", name, row)
		}
	}
}

func TestHandleDragReordersAndDrops(t *testing.T) {
	m := buildModel(t)
	for _, name := range []string{"alpha", "beta", "gamma"} {
		createSession(t, m, name, t.TempDir(), "")
	}
	before := sessionOrder(m)
	m = liftByHandle(t, m, before[0])
	m = railMouse(t, m, before[2], tea.MouseActionMotion, tea.MouseButtonLeft)
	if got := sessionOrder(m); got[2] != before[0] {
		t.Fatalf("dragging onto the last row should move %s there, got %v", before[0], got)
	}
	m = railMouse(t, m, before[0], tea.MouseActionRelease, tea.MouseButtonLeft)
	if m.rail.Reordering() || m.mode != modeList {
		t.Fatalf("release after a drag should drop the row, reorder = %v mode = %v", m.rail.Reordering(), m.mode)
	}
}

func TestHandleThenKeysAndEscPutsItBack(t *testing.T) {
	m := buildModel(t)
	for _, name := range []string{"alpha", "beta", "gamma"} {
		createSession(t, m, name, t.TempDir(), "")
	}
	before := sessionOrder(m)
	m = liftByHandle(t, m, before[0])
	m = railMouse(t, m, before[0], tea.MouseActionRelease, tea.MouseButtonLeft)
	if !m.rail.Reordering() || m.mode != modeList {
		t.Fatalf("a release in place keeps the row lifted and unfocused, reorder = %v mode = %v", m.rail.Reordering(), m.mode)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if got := sessionOrder(m); got[2] != before[0] {
		t.Fatalf("down twice should move %s last, got %v", before[0], got)
	}
	preparedView(m)
	if !strings.Contains(ansi.Strip(m.viewFooter()), "Reorder") {
		t.Fatalf("footer should name the reorder mode:\n%s", ansi.Strip(m.viewFooter()))
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if got := sessionOrder(m); strings.Join(got, ",") != strings.Join(before, ",") || m.rail.Reordering() {
		t.Fatalf("esc should put the row back, got %v want %v", got, before)
	}
}

func TestAnotherHandleTakesOverALiftedRowInOnePress(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m = liftByHandle(t, m, "alpha")
	m = railMouse(t, m, "alpha", tea.MouseActionRelease, tea.MouseButtonLeft)
	if !m.rail.Reordering() {
		t.Fatal("test setup: alpha should stay lifted for the keyboard")
	}
	m = liftByHandle(t, m, "beta")
	if selected, ok := m.selected(); !m.rail.Reordering() || !ok || selected.Name != "beta" {
		t.Fatalf("one press on beta's handle should lift beta, selected = %q", selected.Name)
	}
}

func TestAPressOnALiftedRowsLabelPutsItDownAndClicks(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	m = liftByHandle(t, m, "alpha")
	m = railMouse(t, m, "alpha", tea.MouseActionRelease, tea.MouseButtonLeft)
	y0, _ := m.bodyYRange()
	at := tea.MouseMsg{X: m.displayedRail.Handles["s:"+sessionRow(t, m, "alpha").sess.ID] + 4, Y: y0 + paintedRailLines(t, m, "alpha")[0], Button: tea.MouseButtonLeft}
	at.Action = tea.MouseActionPress
	updated, _ := m.handleMouse(at)
	m = updated.(*Model)
	at.Action = tea.MouseActionRelease
	updated, _ = m.handleMouse(at)
	m = updated.(*Model)
	if m.rail.Reordering() || m.mode != modeFocus {
		t.Fatalf("a label click should drop the row and focus it, reorder = %v mode = %v", m.rail.Reordering(), m.mode)
	}
}

// Lifting a row selects it, and its preview has to follow: the handle's
// press is the only event before the drag, so its fetch cannot be dropped.
func TestLiftingARowFetchesItsPreview(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	preparedView(m)
	y0, _ := m.bodyYRange()
	line := paintedRailLines(t, m, "beta")[0]
	_, cmd := m.handleMouse(tea.MouseMsg{X: m.displayedRail.Handles["s:"+sessionRow(t, m, "beta").sess.ID], Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if !m.rail.Reordering() || cmd == nil {
		t.Fatalf("lifting beta should schedule its preview, reorder = %v cmd = %v", m.rail.Reordering(), cmd != nil)
	}
}

// The handle sits on a row's first line only: in a comfortable row the
// prompt and reply lines below it are the row's label, which a click focuses.
func TestHandleColumnBelowTheFirstLineIsTheLabel(t *testing.T) {
	m := buildModel(t)
	m.prefs.comfortableRows = true
	createSession(t, m, "alpha", t.TempDir(), "")
	preparedView(m)
	y0, _ := m.bodyYRange()
	lines := paintedRailLines(t, m, "alpha")
	if len(lines) < 2 {
		t.Fatalf("test setup: a comfortable row paints more than one line, got %v", lines)
	}
	at := tea.MouseMsg{X: m.displayedRail.Handles[rowKey(sessionRow(t, m, "alpha"))], Y: y0 + lines[1], Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	updated, _ := m.handleMouse(at)
	m = updated.(*Model)
	if m.rail.Reordering() {
		t.Fatal("a press below the first line should not lift the row")
	}
}
