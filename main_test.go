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

// The base ref comes from the process working directory, so the test runs the
// command from inside the repo. The mailbox holds the repo root then the ref.
func TestRunReviewBaseWritesMailbox(t *testing.T) {
	repo := initRepo(t)
	writeFile(t, repo, "a.txt", "one\n")
	gitOutput(t, repo, "add", "-A")
	gitOutput(t, repo, "commit", "-m", "init")
	gitOutput(t, repo, "branch", "feature")
	toplevel := gitOutput(t, repo, "rev-parse", "--show-toplevel")

	sub := filepath.Join(repo, "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)

	configDir := t.TempDir()
	if err := runReviewBase([]string{"feature"}, "abc123", configDir); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(hooks.NewManager(configDir).ReviewBaseFile("abc123"))
	if err != nil {
		t.Fatal(err)
	}
	want := toplevel + "\nfeature\n"
	if string(raw) != want {
		t.Fatalf("mailbox = %q, want %q", raw, want)
	}
}

func TestRunReviewBaseClear(t *testing.T) {
	repo := initRepo(t)
	writeFile(t, repo, "a.txt", "one\n")
	gitOutput(t, repo, "add", "-A")
	gitOutput(t, repo, "commit", "-m", "init")
	toplevel := gitOutput(t, repo, "rev-parse", "--show-toplevel")
	t.Chdir(repo)

	configDir := t.TempDir()
	if err := runReviewBase([]string{"--clear"}, "abc123", configDir); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(hooks.NewManager(configDir).ReviewBaseFile("abc123"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != toplevel+"\n\n" {
		t.Fatalf("clear mailbox = %q, want root with empty ref line", raw)
	}
}

func TestRunReviewBaseRejectsBadInput(t *testing.T) {
	repo := initRepo(t)
	writeFile(t, repo, "a.txt", "one\n")
	gitOutput(t, repo, "add", "-A")
	gitOutput(t, repo, "commit", "-m", "init")
	configDir := t.TempDir()

	t.Run("missing session id", func(t *testing.T) {
		t.Chdir(repo)
		if err := runReviewBase([]string{"main"}, "", configDir); err == nil {
			t.Error("missing session id should fail")
		}
	})
	t.Run("malformed session id", func(t *testing.T) {
		t.Chdir(repo)
		if err := runReviewBase([]string{"main"}, "ABC/../x", configDir); err == nil {
			t.Error("a malformed session id should fail")
		}
	})
	t.Run("bad ref", func(t *testing.T) {
		t.Chdir(repo)
		if err := runReviewBase([]string{"nope"}, "abc123", configDir); err == nil {
			t.Error("an unresolvable ref should fail")
		}
	})
	t.Run("missing argument", func(t *testing.T) {
		t.Chdir(repo)
		if err := runReviewBase(nil, "abc123", configDir); err == nil {
			t.Error("a missing ref argument should fail")
		}
	})
	t.Run("cwd not a repo", func(t *testing.T) {
		t.Chdir(t.TempDir())
		if err := runReviewBase([]string{"main"}, "abc123", configDir); err == nil {
			t.Error("running outside a git repo should fail")
		}
	})
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
