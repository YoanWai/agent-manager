package ui

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/store"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	tea "github.com/charmbracelet/bubbletea"
)

// typeConnection types a name and a destination into the open dialog.
func typeConnection(m *Model, name, destination string) {
	m.handleConnectionKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)})
	m.handleConnectionKey(tea.KeyMsg{Type: tea.KeyTab})
	m.handleConnectionKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(destination)})
}

func TestNewConnectionKeyOpensTheDialogAndSavesToTheStore(t *testing.T) {
	m := buildModel(t)
	m.ssh = newConnections(testRemoteClient(t), nil)
	m.applyTestMsg(t, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("C")})
	if m.mode != modeConnection {
		t.Fatalf("C left mode %v", m.mode)
	}
	typeConnection(m, "box", "me@box")
	_, cmd := m.handleConnectionKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyTestMsg(t, cmd())
	m.drainEffects(t)
	stored, err := m.services.store.Connections()
	if err != nil || len(stored) != 1 || stored[0] != (store.Connection{Name: "box", Destination: "me@box"}) {
		t.Fatalf("stored = %+v, %v", stored, err)
	}
	if m.mode != modeList || len(m.ssh.List()) != 1 {
		t.Fatalf("mode %v list %+v", m.mode, m.ssh.List())
	}
	if selection, _ := m.rail.Selected(); selection != (uirail.Selection{Kind: uirail.ConnectionRow, Host: "box"}) {
		t.Fatalf("selection = %+v, want the new connection", selection)
	}

	_, cmd = m.runRailIntent(uirail.Intent{Kind: uirail.Delete, Target: uirail.Selection{Kind: uirail.ConnectionRow, Host: "box"}})
	if cmd != nil || m.mode != modeConfirmDelete || m.confirm.label != "remove connection box? Its sessions keep running on the host." {
		t.Fatalf("delete: mode %v label %q", m.mode, m.confirm.label)
	}
	_, cmd = m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	m.applyTestMsg(t, cmd())
	m.drainEffects(t)
	if stored, _ := m.services.store.Connections(); len(stored) != 0 || len(m.ssh.List()) != 0 {
		t.Fatalf("stored %+v list %+v after removal", stored, m.ssh.List())
	}
}

func TestAFailedConnectionSaveTakesEnterAgain(t *testing.T) {
	m := buildModel(t)
	m.ssh = newConnections(testRemoteClient(t), nil)
	if err := m.services.store.AddConnection(store.Connection{Name: "box", Destination: "me@elsewhere"}); err != nil {
		t.Fatal(err)
	}
	m.applyTestMsg(t, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("C")})
	typeConnection(m, "box", "me@box")
	_, cmd := m.handleConnectionKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyTestMsg(t, cmd())
	m.drainEffects(t)
	if m.mode != modeConnection {
		t.Fatalf("after a refused save: mode %v", m.mode)
	}
	m.handleConnectionKey(tea.KeyMsg{Type: tea.KeyShiftTab})
	m.handleConnectionKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	_, cmd = m.handleConnectionKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter after a refused save did nothing")
	}
}
