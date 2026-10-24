package ui

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/remote"
	uiconnections "github.com/YoanWai/agent-manager/internal/ui/connections"
	uilink "github.com/YoanWai/agent-manager/internal/ui/connections/link"
)

func TestClicksOnTheConnectionDialog(t *testing.T) {
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 40
	m.ssh = uilink.New(testRemoteClient(t), nil)
	m.openConnectionDialog(remote.Connection{})
	for _, tc := range []struct {
		label string
		field int
	}{{"binary ", uiconnections.BinaryField}, {"ssh to ", uiconnections.DestinationField}, {"name ", uiconnections.NameField}} {
		x, y := bodyCell(t, m, tc.label)
		m = leftClick(m, x, y)
		if !m.connectionDialogOpen() || m.connectionSurface().Dialog().Focus() != tc.field {
			t.Fatalf("a click on %q: open %v", tc.label, m.connectionDialogOpen())
		}
	}
	x, y := paintedCell(t, m, "esc")
	if m = leftClick(m, x, y); m.connectionDialogOpen() {
		t.Fatal("a click on the esc hint left the card open")
	}
}
