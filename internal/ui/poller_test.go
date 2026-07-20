package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/store"
)

func newTestPollerWithSession(t *testing.T) (*poller, store.Session) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	sess := store.Session{ID: "sess-1", Name: "one", Tool: "codex", Cwd: t.TempDir(), Group: "g", Status: "idle"}
	if err := st.CreateSession(sess); err != nil {
		t.Fatal(err)
	}
	got, err := st.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}

	hookManager := hooks.NewManager(t.TempDir())
	p := &poller{store: st, hooks: hookManager}
	return p, got
}

func TestPollerAppliesPendingReviewRepo(t *testing.T) {
	p, sess := newTestPollerWithSession(t)
	path := p.hooks.ReviewRepoFile(sess.ID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("/repos/alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := p.applyPendingReviewRepo(&sess); err != nil {
		t.Fatal(err)
	}
	got, err := p.store.ReviewRepo(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != "/repos/alpha" {
		t.Fatalf("stored review repo = %q, want /repos/alpha", got)
	}
	if _, found := p.hooks.ReadReviewRepo(sess.ID); found {
		t.Fatal("mailbox should be consumed")
	}
}
