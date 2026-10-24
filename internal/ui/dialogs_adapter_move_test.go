package ui

import (
	"testing"

	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	tea "github.com/charmbracelet/bubbletea"
)

func moveOpen(m *Model) bool {
	_, ok := surfaceOf[*uidialogs.MoveSurface](m)
	return ok
}

// movingSession is the session the open card is placing.
func movingSession(m *Model) string {
	if s, ok := surfaceOf[*uidialogs.MoveSurface](m); ok {
		return s.SessionID()
	}
	return ""
}

func TestMoveSession(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("target/deep", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "wanderer", dir, "")

	m.selectSessionRow(t, "wanderer")
	m.openMove()
	if !moveOpen(m) {
		t.Fatal("openMove should enter move mode")
	}
	pickGroup(t, m, "target/deep")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)

	sessions := m.sessionRows()
	if len(sessions) != 1 || sessions[0].Group != "target/deep" {
		t.Fatalf("move failed: %+v", sessions)
	}
}

func TestMoveGroupRow(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("alpha/inner", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := m.services.store.CreateGroup("beta", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "wanderer", dir, "alpha/inner")

	m.selectGroupRow(t, "alpha/inner")
	m.openMove()
	if !moveOpen(m) {
		t.Fatal("openMove on a group row should enter move mode")
	}
	for _, opt := range picker(m).Groups {
		if opt.Path == "alpha/inner" {
			t.Fatal("picker offers the moved group itself")
		}
	}
	pickGroup(t, m, "beta")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)

	sessions := m.sessionRows()
	if len(sessions) != 1 || sessions[0].Group != "beta/inner" {
		t.Fatalf("group move failed: %+v", sessions)
	}
	if !m.onList() {
		t.Fatal("should return to list mode")
	}
}

func TestMoveGroupRowExcludesDescendants(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if err := m.services.store.CreateGroup("alpha/inner/deep", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())

	m.selectGroupRow(t, "alpha")
	m.openMove()
	for _, opt := range picker(m).Groups {
		if opt.Path == "alpha" || opt.Path == "alpha/inner" || opt.Path == "alpha/inner/deep" {
			t.Fatalf("picker offers %q inside the moved subtree", opt.Path)
		}
	}
}

