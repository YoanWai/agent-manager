package ui

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/remote"
	uiconnections "github.com/YoanWai/agent-manager/internal/ui/connections"
)

func TestClicksOnTheConnectionDialog(t *testing.T) {
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 40
	m.ssh = newConnections(testRemoteClient(t), nil)
	m.openConnectionDialog(remote.Connection{})
	for _, tc := range []struct {
		label string
		field int
	}{{"binary ", uiconnections.BinaryField}, {"ssh to ", uiconnections.DestinationField}, {"name ", uiconnections.NameField}} {
		x, y := bodyCell(t, m, tc.label)
		m = leftClick(m, x, y)
		if m.mode != modeConnection || m.ssh.Dialog.Focus() != tc.field {
			t.Fatalf("a click on %q: mode %v, focus %d", tc.label, m.mode, m.ssh.Dialog.Focus())
		}
	}
	x, y := paintedCell(t, m, "esc")
	if m = leftClick(m, x, y); m.mode != modeList {
		t.Fatalf("a click on the esc hint left mode %v", m.mode)
	}
}
