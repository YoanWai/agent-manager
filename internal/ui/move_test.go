package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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
