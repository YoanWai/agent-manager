package ui

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/store"
	uigeometrypanes "github.com/YoanWai/agent-manager/internal/ui/geometry/panes"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	uirailpersist "github.com/YoanWai/agent-manager/internal/ui/rail/persist"
	uilifecycle "github.com/YoanWai/agent-manager/internal/ui/session/lifecycle"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestCursorWrapsAroundTheList(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	createSession(t, m, "first", dir, "")
	createSession(t, m, "second", dir, "")

	setRailCursor(m, 0)
	m.moveCursor(-1)
	if m.rail.Cursor() != len(railRows(m))-1 {
		t.Fatalf("up from the top should wrap to the bottom, cursor = %d", m.rail.Cursor())
	}
	m.moveCursor(1)
	if m.rail.Cursor() != 0 {
		t.Fatalf("down from the bottom should wrap to the top, cursor = %d", m.rail.Cursor())
	}

	resetRailModel(m)
	m.moveCursor(1)
	if m.rail.Cursor() != 0 {
		t.Fatalf("empty list should leave the cursor alone, cursor = %d", m.rail.Cursor())
	}
}

// Right steps into the row under the cursor: a session is focused, and a
// collapsed group opens without the toggle closing an open one.
func TestRightStepsIntoTheRow(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if err := m.services.store.CreateGroup("grouped", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "stepin", t.TempDir(), "grouped")
	m.selectGroupRow(t, "grouped")

	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	*m = *updated.(*Model)
	if !m.rail.IsCollapsed("grouped") {
		t.Fatal("left did not close the group")
	}
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	*m = *updated.(*Model)
	if m.rail.IsCollapsed("grouped") {
		t.Fatal("right did not open the group")
	}
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	*m = *updated.(*Model)
	if m.rail.IsCollapsed("grouped") {
		t.Fatal("a second right closed the group it had opened")
	}

	m.selectSessionRow(t, "stepin")
	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	*m = *updated.(*Model)
	m.drainEffects(t)
	if !m.focused() {
		t.Fatalf("right did not focus the session, screen = %v, err = %q", m.screenID(), m.statusbar.Text())
	}
}

