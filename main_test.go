package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/hooks"
)

func TestRunRenameWritesNameFile(t *testing.T) {
	dir := t.TempDir()
	if err := runRename([]string{"fix auth bug"}, "abcd1234", dir); err != nil {
		t.Fatalf("runRename: %v", err)
	}
	raw, err := os.ReadFile(hooks.NewManager(dir).NameFile("abcd1234"))
	if err != nil {
		t.Fatalf("read name file: %v", err)
	}
	if string(raw) != "fix auth bug" {
		t.Fatalf("name file = %q", raw)
	}
}

func TestRunRenameValidation(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		label     string
		args      []string
		sessionID string
	}{
		{"no args", nil, "abcd1234"},
		{"two args", []string{"a", "b"}, "abcd1234"},
		{"blank name", []string{"  "}, "abcd1234"},
		{"missing session id", []string{"name"}, ""},
		{"traversal session id", []string{"name"}, "../evil"},
		{"uppercase session id", []string{"name"}, "ABCD1234"},
	}
	for _, c := range cases {
		if err := runRename(c.args, c.sessionID, dir); err == nil {
			t.Fatalf("%s: want error", c.label)
		}
	}
}

// A subdirectory must normalise to the repo toplevel, which is the whole point
// of the subcommand. The expected value comes from git rather than the temp
// path because t.TempDir() resolves through /private on macOS.
func TestRunReviewRepoWritesMailbox(t *testing.T) {
	repo := initRepo(t)
	sub := filepath.Join(repo, "pkg", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	toplevel := gitOutput(t, repo, "rev-parse", "--show-toplevel")

	configDir := t.TempDir()
	if err := runReviewRepo([]string{sub}, "abc123", configDir); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(hooks.NewManager(configDir).ReviewRepoFile("abc123"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(raw)); got != toplevel {
		t.Fatalf("mailbox = %q, want the repo toplevel %q", got, toplevel)
	}
}

// An umbrella folder holding repos is not itself inside a repo. Recording the
// dirtiest nested repo there would file a guess as a declaration.
func TestRunReviewRepoRejectsUmbrella(t *testing.T) {
	umbrella := t.TempDir()
	for _, name := range []string{"alpha", "bravo"} {
		dir := filepath.Join(umbrella, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		initRepoAt(t, dir)
	}
	configDir := t.TempDir()
	err := runReviewRepo([]string{umbrella}, "abc123", configDir)
	if err == nil {
		t.Fatal("an umbrella of repos is not inside a git repo and must be rejected")
	}
	if !strings.Contains(err.Error(), "not inside a git repository") {
		t.Fatalf("error should name the real problem, got %v", err)
	}
	if _, statErr := os.Stat(hooks.NewManager(configDir).ReviewRepoFile("abc123")); !os.IsNotExist(statErr) {
		t.Fatal("a rejected path must not be recorded")
	}
}

func initRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	initRepoAt(t, repo)
	return repo
}

func initRepoAt(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{{"init"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"}} {
		gitOutput(t, dir, args...)
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestRunReviewRepoRejectsBadInput(t *testing.T) {
	configDir := t.TempDir()
	if err := runReviewRepo([]string{t.TempDir()}, "", configDir); err == nil {
		t.Error("missing session id should fail")
	}
	if err := runReviewRepo([]string{t.TempDir()}, "abc123", configDir); err == nil {
		t.Error("a path that is not a repo should fail")
	}
	if err := runReviewRepo(nil, "abc123", configDir); err == nil {
		t.Error("a missing path argument should fail")
	}
}
