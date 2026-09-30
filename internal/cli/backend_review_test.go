package cli

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

func TestBoundReviewCommandsRejectClosedBackend(t *testing.T) {
	backend := sessioncmd.OpenBackend(t.TempDir())
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}
	commands := CommandsWithBackend("test", backend)
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"rename", []string{"worker"}}, {"review-repo", []string{"."}},
		{"review-base", []string{"--clear"}}, {"review-mode", []string{"staged"}},
		{"review-comment", []string{"0123456789abcdef"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			err := commands[tc.name](tc.args, func() string { return "cafe" }, dir)
			if err == nil || !strings.Contains(err.Error(), "backend is closed") {
				t.Fatalf("error = %v, want closed backend", err)
			}
			if _, err := os.Stat(hooks.NewManager(dir).ReviewScopeFile("cafe")); !os.IsNotExist(err) {
				t.Fatalf("alternate mailbox touched: %v", err)
			}
		})
	}
}

func TestBoundMailboxCommandsIgnoreAlternateProfile(t *testing.T) {
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
	repo := initRepo(t)
	t.Chdir(repo)
	commands := CommandsWithBackend("test", backend)
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"rename", []string{"worker"}}, {"review-repo", []string{repo}},
		{"review-base", []string{"--clear"}}, {"review-mode", []string{"staged"}},
		{"review-comment", []string{comment}},
	} {
		if err := commands[tc.name](tc.args, func() string { return "cafe" }, alternate); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
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
