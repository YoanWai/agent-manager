package sessioncmd

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/store"
)

const reviewTestComment = "0123456789abcdef"

func reviewConfigDir(t *testing.T) string {
	t.Helper()
	configDir := t.TempDir()
	st, err := store.Open(filepath.Join(configDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetReviewState("abc123", "/repo", store.ReviewState{Comments: []store.ReviewComment{{
		ID: reviewTestComment, Round: 1, Point: 1, Text: "fix this",
	}}}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	return configDir
}

func reviewCommentResolved(t *testing.T, configDir, repoRoot string) bool {
	t.Helper()
	st, err := store.Open(filepath.Join(configDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	state, err := st.ReviewState("abc123", repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Comments) != 1 {
		t.Fatalf("review state for %s = %+v, want the one comment", repoRoot, state.Comments)
	}
	return state.Comments[0].Resolved
}

// The same id under two repos is the one case the store refuses outright,
// since it cannot tell which comment the agent meant.
func reviewCommentInASecondRepo(t *testing.T, configDir string) {
	t.Helper()
	st, err := store.Open(filepath.Join(configDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetReviewState("abc123", "/other", store.ReviewState{Comments: []store.ReviewComment{{
		ID: reviewTestComment, Round: 1, Point: 1, Text: "fix this too",
	}}}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
}

type testGitRepo struct {
	root    string
	baseRef string
}

func isolateGitEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"GIT_DIR",
		"GIT_WORK_TREE",
		"GIT_COMMON_DIR",
		"GIT_OBJECT_DIRECTORY",
		"GIT_INDEX_FILE",
		"GIT_ALTERNATE_OBJECT_DIRECTORIES",
	} {
		value, present := os.LookupEnv(key)
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset %s: %v", key, err)
		}
		t.Cleanup(func() {
			if present {
				if err := os.Setenv(key, value); err != nil {
					t.Errorf("restore %s: %v", key, err)
				}
				return
			}
			if err := os.Unsetenv(key); err != nil {
				t.Errorf("restore %s: %v", key, err)
			}
		})
	}
}

func newTestGitRepo(t *testing.T, dir string) testGitRepo {
	t.Helper()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "test@test"},
		{"config", "user.name", "test"},
		{"commit", "--allow-empty", "--no-gpg-sign", "-m", "initial commit"},
		{"branch", "test-base"},
	} {
		run(args...)
	}
	return testGitRepo{
		root:    run("rev-parse", "--show-toplevel"),
		baseRef: "test-base",
	}
}

func TestReviewRepo(t *testing.T) {
	isolateGitEnvironment(t)
	repo := newTestGitRepo(t, t.TempDir())
	nested := filepath.Join(repo.root, "nested")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	umbrella := t.TempDir()
	nestedRepoDir := filepath.Join(umbrella, "repo")
	if err := os.Mkdir(nestedRepoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	newTestGitRepo(t, nestedRepoDir)

	for _, tc := range []struct {
		name    string
		session string
		target  string
		wantMsg string
		wantErr string
	}{
		{name: "nested directory", session: "abc123", target: " " + nested + " ", wantMsg: "review repo set to " + repo.root},
		{name: "blank path", session: "abc123", target: "  ", wantErr: "path is empty"},
		{name: "invalid session", session: "not-a-session", target: repo.root, wantErr: "invalid session id"},
		{name: "outside a repo", session: "abc123", target: outside, wantErr: "is not inside a git repository"},
		{name: "umbrella is not a repo", session: "abc123", target: umbrella, wantErr: "is not inside a git repository"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			configDir := t.TempDir()
			got, err := ReviewRepo(configDir, tc.session, tc.target)
			path := hooks.NewManager(configDir).ReviewRepoFile(tc.session)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want one containing %q", err, tc.wantErr)
				}
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("failed request wrote a mailbox: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.wantMsg {
				t.Fatalf("message = %q, want %q", got, tc.wantMsg)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != repo.root {
				t.Fatalf("mailbox = %q, want %q", data, repo.root)
			}
		})
	}
}

func TestReviewBase(t *testing.T) {
	isolateGitEnvironment(t)
	repo := newTestGitRepo(t, t.TempDir())
	subdir := filepath.Join(repo.root, "nested")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	nonRepo := t.TempDir()

	for _, tc := range []struct {
		name        string
		session     string
		cwd         string
		ref         string
		wantMsg     string
		wantMailbox string
		wantErr     string
	}{
		{
			name:        "branch from a nested directory",
			session:     "abc123",
			cwd:         subdir,
			ref:         repo.baseRef,
			wantMsg:     "review base set to test-base",
			wantMailbox: repo.root + "\n" + repo.baseRef + "\n",
		},
		{
			name:        "trim ref",
			session:     "abc123",
			cwd:         repo.root,
			ref:         " " + repo.baseRef + " ",
			wantMsg:     "review base set to test-base",
			wantMailbox: repo.root + "\n" + repo.baseRef + "\n",
		},
		{
			name:        "clear",
			session:     "abc123",
			cwd:         repo.root,
			ref:         "  ",
			wantMsg:     "review base cleared for " + repo.root,
			wantMailbox: repo.root + "\n\n",
		},
		{name: "invalid session", session: "not-a-session", cwd: repo.root, ref: repo.baseRef, wantErr: "invalid session id"},
		{name: "outside a repo", session: "abc123", cwd: nonRepo, ref: repo.baseRef, wantErr: "not inside a git repository"},
		{name: "unknown ref", session: "abc123", cwd: repo.root, ref: "missing-ref", wantErr: `ref "missing-ref" does not resolve to a commit`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			configDir := t.TempDir()
			got, err := ReviewBase(configDir, tc.session, tc.cwd, tc.ref)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want one containing %q", err, tc.wantErr)
				}
				if _, err := os.Stat(hooks.NewManager(configDir).ReviewBaseFile(tc.session)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("failed request wrote a mailbox: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.wantMsg {
				t.Fatalf("message = %q, want %q", got, tc.wantMsg)
			}
			path := hooks.NewManager(configDir).ReviewBaseFile(tc.session)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != tc.wantMailbox {
				t.Fatalf("mailbox = %q, want %q", data, tc.wantMailbox)
			}
		})
	}
}

func TestReviewScope(t *testing.T) {
	for _, tc := range []struct {
		name      string
		session   string
		scope     string
		wantScope string
		wantErr   string
	}{
		{name: "uncommitted", session: "abc123", scope: "uncommitted", wantScope: "uncommitted"},
		{name: "branch trimmed", session: "abc123", scope: " branch ", wantScope: "branch"},
		{name: "last commit", session: "abc123", scope: "last_commit", wantScope: "last_commit"},
		{name: "staged", session: "abc123", scope: "staged", wantScope: "staged"},
		{name: "unknown", session: "abc123", scope: "workspace", wantErr: "unknown scope"},
		{name: "invalid session", session: "not-a-session", scope: "branch", wantErr: "invalid session id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			configDir := t.TempDir()
			got, err := ReviewScope(configDir, tc.session, tc.scope)
			path := hooks.NewManager(configDir).ReviewScopeFile(tc.session)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want one containing %q", err, tc.wantErr)
				}
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("failed request wrote a mailbox: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			wantMsg := "review scope set to " + tc.wantScope
			if got != wantMsg {
				t.Fatalf("message = %q, want %q", got, wantMsg)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != tc.wantScope {
				t.Fatalf("mailbox = %q, want %q", data, tc.wantScope)
			}
		})
	}
}

func TestPathWithin(t *testing.T) {
	container := t.TempDir()
	root := filepath.Join(container, "root")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(root, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	sibling := root + "-other"
	if err := os.Mkdir(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(container, "root-link")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		path string
		root string
		want bool
	}{
		{name: "root", path: root, root: root, want: true},
		{name: "child", path: child, root: root, want: true},
		{name: "symlinked root", path: filepath.Join(alias, "child"), root: root, want: true},
		{name: "sibling", path: sibling, root: root, want: false},
		{name: "shared prefix", path: filepath.Join(root+"-suffix", "child"), root: root, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := pathWithin(tc.path, tc.root); got != tc.want {
				t.Fatalf("pathWithin(%q, %q) = %t, want %t", tc.path, tc.root, got, tc.want)
			}
		})
	}
}

func TestReviewCommentMarksHandledAndReopens(t *testing.T) {
	configDir := reviewConfigDir(t)

	message, err := ReviewComment(configDir, "abc123", reviewTestComment, true)
	if err != nil {
		t.Fatal(err)
	}
	if message != "review comment "+reviewTestComment+" marked handled" {
		t.Fatalf("message = %q", message)
	}
	if !reviewCommentResolved(t, configDir, "/repo") {
		t.Fatal("the comment was not stored as handled")
	}

	message, err = ReviewComment(configDir, "abc123", reviewTestComment, false)
	if err != nil {
		t.Fatal(err)
	}
	if message != "review comment "+reviewTestComment+" reopened" {
		t.Fatalf("reopen message = %q", message)
	}
	if reviewCommentResolved(t, configDir, "/repo") {
		t.Fatal("the comment was not stored as open again")
	}
}

func TestReviewCommentRejectsAnIDItCannotUse(t *testing.T) {
	configDir := reviewConfigDir(t)
	unknown := "fedcba9876543210"

	for _, id := range []string{"", "  ", "nope", strings.ToUpper(reviewTestComment), reviewTestComment + "0"} {
		if _, err := ReviewComment(configDir, "abc123", id, true); err == nil {
			t.Fatalf("%q was accepted as a comment id", id)
		}
	}
	if _, err := ReviewComment(configDir, "abc123", unknown, true); err == nil {
		t.Fatal("an id belonging to no comment was accepted")
	}
	if reviewCommentResolved(t, configDir, "/repo") {
		t.Fatal("a rejected id still changed the stored comment")
	}
}

func TestReviewCommentRefusesAnIDTwoReposShare(t *testing.T) {
	configDir := reviewConfigDir(t)
	reviewCommentInASecondRepo(t, configDir)

	_, err := ReviewComment(configDir, "abc123", reviewTestComment, true)
	if err == nil {
		t.Fatal("an id two repos share was accepted")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("error = %v, want the ambiguity refusal", err)
	}
	for _, repoRoot := range []string{"/repo", "/other"} {
		if reviewCommentResolved(t, configDir, repoRoot) {
			t.Fatalf("the refused call still marked %s handled", repoRoot)
		}
	}
}

func stampManagerHeartbeat(t *testing.T, configDir string) {
	t.Helper()
	st, err := store.Open(filepath.Join(configDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.SetSetting(store.PollerHeartbeatKey, strconv.FormatInt(time.Now().UnixNano(), 10)); err != nil {
		t.Fatal(err)
	}
}

func TestRenameWithoutAManagerReportsItQueued(t *testing.T) {
	configDir := t.TempDir()
	message, err := Rename(t.Context(), configDir, "abc123", "fix auth bug")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if !strings.Contains(message, "queued") || strings.Contains(message, "renamed to") {
		t.Fatalf("with no manager running the answer must not claim the rename happened: %q", message)
	}
	if _, name, found := hooks.NewManager(configDir).ReadName("abc123"); !found || name != "fix auth bug" {
		t.Fatalf("name file = %q, %v; the rename must still wait for a manager", name, found)
	}
}

// The subcommand answers with what the poller did, so an agent that
// names itself reasons about the name it actually has.
func TestRenameWaitsForTheManagerAndReportsItsAnswer(t *testing.T) {
	configDir := t.TempDir()
	stampManagerHeartbeat(t, configDir)
	mailbox := hooks.NewManager(configDir)
	answer := func(t *testing.T, name string, refusal error) {
		t.Helper()
		go func() {
			for {
				request, pending, found, err := mailbox.ClaimName("abc123")
				if err != nil {
					t.Errorf("ClaimName: %v", err)
					return
				}
				if found {
					mine := pending == name
					if mine {
						if err := mailbox.WriteNameResult("abc123", request, name, name, refusal); err != nil {
							t.Errorf("WriteNameResult: %v", err)
						}
					}
					// The claim is released either way, or the next one
					// picks this rename up again instead of the next name.
					if err := mailbox.ReleaseName("abc123"); err != nil {
						t.Errorf("ReleaseName: %v", err)
						return
					}
					if mine {
						return
					}
				}
				time.Sleep(10 * time.Millisecond)
			}
		}()
	}

	answer(t, "fix auth bug", nil)
	// The typed name carries whitespace the manager squashes out, so the
	// wait has to recognize the answer by the squashed name.
	message, err := Rename(t.Context(), configDir, "abc123", "fix   auth bug")
	if err != nil || message != "session renamed to fix auth bug" {
		t.Fatalf("Rename = %q, %v", message, err)
	}
	if left, err := filepath.Glob(filepath.Join(mailbox.Dir(), "abc123.*.renamed")); err != nil || len(left) != 0 {
		t.Fatalf("a read answer must be consumed, left %v (%v)", left, err)
	}

	answer(t, "taken", errors.New("worktree rename: branch already exists: am/taken"))
	if _, err := Rename(t.Context(), configDir, "abc123", "taken"); err == nil || !strings.Contains(err.Error(), "branch already exists: am/taken") {
		t.Fatalf("a refusal must reach the caller with its reason, got %v", err)
	}
}

// Two renames for one session are in flight at once, so each caller is
// answered for the rename it queued and neither consumes the other's.
func TestRenameAnswersEachCallerItsOwnRename(t *testing.T) {
	configDir := t.TempDir()
	stampManagerHeartbeat(t, configDir)
	mailbox := hooks.NewManager(configDir)
	if err := os.MkdirAll(mailbox.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	// The stand-in manager claims whatever is pending, the way the poller
	// does, and answers it under the request that asked.
	stop := make(chan struct{})
	stopped := make(chan struct{})
	t.Cleanup(func() {
		close(stop)
		<-stopped
	})
	go func() {
		defer close(stopped)
		for {
			select {
			case <-stop:
				return
			case <-time.After(5 * time.Millisecond):
			}
			request, pending, found, err := mailbox.ClaimName("abc123")
			if err != nil {
				t.Errorf("ClaimName: %v", err)
				return
			}
			if !found {
				continue
			}
			if err := mailbox.WriteNameResult("abc123", request, pending, pending, nil); err != nil {
				t.Errorf("WriteNameResult: %v", err)
			}
			if err := mailbox.ReleaseName("abc123"); err != nil {
				t.Errorf("ReleaseName: %v", err)
				return
			}
		}
	}()

	// Both renames are in flight together, and a caller whose request the
	// other replaced in the mailbox gives up rather than hold the test for
	// the full rename deadline.
	type outcome struct{ asked, message string }
	results := make(chan outcome, 2)
	for _, name := range []string{"first racer", "second racer"} {
		go func() {
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			message, err := Rename(ctx, configDir, "abc123", name)
			if err != nil {
				message = "unanswered: " + err.Error()
			}
			results <- outcome{name, message}
		}()
	}
	applied := 0
	for range 2 {
		got := <-results
		other := "first racer"
		if got.asked == other {
			other = "second racer"
		}
		if strings.Contains(got.message, other) {
			t.Fatalf("the caller that asked for %q was told about %q: %q", got.asked, other, got.message)
		}
		if got.message == "session renamed to "+got.asked {
			applied++
			continue
		}
		// The other rename replacing this one in the mailbox is reported as
		// exactly that, and giving up on the context is the other way this
		// caller ends.
		if !strings.HasPrefix(got.message, "unanswered: ") && !strings.Contains(got.message, "was replaced by a later rename") {
			t.Fatalf("caller asking for %q got %q", got.asked, got.message)
		}
	}
	if applied == 0 {
		t.Fatal("neither rename was answered as applied")
	}
}

func TestRenameStopsWhenItsCallerGivesUp(t *testing.T) {
	configDir := t.TempDir()
	stampManagerHeartbeat(t, configDir)
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	if _, err := Rename(ctx, configDir, "abc123", "abandoned"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Rename = %v, want the cancellation", err)
	}
}

func TestRenameIgnoresAnAnswerToAnotherRequest(t *testing.T) {
	configDir := t.TempDir()
	mailbox := hooks.NewManager(configDir)
	if err := os.MkdirAll(mailbox.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := mailbox.WriteNameResult("abc123", "earlier", "old name", "old name", nil); err != nil {
		t.Fatal(err)
	}
	message, err := Rename(t.Context(), configDir, "abc123", "new name")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if strings.Contains(message, "old name") || strings.Contains(message, "renamed to") {
		t.Fatalf("an answer to an earlier rename must not be read as this one: %q", message)
	}
	if _, found, err := mailbox.ReadNameResult("abc123", "earlier"); !found || err != nil {
		t.Fatalf("the earlier caller's answer was taken away: found=%v err=%v", found, err)
	}
}

// A rename the next one replaced in the mailbox is never applied, so the
// caller hears that rather than waiting out its deadline to be told the
// rename is still on its way.
func TestRenameReportsARequestALaterRenameReplaced(t *testing.T) {
	configDir := t.TempDir()
	stampManagerHeartbeat(t, configDir)
	mailbox := hooks.NewManager(configDir)
	if err := os.MkdirAll(mailbox.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	// The later rename lands in the mailbox while this one is waiting, and
	// no manager ever claims either.
	go func() {
		for {
			if _, _, found := mailbox.ReadName("abc123"); found {
				later, err := hooks.NewRequestID()
				if err != nil {
					t.Errorf("NewRequestID: %v", err)
					return
				}
				if err := hooks.WriteWhole(mailbox.NameFile("abc123"), hooks.NameRequest(later, "the later name")); err != nil {
					t.Errorf("queue the later rename: %v", err)
				}
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	message, err := Rename(t.Context(), configDir, "abc123", "the replaced name")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if !strings.Contains(message, "was replaced by a later rename") || !strings.Contains(message, "the replaced name") {
		t.Fatalf("Rename = %q; want the replaced rename named as never applied", message)
	}
}

// A rename the manager has already claimed is still on its way, however
// many renames queue behind it, so its caller waits for the answer rather
// than being told it was replaced.
func TestRenameWaitsWhileItsClaimedRenameIsApplied(t *testing.T) {
	configDir := t.TempDir()
	stampManagerHeartbeat(t, configDir)
	mailbox := hooks.NewManager(configDir)
	if err := os.MkdirAll(mailbox.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	// The manager claims this rename, a later one queues behind it, and
	// only then is the claimed rename answered.
	go func() {
		for {
			request, pending, found, err := mailbox.ClaimName("abc123")
			if err != nil {
				t.Errorf("ClaimName: %v", err)
				return
			}
			if !found {
				time.Sleep(5 * time.Millisecond)
				continue
			}
			later, err := hooks.NewRequestID()
			if err != nil {
				t.Errorf("NewRequestID: %v", err)
				return
			}
			if err := hooks.WriteWhole(mailbox.NameFile("abc123"), hooks.NameRequest(later, "the later name")); err != nil {
				t.Errorf("queue the later rename: %v", err)
				return
			}
			// Long enough that the caller polls while the later rename is
			// the pending one and this rename has no answer yet.
			time.Sleep(300 * time.Millisecond)
			if err := mailbox.WriteNameResult("abc123", request, pending, pending, nil); err != nil {
				t.Errorf("WriteNameResult: %v", err)
			}
			if err := mailbox.ReleaseName("abc123"); err != nil {
				t.Errorf("ReleaseName: %v", err)
			}
			return
		}
	}()

	message, err := Rename(t.Context(), configDir, "abc123", "the claimed name")
	if err != nil || message != "session renamed to the claimed name" {
		t.Fatalf("Rename = %q, %v; want the claimed rename reported as applied", message, err)
	}
}
