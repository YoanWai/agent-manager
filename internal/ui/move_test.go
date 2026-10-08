package ui

import (
	"reflect"
	"testing"

	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	tea "github.com/charmbracelet/bubbletea"
)

// handleMoveKey drives the move dialog with the root as its host, the way
// the key dispatch does.
func (m *Model) handleMoveKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	return m, m.move.handleKey(m, msg)
}

// fakeMoveHost stands in for the root's borrowed group picker and effect
// lane.
type fakeMoveHost struct {
	mode     mode
	options  []groupOption
	index    int
	moves    []uirail.Mutation
	closes   []moveDialogClose
	cursored int
}

func (h *fakeMoveHost) setMode(next mode) { h.mode = next }
func (h *fakeMoveHost) moveGroupCursor(delta int) tea.Cmd {
	h.cursored += delta
	h.index = (h.index + delta + len(h.options)) % len(h.options)
	return nil
}
func (h *fakeMoveHost) pickedGroupOption() groupOption { return h.options[h.index] }
func (h *fakeMoveHost) selectedGroupPath() string      { return h.options[h.index].path }
func (h *fakeMoveHost) enqueueMove(mut uirail.Mutation, close moveDialogClose) tea.Cmd {
	h.moves = append(h.moves, mut)
	h.closes = append(h.closes, close)
	return func() tea.Msg { return nil }
}
func (h *fakeMoveHost) card(title, body string, hint [][2]string) string { return title + "|" + body }
func (h *fakeMoveHost) viewGroupPicker() string                          { return "picker" }

func TestMoveDialogWithFakeHost(t *testing.T) {
	h := &fakeMoveHost{mode: modeMove, options: []groupOption{{path: ""}, {path: "work", depth: 1}, {path: "work", depth: 2, sessID: "parent", name: "agent"}}}
	d := moveDialog{moveTarget{id: "shell"}}
	if got := d.view(h); got != "⇄ Move|picker" {
		t.Fatalf("view = %q", got)
	}
	d.handleKey(h, tea.KeyMsg{Type: tea.KeyDown})
	d.handleKey(h, tea.KeyMsg{Type: tea.KeyDown})
	if cmd := d.handleKey(h, tea.KeyMsg{Type: tea.KeyEnter}); cmd == nil {
		t.Fatal("enter queued nothing")
	}
	want := uirail.Mutation{Kind: uirail.PlaceSession, SessionID: "shell", Group: "work", ParentID: "parent"}
	if len(h.moves) != 1 || !reflect.DeepEqual(h.moves[0], want) || h.closes[0].optSessID != "parent" {
		t.Fatalf("moves = %+v closes = %+v, want the session placed under its picked agent", h.moves, h.closes)
	}

	group := moveDialog{moveTarget{path: "work/sub"}}
	h = &fakeMoveHost{mode: modeMove, options: []groupOption{{path: ""}, {path: "work", depth: 1}}, index: 1}
	if cmd := group.handleKey(h, tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil || h.mode != modeList || len(h.moves) != 0 {
		t.Fatalf("moving a group onto its own parent should just close: mode %v moves %+v", h.mode, h.moves)
	}
	h = &fakeMoveHost{mode: modeMove, options: []groupOption{{path: ""}, {path: "work", depth: 1}}}
	group.handleKey(h, tea.KeyMsg{Type: tea.KeyEnter})
	if len(h.moves) != 1 || !reflect.DeepEqual(h.moves[0], uirail.Mutation{Kind: uirail.MoveGroup, Path: "work/sub", Group: ""}) {
		t.Fatalf("moves = %+v, want the group moved to root", h.moves)
	}
	d.handleKey(h, tea.KeyMsg{Type: tea.KeyEsc})
	if h.mode != modeList {
		t.Fatalf("esc left mode %v", h.mode)
	}
}

func TestMoveSession(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.services.store.CreateGroup("target/deep", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "wanderer", dir, "")

	m.selectSessionRow(t, "wanderer")
	m.openMove()
	if m.mode != modeMove {
		t.Fatal("openMove should enter move mode")
	}
	pickGroup(t, m, "target/deep")
	_, cmd := m.handleMoveKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)

	sessions := m.sessionRows()
	if len(sessions) != 1 || sessions[0].Group != "target/deep" {
		t.Fatalf("move failed: %+v", sessions)
	}
}

