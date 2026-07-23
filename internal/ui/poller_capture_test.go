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

// Two codex sessions share a directory, A launched a fraction of a second
// before B, both within the same wall-clock second. The poller receives
// them in store order (B first), not launch order. Sub-second launch times
// must survive the store round-trip so capture binds each to its own
// conversation instead of swapping them.
func TestCaptureAgentSessionIDsAssignsInLaunchOrder(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	cwd := t.TempDir()

	// A whole second, so A and B share it and only nanoseconds separate them.
	base := time.Now().Truncate(time.Second).Add(-time.Minute)
	aLaunch := base.Add(100 * time.Millisecond)
	bLaunch := base.Add(600 * time.Millisecond)
	writeCodexRollout(t, filepath.Join(codexHome, "sessions", "rollout-A.jsonl"), "A-id", cwd, aLaunch)
	writeCodexRollout(t, filepath.Join(codexHome, "sessions", "rollout-B.jsonl"), "B-id", cwd, bLaunch)

	if err := st.CreateSession(store.Session{ID: "sess-A", Name: "a", Tool: "codex", Cwd: cwd, Group: "g", Status: "idle", CreatedAt: aLaunch}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(store.Session{ID: "sess-B", Name: "b", Tool: "codex", Cwd: cwd, Group: "g", Status: "idle", CreatedAt: bLaunch}); err != nil {
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
	captured, err := p.captureAgentSessionIDs(sessions, panes)
	if err != nil {
		t.Fatal(err)
	}
	if captured != 2 {
		t.Fatalf("captured %d, want 2", captured)
	}

	gotA, err := st.Get("sess-A")
	if err != nil {
		t.Fatal(err)
	}
	if gotA.AgentSessionID != "A-id" {
		t.Fatalf("session A captured %q, want A-id", gotA.AgentSessionID)
	}
	gotB, err := st.Get("sess-B")
	if err != nil {
		t.Fatal(err)
	}
	if gotB.AgentSessionID != "B-id" {
		t.Fatalf("session B captured %q, want B-id", gotB.AgentSessionID)
	}
}
