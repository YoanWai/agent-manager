package ui

import (
	"testing"
	"time"
)

// The rows are marked against the server the poll read panes from, so the
// refresh has to hand that socket to the model it renders from.
func TestRefreshCarriesTheSocketItReadPanesFrom(t *testing.T) {
	for _, socket := range []string{"/tmp/first/agentmgr", "/tmp/second/agentmgr", ""} {
		m := &Model{workspace: workspace{tmuxSocket: "/tmp/stale/agentmgr"}, rail: railModelFromRows(nil, 0)}
		m.Update(refreshMsg{tmuxSocket: socket, leadingManager: true, listedAt: time.Now()})
		if m.workspace.tmuxSocket != socket {
			t.Fatalf("model socket = %q, want the poll's %q", m.workspace.tmuxSocket, socket)
		}
		if !m.workspace.leadingManager {
			t.Fatal("the poll's hold on the store should reach the model")
		}
	}
}

func TestRefreshWithStaleSelectionFetchesPreview(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "fresh-one", t.TempDir(), "")
	m.selectSessionRow(t, "fresh-one")
	sess := m.sessionRows()[0]

	_, cmd := m.Update(refreshMsg{sessions: m.workspace.sessions, procFor: ""})
	if cmd == nil {
		t.Fatal("stale refresh should schedule an immediate preview fetch")
	}
	m.applyCmd(t, cmd)
	// A fresh pane may not have drawn anything yet (psmux is slower to
	// print a prompt), so check the fetch landed for the selection rather
	// than that it carried text.
	if m.workspace.procFor != sess.ID {
		t.Fatalf("scheduled selection refresh landed for %q, want %q", m.workspace.procFor, sess.ID)
	}

	m.workspace.preview = "existing"
	if _, cmd := m.Update(refreshMsg{sessions: m.workspace.sessions, procFor: sess.ID, preview: "pane text"}); cmd != nil {
		t.Fatal("matching refresh should not schedule extra work")
	}
	if m.workspace.preview != "pane text" {
		t.Fatalf("preview = %q want %q", m.workspace.preview, "pane text")
	}
}
