package ui

import (
	"github.com/YoanWai/agent-manager/internal/hooks"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The rows are marked against the server the poll read panes from, so the
// refresh has to hand that socket to the model it renders from.
func TestRefreshCarriesTheSocketItReadPanesFrom(t *testing.T) {
	for _, socket := range []string{"/tmp/first/agentmgr", "/tmp/second/agentmgr", ""} {
		m := &Model{workspace: workspace{tmuxSocket: "/tmp/stale/agentmgr"}, rail: railState{collapsed: map[string]bool{}}}
		m.Update(refreshMsg{tmuxSocket: socket, leadingManager: true, listedAt: time.Now()})
		if m.workspace.tmuxSocket != socket {
			t.Fatalf("model socket = %q, want the poll's %q", m.workspace.tmuxSocket, socket)
		}
		if !m.workspace.leadingManager {
			t.Fatal("the poll's hold on the store should reach the model")
		}
	}
}

// writeName queues a rename the way the subcommand does, and returns the
// request its answer comes back under.
func writeName(t *testing.T, m *Model, id, name string) string {
	t.Helper()
	path := m.services.hooks.NameFile(id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("hooks dir: %v", err)
	}
	request, err := hooks.NewRequestID()
	if err != nil {
		t.Fatalf("request id: %v", err)
	}
	if err := os.WriteFile(path, []byte(hooks.NameRequest(request, name)), 0o644); err != nil {
		t.Fatalf("write name file: %v", err)
	}
	return request
}

func TestRefreshWithStaleSelectionFetchesPreview(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "fresh-one", t.TempDir(), "")
	m.selectSessionRow(t, "fresh-one")
	sess := m.sessionRows()[0]

	_, cmd := m.Update(refreshMsg{sessions: m.workspace.sessions, procFor: ""})
	if cmd == nil {
		t.Fatal("stale refresh should schedule an immediate preview fetch")
	}
	m.applyCmd(t, cmd)
	if m.workspace.preview == "" {
		t.Fatal("scheduled selection refresh produced no preview")
	}

	m.workspace.preview = "existing"
	if _, cmd := m.Update(refreshMsg{sessions: m.workspace.sessions, procFor: sess.ID, preview: "pane text"}); cmd != nil {
		t.Fatal("matching refresh should not schedule extra work")
	}
	if m.workspace.preview != "pane text" {
		t.Fatalf("preview = %q want %q", m.workspace.preview, "pane text")
	}
}
