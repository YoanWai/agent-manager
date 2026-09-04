package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestMoveSession(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("target/deep", ""); err != nil {
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
	_, cmd := m.handleMoveKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.applyCmd(t, cmd)

	sessions := m.sessionRows()
	if len(sessions) != 1 || sessions[0].Group != "target/deep" {
		t.Fatalf("move failed: %+v", sessions)
	}
}

func TestMoveGroupRow(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("alpha/inner", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := m.store.CreateGroup("beta", ""); err != nil {
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
	_, cmd := m.handleMoveKey(tea.KeyPressMsg{Code: tea.KeyEnter})
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
	if err := m.store.CreateGroup("alpha/inner/deep", ""); err != nil {
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
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	m.selectGroupRow(t, "backend")
	shell := spawnTerminal(t, m)
	m.selectSessionRow(t, shell.Name)
	m.openMove()
	agent, _ := m.store.Get(m.sessionRows()[0].ID)
	for i, opt := range m.form.groups {
		if opt.sessID == agent.ID {
			m.form.groupIndex = i
			break
		}
	}
	if m.form.groups[m.form.groupIndex].sessID != agent.ID {
		t.Fatal("picker must list the agent")
	}
	_, cmd := m.handleMoveKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.applyCmd(t, cmd)
	got, err := m.store.Get(shell.ID)
	if err != nil || got.ParentID != agent.ID {
		t.Fatalf("nested = %+v err %v", got, err)
	}
}

func TestMoveTerminalOntoGroupUnnests(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	if err := m.store.CreateGroup("other", dir); err != nil {
		t.Fatalf("other: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	m.selectSessionRow(t, "coder")
	shell := spawnTerminal(t, m)
	m.selectSessionRow(t, shell.Name)
	m.openMove()
	pickGroup(t, m, "other")
	_, cmd := m.handleMoveKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.applyCmd(t, cmd)
	got, _ := m.store.Get(shell.ID)
	if got.ParentID != "" || got.Group != "other" {
		t.Fatalf("unnest = %+v", got)
	}
}

func TestMoveReportsPlacementFailure(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
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
	if err := m.store.Delete(agent.ID); err != nil {
		t.Fatalf("delete agent: %v", err)
	}
	_, cmd := m.handleMoveKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.applyCmd(t, cmd)
	if m.errBar.text == "" {
		t.Fatal("failed placement reported nothing")
	}
	if m.mode != modeMove {
		t.Fatalf("mode = %v, want modeMove", m.mode)
	}
	got, err := m.store.Get(shell.ID)
	if err != nil || got.ParentID != agent.ID {
		t.Fatalf("shell left its parent: %+v err %v", got, err)
	}
}

func TestMoveReportsMissingSource(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "coder", dir, "backend")
	m.selectSessionRow(t, "coder")
	shell := spawnTerminal(t, m)
	m.selectSessionRow(t, shell.Name)
	m.openMove()
	if err := m.store.Delete(shell.ID); err != nil {
		t.Fatalf("delete shell: %v", err)
	}
	_, cmd := m.handleMoveKey(tea.KeyPressMsg{Code: tea.KeyEnter})
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
		if err := m.store.CreateGroup(group, dir); err != nil {
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
	if err := m.store.CreateGroup("backend", dir); err != nil {
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
