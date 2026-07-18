package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
)

func writeCodexRollout(t *testing.T, path, sessionID, cwd string, modTime time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"session_meta","payload":{"session_id":"` + sessionID + `","cwd":"` + cwd + `"}}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatal(err)
	}
}

// Two codex sessions share a directory, A launched before B. The poller
// receives them in store order (B first), not launch order. Capture must
// still bind each to its own conversation instead of swapping them.
func TestCaptureAgentSessionIDsAssignsInLaunchOrder(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	cwd := t.TempDir()

	base := time.Now().Add(-time.Minute)
	writeCodexRollout(t, filepath.Join(codexHome, "sessions", "rollout-A.jsonl"), "A-id", cwd, base)
	writeCodexRollout(t, filepath.Join(codexHome, "sessions", "rollout-B.jsonl"), "B-id", cwd, base.Add(2*time.Second))

	if err := st.CreateSession(store.Session{ID: "sess-A", Name: "a", Tool: "codex", Cwd: cwd, Group: "g", Status: "idle", CreatedAt: base}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(store.Session{ID: "sess-B", Name: "b", Tool: "codex", Cwd: cwd, Group: "g", Status: "idle", CreatedAt: base.Add(2 * time.Second)}); err != nil {
		t.Fatal(err)
	}
	sessA, err := st.Get("sess-A")
	if err != nil {
		t.Fatal(err)
	}
	sessB, err := st.Get("sess-B")
	if err != nil {
		t.Fatal(err)
	}

	sessions := []store.Session{sessB, sessA} // store order, not launch order
	p := &poller{store: st, sessionStores: map[string]string{"codex": "codex"}}
	panes := map[string]int{"sess-A": 123, "sess-B": 456}
	if err := p.captureAgentSessionIDs(sessions, panes, map[string]bool{}); err != nil {
		t.Fatal(err)
	}

	if got := sessions[1].AgentSessionID; got != "A-id" {
		t.Fatalf("session A captured %q, want A-id", got)
	}
	if got := sessions[0].AgentSessionID; got != "B-id" {
		t.Fatalf("session B captured %q, want B-id", got)
	}
}
