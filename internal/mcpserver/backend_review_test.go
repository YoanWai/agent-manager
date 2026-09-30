package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

func TestBoundMailboxToolsRejectClosedBackend(t *testing.T) {
	backend := sessioncmd.OpenBackend(t.TempDir())
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}
	session := connectServer(t, NewServerWithBackend(t.TempDir(), "cafe", "test", false, backend))
	for _, tc := range []struct {
		name string
		args map[string]any
	}{
		{"rename", map[string]any{"name": "worker"}},
		{"review", map[string]any{"repo": "."}},
		{"review", map[string]any{"base": "auto"}},
		{"review", map[string]any{"mode": "staged"}},
		{"review_comment", map[string]any{"comment_id": "0123456789abcdef"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text, failed := callText(t, session, tc.name, tc.args)
			if !failed || !strings.Contains(text, "backend is closed") {
				t.Fatalf("text=%q failed=%v, want closed backend", text, failed)
			}
		})
	}
}

func TestBoundMailboxToolsIgnoreAlternateProfile(t *testing.T) {
	dir, alternate := t.TempDir(), t.TempDir()
	st, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	backend, err := sessioncmd.BorrowBackend(sessioncmd.Runtime{Store: st, Driver: new(tmux.Driver), Hooks: hooks.NewManager(dir), Snapshot: st.SetSnapshot})
	if err != nil {
		t.Fatal(err)
	}
	const comment = "0123456789abcdef"
	if err := st.SetReviewState("cafe", "/repo", store.ReviewState{Comments: []store.ReviewComment{{ID: comment, Round: 1, Point: 1, Text: "fix"}}}); err != nil {
		t.Fatal(err)
	}
	session := connectServer(t, NewServerWithBackend(alternate, "cafe", "test", false, backend))
	for _, tc := range []struct {
		name string
		args map[string]any
	}{
		{"rename", map[string]any{"name": "worker"}},
		{"review", map[string]any{"repo": ".", "base": "auto", "mode": "staged"}},
		{"review_comment", map[string]any{"comment_id": comment}},
	} {
		if text, failed := callText(t, session, tc.name, tc.args); failed {
			t.Fatalf("%s: %s", tc.name, text)
		}
	}
	mailbox := hooks.NewManager(dir)
	for _, path := range []string{mailbox.NameFile("cafe"), mailbox.ReviewRepoFile("cafe"), mailbox.ReviewBaseFile("cafe"), mailbox.ReviewScopeFile("cafe")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("bound mailbox %s: %v", path, err)
		}
	}
	state, err := st.ReviewState("cafe", "/repo")
	if err != nil || len(state.Comments) != 1 || !state.Comments[0].Resolved {
		t.Fatalf("bound comment=%+v err=%v", state, err)
	}
	entries, err := os.ReadDir(alternate)
	if err != nil || len(entries) != 0 {
		t.Fatalf("alternate profile touched: %v %v", entries, err)
	}
}
