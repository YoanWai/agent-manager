package cli

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/store"
)

// Every launched session runs `agent-manager rename "<name>"` as the first
// line of its first prompt, so this exact invocation is what the whole
// naming mechanism rests on.
func TestRenameWritesNameFile(t *testing.T) {
	dir := t.TempDir()
	out := &bytes.Buffer{}
	if err := runRename(out, []string{"fix auth bug"}, "abcd1234", dir); err != nil {
		t.Fatalf("runRename: %v", err)
	}
	// No manager runs in this test, so the answer is that the name waits.
	if !strings.Contains(out.String(), `rename to "fix auth bug" is queued`) {
		t.Fatalf("rename output = %q", out.String())
	}
	raw, err := os.ReadFile(hooks.NewManager(dir).NameFile("abcd1234"))
	if err != nil {
		t.Fatalf("read name file: %v", err)
	}
	// The first line is the request the answer comes back under.
	request, name, ok := strings.Cut(string(raw), "\n")
	if !ok || request == "" || name != "fix auth bug" {
		t.Fatalf("name file = %q", raw)
	}
}

func TestRenameValidation(t *testing.T) {
	dir := t.TempDir()
	const usage = `usage: agent-manager rename "<name>"`
	cases := []struct {
		label     string
		args      []string
		sessionID string
		want      string
	}{
		{"no args", nil, "abcd1234", usage},
		{"two args", []string{"a", "b"}, "abcd1234", usage},
		{"blank name", []string{"  "}, "abcd1234", usage},
		{"missing session id", []string{"name"}, "", ""},
		{"traversal session id", []string{"name"}, "../evil", ""},
		{"uppercase session id", []string{"name"}, "ABCD1234", ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.label, func(t *testing.T) {
			err := runRename(&bytes.Buffer{}, testCase.args, testCase.sessionID, dir)
			if err == nil {
				t.Fatal("want error")
			}
			if testCase.want != "" && err.Error() != testCase.want {
				t.Fatalf("error = %q, want %q", err, testCase.want)
			}
		})
	}
}