func TestMoveGroupRow(t *testing.T) {
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
	if m.mode != modeMove {
		t.Fatal("openMove on a group row should enter move mode")
	}
	for _, opt := range m.form.groups {
		if opt.path == "alpha/inner" {
			t.Fatal("picker offers the moved group itself")
		}
	}
	pickGroup(t, m, "beta")
	_, cmd := m.handleMoveKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)

	sessions := m.sessionRows()
	if len(sessions) != 1 || sessions[0].Group != "beta/inner" {
		t.Fatalf("group move failed: %+v", sessions)
	}
	if m.mode != modeList {
		t.Fatal("should return to list mode")
	}
}

func TestMoveGroupRowExcludesDescendants(t *testing.T) {
	m := buildModel(t)
	if err := m.services.store.CreateGroup("alpha/inner/deep", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())

	m.selectGroupRow(t, "alpha")
	m.openMove()
	for _, opt := range m.form.groups {
		if opt.path == "alpha" || opt.path == "alpha/inner" || opt.path == "alpha/inner/deep" {
			t.Fatalf("picker offers %q inside the moved subtree", opt.path)
		}
	}
}

func TestMoveTerminalOntoAgentNests(t *testing.T) {
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
	for i, opt := range m.form.groups {
		if opt.sessID == agent.ID {
			m.form.groupIndex = i
			break
		}
	}
	if m.form.groups[m.form.groupIndex].sessID != agent.ID {
		t.Fatal("picker must list the agent")
	}
	_, cmd := m.handleMoveKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	got, err := m.services.store.Get(shell.ID)
	if err != nil || got.ParentID != agent.ID {
		t.Fatalf("nested = %+v err %v", got, err)
	}
}

func TestMoveTerminalOntoGroupUnnests(t *testing.T) {
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
	_, cmd := m.handleMoveKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	got, _ := m.services.store.Get(shell.ID)
	if got.ParentID != "" || got.Group != "other" {
		t.Fatalf("unnest = %+v", got)
	}
}

func TestMoveReportsPlacementFailure(t *testing.T) {
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
	for m.form.groups[m.form.groupIndex].sessID != agent.ID {
		m.form.groupIndex++
		if m.form.groupIndex >= len(m.form.groups) {
			t.Fatal("picker must list the agent")
		}
	}
	if err := m.services.store.Delete(agent.ID); err != nil {
		t.Fatalf("delete agent: %v", err)
	}
	_, cmd := m.handleMoveKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	if m.errBar.text == "" {
		t.Fatal("failed placement reported nothing")
	}
	if m.mode != modeMove {
		t.Fatalf("mode = %v, want modeMove", m.mode)
	}
	got, err := m.services.store.Get(shell.ID)
	if err != nil || got.ParentID != agent.ID {
		t.Fatalf("shell left its parent: %+v err %v", got, err)
	}
}

func TestMoveReportsMissingSource(t *testing.T) {
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
	_, cmd := m.handleMoveKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	if m.errBar.text == "" {
		t.Fatal("missing source reported nothing")
	}
	if m.mode != modeMove {
		t.Fatalf("mode = %v, want modeMove", m.mode)
	}
}

func TestMovePickerKeepsTheTerminalsOwnGroupSelected(t *testing.T) {
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
	if sessID := m.form.groups[m.form.groupIndex].sessID; sessID != "" {
		t.Fatalf("selected option targets session %q", sessID)
	}
}

func TestMoveAgentPickerHasNoSessionTargets(t *testing.T) {
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
	for _, opt := range m.form.groups {
		if opt.sessID != "" {
			t.Fatalf("agent move listed session %q", opt.sessID)
		}
	}
}

func TestMoveCompletionDoesNotCloseReopenedSameTargetDialog(t *testing.T) {
	m := buildModel(t)
	if err := m.services.store.CreateGroup("target", ""); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "wanderer", t.TempDir(), "")
	m.selectSessionRow(t, "wanderer")
	m.openMove()
	pickGroup(t, m, "target")
	_, cmd := m.handleMoveKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("move returned no effect command")
	}
	_, _ = m.handleMoveKey(tea.KeyMsg{Type: tea.KeyEsc})
	m.openMove()
	m.applyCmd(t, cmd)
	if m.mode != modeMove {
		t.Fatal("older move completion closed the reopened dialog")
	}
	got, err := m.services.store.Get(m.move.id)
	if err != nil || got.Group != "target" {
		t.Fatalf("accepted move did not reconcile: %+v, %v", got, err)
	}
}