func TestNamesContainingTheHandleStillSelectAndDrag(t *testing.T) {
	m := buildModel(t)
	name := "界e\u0301 " + m.glyphs().ReorderGrip + " notes"
	createSession(t, m, name, t.TempDir(), "")
	createSession(t, m, "other", t.TempDir(), "")
	m.View()
	line := paintedRailLines(t, m, name)[0]
	y0, _ := m.bodyYRange()
	frame := strings.Split(ansi.Strip(m.View()), "\n")
	head := frame[y0+line]
	nameAt := strings.Index(head, name)
	if nameAt < 0 {
		t.Fatalf("row lost its Unicode name: %q", head)
	}
	x := ansi.StringWidth(head[:nameAt]) + ansi.StringWidth("界e\u0301 ")
	m.handleMouse(tea.MouseMsg{X: x, Y: y0 + line, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if m.rail.Reordering() {
		t.Fatal("clicking punctuation in the name lifted the row")
	}
	m.handleMouse(tea.MouseMsg{X: x, Y: y0 + line, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	m = liftByHandle(t, m, name)
	if !m.rail.Reordering() || railSelectedSession(m).Name != name {
		t.Fatal("the real handle did not lift the named row")
	}
}

func TestArchivedViewIgnoresFold(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()

	if err := m.services.store.CreateGroup("work", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "alpha", dir, "work")

	m.selectSessionRow(t, "alpha")
	m.archiveSelected()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.drainEffects(t)
	m.applyCmd(t, cmd)

	m.rail.SetCollapsed("work", true)

	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	if len(m.sessionRows()) != 1 {
		t.Fatalf("archived session inside a folded group should still show, got %d rows", len(m.sessionRows()))
	}

	m.selectSessionRow(t, "alpha")
	m.restoreSelected()
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.drainEffects(t)
	m.applyCmd(t, cmd)

	active, err := m.services.store.ListSessions(false)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("after restore, active sessions in store = %d want 1", len(active))
	}
}

func TestCollapsedStatePersistsAcrossReload(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.rail.SetCollapsed("backend", true)
	m.rail.SetCollapsed("backend/api", true)
	if err := m.persistCollapsed(m.rail.Collapsed()); err != nil {
		t.Fatalf("persist collapsed: %v", err)
	}
	if restored := uirailpersist.Load(m.services.store); !restored.IsCollapsed("backend/api") {
		t.Fatal("a reload lost the saved folds")
	}

	m.rail.SetCollapsed("backend", false)
	if err := m.persistCollapsed(m.rail.Collapsed()); err != nil {
		t.Fatalf("persist expanded state: %v", err)
	}
	restored := uirailpersist.Load(m.services.store)
	if restored.IsCollapsed("backend") {
		t.Fatal("expanded group leaked back as collapsed")
	}
	if !restored.IsCollapsed("backend/api") {
		t.Fatal("still-folded group dropped")
	}
}

func TestToggleCollapseAllFlipsEveryGroup(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.workspace.sessions = []store.Session{{ID: "a", Group: "backend/api"}, {ID: "b", Group: "frontend"}}
	m.rebuildRows()
	want := []string{"backend", "backend/api", "frontend"}

	updated, _ := m.handleKey(runeKey("F"))
	m.drainEffects(t)
	m = updated.(*Model)
	for _, group := range want {
		if !m.rail.IsCollapsed(group) {
			t.Fatalf("group %q not collapsed after fold-all", group)
		}
	}
	if restored := uirailpersist.Collapsed(m.services.store); len(restored) != 3 {
		t.Fatalf("fold-all not persisted: %v", restored)
	}

	updated, _ = m.handleKey(runeKey("F"))
	m.drainEffects(t)
	m = updated.(*Model)
	for _, group := range want {
		if m.rail.IsCollapsed(group) {
			t.Fatalf("group %q still collapsed after unfold-all", group)
		}
	}
	if restored := uirailpersist.Collapsed(m.services.store); len(restored) != 0 {
		t.Fatalf("unfold-all not persisted: %v", restored)
	}
}

func TestRailStateDecisionPreservesLatestCollapseSnapshot(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"reveal", "rename"} {
		t.Run(scenario, func(t *testing.T) {
			m := buildModel(t)
			m.queueRail([]uirail.Mutation{{Kind: uirail.SaveCollapsed, Collapsed: []string{"initial"}}}, 0, false)
			m.nextEffectCmd()
			m.rail.SetCollapsed("parent", true)
			m.queueRail([]uirail.Mutation{{Kind: uirail.SaveCollapsed, Collapsed: m.rail.Collapsed()}}, 0, false)
			var expected []string
			if scenario == "rename" {
				m.queueRail(m.rail.RenameGroup("parent", "renamed").Mutations, 0, true)
				expected = []string{"renamed"}
			} else {
				m.queueRail(m.rail.RevealGroup("parent/child").Mutations, 0, true)
			}
			m.drainEffects(t)
			raw, err := m.services.store.Setting(uirailpersist.CollapsedSetting)
			if err != nil {
				t.Fatal(err)
			}
			var stored []string
			if err := json.Unmarshal([]byte(raw), &stored); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(stored, expected) {
				t.Fatalf("latest %s preference overwritten: got %v, want %v", scenario, stored, expected)
			}
		})
	}
}

func TestPrioritizedCollapseSaveKeepsActiveAndUnrelatedJobs(t *testing.T) {
	m := &Model{}
	m.queueRail([]uirail.Mutation{{Kind: uirail.SaveCollapsed}}, 0, false)
	m.nextEffectCmd()
	active := m.effects.Main.Active
	m.enqueueEffect(uigeometrypanes.ResizeRequest{}, 0, false)
	geometry := m.effects.Main.Pending[0]
	m.queueRail([]uirail.Mutation{{Kind: uirail.SaveCollapsed}}, 0, false)
	m.enqueueEffect(uilifecycle.Request{}, 0, false)
	lifecycle := m.effects.Main.Pending[2]
	m.queueRail([]uirail.Mutation{{Kind: uirail.SaveCollapsed, Collapsed: []string{"latest"}}}, 0, true)
	if m.effects.Main.Active != active || len(m.effects.Main.Pending) != 3 || m.effects.Main.Pending[1] != geometry || m.effects.Main.Pending[2] != lifecycle {
		t.Fatal("superseding snapshots changed active work or unrelated job order")
	}
}