// A subdirectory must normalise to the repo toplevel, which is the whole point
// of the subcommand. The expected value comes from git rather than the temp
// path because t.TempDir() resolves through /private on macOS.
func TestReviewRepoWritesMailbox(t *testing.T) {
	repo := initRepo(t)
	sub := filepath.Join(repo, "pkg", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	toplevel := gitOutput(t, repo, "rev-parse", "--show-toplevel")

	configDir := t.TempDir()
	out := &bytes.Buffer{}
	if err := runReviewRepo(out, []string{sub}, "abc123", configDir); err != nil {
		t.Fatal(err)
	}
	if out.String() != "review repo set to "+toplevel+"\n" {
		t.Fatalf("review-repo output = %q", out.String())
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
func TestReviewRepoRejectsUmbrella(t *testing.T) {
	umbrella := t.TempDir()
	for _, name := range []string{"alpha", "bravo"} {
		dir := filepath.Join(umbrella, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		initRepoAt(t, dir)
	}
	configDir := t.TempDir()
	err := runReviewRepo(&bytes.Buffer{}, []string{umbrella}, "abc123", configDir)
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

func TestReviewRepoRejectsBadInput(t *testing.T) {
	configDir := t.TempDir()
	if err := runReviewRepo(&bytes.Buffer{}, []string{t.TempDir()}, "", configDir); err == nil {
		t.Error("missing session id should fail")
	}
	if err := runReviewRepo(&bytes.Buffer{}, []string{t.TempDir()}, "abc123", configDir); err == nil {
		t.Error("a path that is not a repo should fail")
	}
	const usage = "usage: agent-manager review-repo <path>"
	for _, args := range [][]string{nil, {"  "}} {
		err := runReviewRepo(&bytes.Buffer{}, args, "abc123", configDir)
		if err == nil || err.Error() != usage {
			t.Errorf("runReviewRepo(%v) = %v, want %q", args, err, usage)
		}
	}
}

func TestReviewCommentPrintsAndStoresTheHandledFlag(t *testing.T) {
	configDir := t.TempDir()
	st, err := store.Open(filepath.Join(configDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	const commentID = "0123456789abcdef"
	if err := st.SetReviewState("abc123", "/repo", store.ReviewState{Comments: []store.ReviewComment{{
		ID: commentID, Round: 1, Point: 1, Text: "fix this",
	}}}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	out := &bytes.Buffer{}
	if err := runReviewComment(out, []string{commentID}, "abc123", configDir); err != nil {
		t.Fatal(err)
	}
	if out.String() != "review comment "+commentID+" marked handled\n" {
		t.Fatalf("output = %q", out.String())
	}
	st, err = store.Open(filepath.Join(configDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	state, err := st.ReviewState("abc123", "/repo")
	if err != nil || len(state.Comments) != 1 || !state.Comments[0].Resolved {
		t.Fatalf("handled comment = %+v, %v", state.Comments, err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	if err := runReviewComment(out, []string{commentID, "--reopen"}, "abc123", configDir); err != nil {
		t.Fatal(err)
	}
	st, err = store.Open(filepath.Join(configDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	state, err = st.ReviewState("abc123", "/repo")
	if err != nil || len(state.Comments) != 1 || state.Comments[0].Resolved {
		t.Fatalf("reopened comment = %+v, %v", state.Comments, err)
	}
}

func TestReviewCommentRejectsUnknownOrInvalidID(t *testing.T) {
	configDir := t.TempDir()
	for _, id := range []string{"../bad", "0123456789abcdef"} {
		if err := runReviewComment(&bytes.Buffer{}, []string{id}, "abc123", configDir); err == nil {
			t.Fatalf("comment id %q should fail", id)
		}
	}
	if err := runReviewComment(&bytes.Buffer{}, nil, "abc123", configDir); err == nil || err.Error() != "usage: agent-manager "+usageReviewComment {
		t.Fatalf("missing id = %v", err)
	}
}

// The base ref comes from the process working directory, so the test runs the
// command from inside the repo. The mailbox holds the repo root then the ref.
func TestReviewBaseWritesMailbox(t *testing.T) {
	repo := initRepo(t)
	commitFile(t, repo)
	gitOutput(t, repo, "branch", "feature")
	toplevel := gitOutput(t, repo, "rev-parse", "--show-toplevel")

	sub := filepath.Join(repo, "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)

	configDir := t.TempDir()
	out := &bytes.Buffer{}
	if err := runReviewBase(out, []string{"feature"}, "abc123", configDir); err != nil {
		t.Fatal(err)
	}
	if out.String() != "review base set to feature\n" {
		t.Fatalf("review-base output = %q", out.String())
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

func TestReviewBaseClear(t *testing.T) {
	repo := initRepo(t)
	commitFile(t, repo)
	toplevel := gitOutput(t, repo, "rev-parse", "--show-toplevel")
	t.Chdir(repo)

	configDir := t.TempDir()
	out := &bytes.Buffer{}
	if err := runReviewBase(out, []string{"--clear"}, "abc123", configDir); err != nil {
		t.Fatal(err)
	}
	if out.String() != "review base cleared for "+toplevel+"\n" {
		t.Fatalf("review-base --clear output = %q", out.String())
	}
	raw, err := os.ReadFile(hooks.NewManager(configDir).ReviewBaseFile("abc123"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != toplevel+"\n\n" {
		t.Fatalf("clear mailbox = %q, want root with empty ref line", raw)
	}
}

func TestReviewBaseRejectsBadInput(t *testing.T) {
	repo := initRepo(t)
	commitFile(t, repo)
	configDir := t.TempDir()
	const usage = "usage: agent-manager review-base <ref>|--clear"

	t.Run("missing session id", func(t *testing.T) {
		t.Chdir(repo)
		if err := runReviewBase(&bytes.Buffer{}, []string{"main"}, "", configDir); err == nil {
			t.Error("missing session id should fail")
		}
	})
	t.Run("malformed session id", func(t *testing.T) {
		t.Chdir(repo)
		if err := runReviewBase(&bytes.Buffer{}, []string{"main"}, "ABC/../x", configDir); err == nil {
			t.Error("a malformed session id should fail")
		}
	})
	t.Run("bad ref", func(t *testing.T) {
		t.Chdir(repo)
		if err := runReviewBase(&bytes.Buffer{}, []string{"nope"}, "abc123", configDir); err == nil {
			t.Error("an unresolvable ref should fail")
		}
	})
	t.Run("cwd not a repo", func(t *testing.T) {
		t.Chdir(t.TempDir())
		if err := runReviewBase(&bytes.Buffer{}, []string{"main"}, "abc123", configDir); err == nil {
			t.Error("running outside a git repo should fail")
		}
	})
	for label, args := range map[string][]string{
		"missing argument":  nil,
		"a ref and --clear": {"main", "--clear"},
		"a blank ref":       {"  "},
	} {
		t.Run(label, func(t *testing.T) {
			t.Chdir(repo)
			err := runReviewBase(&bytes.Buffer{}, args, "abc123", configDir)
			if err == nil || err.Error() != usage {
				t.Errorf("error = %v, want %q", err, usage)
			}
		})
	}
}

func TestReviewModeRecordsAKnownScope(t *testing.T) {
	configDir := t.TempDir()
	out := &bytes.Buffer{}
	if err := runReviewMode(out, []string{"staged"}, "abc123", configDir); err != nil {
		t.Fatalf("review-mode: %v", err)
	}
	if out.String() != "review scope set to staged\n" {
		t.Fatalf("review-mode output = %q", out.String())
	}
	raw, err := os.ReadFile(hooks.NewManager(configDir).ReviewScopeFile("abc123"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "staged" {
		t.Fatalf("recorded scope = %q", raw)
	}

	err = runReviewMode(&bytes.Buffer{}, []string{"everything"}, "abc123", configDir)
	if err == nil || !strings.Contains(err.Error(), "uncommitted, branch, last_commit, staged") {
		t.Fatalf("error = %v, want it to list the scopes", err)
	}
}

func TestReviewCommandsShowUsageOnHelp(t *testing.T) {
	out := &bytes.Buffer{}
	if err := runRename(out, []string{"-h"}, "abcd1234", t.TempDir()); !errors.Is(err, ErrUsageShown) {
		t.Fatalf("rename -h = %v, want ErrUsageShown", err)
	}
	if !strings.Contains(out.String(), "usage: agent-manager "+usageRename) {
		t.Fatalf("rename -h output = %q", out.String())
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

func commitFile(t *testing.T, repo string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitOutput(t, repo, "add", "-A")
	gitOutput(t, repo, "commit", "-m", "init")
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