func TestMoveTerminalOntoAgentNests(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	m.selectGroupRow(t, "backend")
	shell := spawnTerminal(t, m)
	m.selectSessionRow(t, shell.Name)
	m.openMove()
	agent, _ := m.services.store.Get(m.sessionRows()[0].ID)
	for i, opt := range picker(m).Groups {
		if opt.SessionID == agent.ID {
			picker(m).GroupIndex = i
			break
		}
	}
	if picker(m).Groups[picker(m).GroupIndex].SessionID != agent.ID {
		t.Fatal("picker must list the agent")
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	got, err := m.services.store.Get(shell.ID)
	if err != nil || got.ParentID != agent.ID {
		t.Fatalf("nested = %+v err %v", got, err)
	}
}

func TestMoveTerminalOntoGroupUnnests(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	if err := m.services.store.CreateGroup("other", dir); err != nil {
		t.Fatalf("other: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	m.selectSessionRow(t, "coder")
	shell := spawnTerminal(t, m)
	m.selectSessionRow(t, shell.Name)
	m.openMove()
	pickGroup(t, m, "other")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	got, _ := m.services.store.Get(shell.ID)
	if got.ParentID != "" || got.Group != "other" {
		t.Fatalf("unnest = %+v", got)
	}
}

func TestMoveReportsPlacementFailure(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	m.selectSessionRow(t, "coder")
	agent := m.sessionRows()[0]
	shell := spawnTerminal(t, m)
	m.applyCmd(t, m.refreshCmd())
	m.selectSessionRow(t, shell.Name)
	m.openMove()
	for picker(m).Groups[picker(m).GroupIndex].SessionID != agent.ID {
		picker(m).GroupIndex++
		if picker(m).GroupIndex >= len(picker(m).Groups) {
			t.Fatal("picker must list the agent")
		}
	}
	if err := m.services.store.Delete(agent.ID); err != nil {
		t.Fatalf("delete agent: %v", err)
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	if m.errBar.text == "" {
		t.Fatal("failed placement reported nothing")
	}
	if !moveOpen(m) {
		t.Fatalf("screen = %v, want the move card", m.screenID())
	}
	got, err := m.services.store.Get(shell.ID)
	if err != nil || got.ParentID != agent.ID {
		t.Fatalf("shell left its parent: %+v err %v", got, err)
	}
}

func TestMoveReportsMissingSource(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	m.selectSessionRow(t, "coder")
	shell := spawnTerminal(t, m)
	m.selectSessionRow(t, shell.Name)
	m.openMove()
	if err := m.services.store.Delete(shell.ID); err != nil {
		t.Fatalf("delete shell: %v", err)
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	if m.errBar.text == "" {
		t.Fatal("missing source reported nothing")
	}
	if !moveOpen(m) {
		t.Fatalf("screen = %v, want the move card", m.screenID())
	}
}

func TestMovePickerKeepsTheTerminalsOwnGroupSelected(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	for _, group := range []string{"alpha", "beta"} {
		if err := m.services.store.CreateGroup(group, dir); err != nil {
			t.Fatalf("group %s: %v", group, err)
		}
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "beta")
	shell := spawnTerminal(t, m)
	m.selectSessionRow(t, shell.Name)
	m.openMove()
	if m.selectedGroupPath() != "beta" {
		t.Fatalf("selected group = %q, want beta", m.selectedGroupPath())
	}
	if sessID := picker(m).Groups[picker(m).GroupIndex].SessionID; sessID != "" {
		t.Fatalf("selected option targets session %q", sessID)
	}
}

func TestMoveAgentPickerHasNoSessionTargets(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	createSession(t, m, "other", dir, "backend")
	m.selectSessionRow(t, "coder")
	m.openMove()
	for _, opt := range picker(m).Groups {
		if opt.SessionID != "" {
			t.Fatalf("agent move listed session %q", opt.SessionID)
		}
	}
}

func TestMoveCompletionDoesNotCloseReopenedSameTargetDialog(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if err := m.services.store.CreateGroup("target", ""); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "wanderer", t.TempDir(), "")
	m.selectSessionRow(t, "wanderer")
	m.openMove()
	moved := movingSession(m)
	pickGroup(t, m, "target")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("move returned no effect command")
	}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m.openMove()
	m.applyCmd(t, cmd)
	if !moveOpen(m) {
		t.Fatal("older move completion closed the reopened dialog")
	}
	got, err := m.services.store.Get(moved)
	if err != nil || got.Group != "target" {
		t.Fatalf("accepted move did not reconcile: %+v, %v", got, err)
	}
}

func openMoveFor(t *testing.T) *Model {
	t.Helper()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	createSession(t, m, "wanderer", t.TempDir(), "")
	m.selectSessionRow(t, "wanderer")
	m.openMove()
	if !moveOpen(m) {
		t.Fatalf("test setup did not open the card, err %q", m.errBar.text)
	}
	return m
}

func TestMoveClosesOnEscAndBackdrop(t *testing.T) {
	for name, close := range map[string]func(*Model) *Model{
		"esc":      func(m *Model) *Model { m.Update(tea.KeyMsg{Type: tea.KeyEsc}); return m },
		"backdrop": func(m *Model) *Model { return clickBackdrop(t, m) },
	} {
		m := openMoveFor(t)
		if !mustTop(t, m).Dismissible() {
			t.Fatalf("%s: the move card holds no draft and must be dismissible", name)
		}
		m = close(m)
		if moveOpen(m) || m.effects.Quitting || !m.onList() {
			t.Errorf("%s: open=%t quitting=%t screen = %v, want the card closed and the manager running", name, moveOpen(m), m.effects.Quitting, m.screenID())
		}
	}
}

// A close follow-up from a card that was popped, or replaced by a newer
// one, leaves the screen alone.
func TestStaleMoveCloseIsDropped(t *testing.T) {
	m := openMoveFor(t)
	stale := moveDialogClose{gen: m.gens.dialog}
	m.Pop()
	m.applyMoveDialogClose(stale)
	if moveOpen(m) || len(m.surfaces) != 0 {
		t.Fatal("a stale close touched the stack")
	}
	m.openMove()
	m.applyMoveDialogClose(stale)
	if !moveOpen(m) {
		t.Fatal("a close from an older card closed the newer one")
	}
	m.applyMoveDialogClose(moveDialogClose{gen: m.gens.dialog})
	if moveOpen(m) {
		t.Fatal("the close of the open card did not close it")
	}
}
