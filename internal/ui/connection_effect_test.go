package ui

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/remote"
	"github.com/YoanWai/agent-manager/internal/store"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	tea "github.com/charmbracelet/bubbletea"
)

func TestNewConnectionKeyOpensTheDialogAndSavesToTheStore(t *testing.T) {
	m := buildModel(t)
	m.ssh = newConnections(remote.New(t.TempDir()), nil)
	m.applyTestMsg(t, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("C")})
	if m.mode != modeConnection {
		t.Fatalf("C left mode %v", m.mode)
	}
	m.ssh.dialog.name.SetValue("box")
	m.ssh.dialog.destination.SetValue("me@box")
	_, cmd := m.handleConnectionKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyTestMsg(t, cmd())
	m.drainEffects(t)
	stored, err := m.services.store.Connections()
	if err != nil || len(stored) != 1 || stored[0] != (store.Connection{Name: "box", Destination: "me@box"}) {
		t.Fatalf("stored = %+v, %v", stored, err)
	}
	if m.mode != modeList || len(m.ssh.list) != 1 {
		t.Fatalf("mode %v list %+v", m.mode, m.ssh.list)
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
	if stored, _ := m.services.store.Connections(); len(stored) != 0 || len(m.ssh.list) != 0 {
		t.Fatalf("stored %+v list %+v after removal", stored, m.ssh.list)
	}
}
