package ui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/launch"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
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

func TestPollerAppliesPendingReviewBase(t *testing.T) {
	p, sess := newTestPollerWithSession(t)
	path := p.hooks.ReviewBaseFile(sess.ID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("/repos/alpha\nmain\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := p.applyPendingReviewBase(&sess); err != nil {
		t.Fatal(err)
	}
	got, err := p.store.ReviewBase(sess.ID, "/repos/alpha")
	if err != nil {
		t.Fatal(err)
	}
	if got != "main" {
		t.Fatalf("stored review base = %q, want main", got)
	}
	if _, _, found := p.hooks.ReadReviewBase(sess.ID); found {
		t.Fatal("mailbox should be consumed")
	}
}

// An empty ref line clears the stored base, and the mailbox is still consumed.
func TestPollerAppliesReviewBaseClear(t *testing.T) {
	p, sess := newTestPollerWithSession(t)
	if err := p.store.SetReviewBase(sess.ID, "/repos/alpha", "main"); err != nil {
		t.Fatal(err)
	}
	path := p.hooks.ReviewBaseFile(sess.ID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("/repos/alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := p.applyPendingReviewBase(&sess); err != nil {
		t.Fatal(err)
	}
	got, err := p.store.ReviewBase(sess.ID, "/repos/alpha")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("review base after clear = %q, want empty", got)
	}
	if _, _, found := p.hooks.ReadReviewBase(sess.ID); found {
		t.Fatal("mailbox should be consumed")
	}
}

// A detached session must boot at the preview panel's width×height so its
// pane preview fills 1:1, and follow later terminal resizes, rather than
// staying at tmux's 80×24 default until attach.
func TestSessionSizesToPreviewPane(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "sized", t.TempDir(), "")
	id := m.sessionRows()[0].ID
	// Create sizes from pre-selection geometry; re-pin to the live preview box.
	m.resizeSessions()

	wantW, wantH := m.previewPaneWidth(), m.previewPaneHeight()
	if w, h := windowSize(t, id); w != wantW || h != wantH {
		t.Fatalf("new session window = %dx%d, want %dx%d", w, h, wantW, wantH)
	}

	m.Update(tea.WindowSizeMsg{Width: 150, Height: 45})
	wantW, wantH = m.previewPaneWidth(), m.previewPaneHeight()
	if w, h := windowSize(t, id); w != wantW || h != wantH {
		t.Fatalf("after resize, window = %dx%d, want %dx%d", w, h, wantW, wantH)
	}
}

func TestPendingRenameForADeletedSessionDoesNotFailThePoll(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "doomed", t.TempDir(), "")
	sess := m.sessionRows()[0]

	// The manager deleted the row while this poll pass still held it.
	if err := m.store.Delete(sess.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	request := writeName(t, m, sess.ID, "renamed")

	if err := m.poller.applyPendingRename(&sess); err != nil {
		t.Fatalf("rename of a deleted session should not fail the pass: %v", err)
	}
	if _, _, found := m.hooks.ReadName(sess.ID); found {
		t.Fatal("the name file should be consumed instead of retried every poll")
	}
	// The agent that asked hears that the row went away rather than a
	// success for a session that no longer exists.
	verdict, found, err := m.hooks.ReadNameResult(sess.ID, request)
	if err != nil || !found || verdict.Refusal == nil || !strings.Contains(verdict.Refusal.Error(), "session no longer exists") {
		t.Fatalf("result = %+v, %v, %v; want the refusal", verdict, found, err)
	}
}

func TestPendingRenameKeepsTheWorktreeDirectory(t *testing.T) {
	m := buildModel(t)
	repo := seedRepo(t)
	spawned := createWorktreeSession(t, m, "claude-7a72", repo)
	writeName(t, m, spawned.ID, "audit the poller")

	sess := spawned
	if err := m.poller.applyPendingRename(&sess); err != nil {
		t.Fatalf("rename: %v", err)
	}

	if sess.Cwd != spawned.Cwd || sess.WorktreeBranch != "am/audit-the-poller" {
		t.Fatalf("session did not keep its path and follow the branch: %+v", sess)
	}
	if sess.Name != "audit the poller" {
		t.Fatalf("name = %q", sess.Name)
	}
	stored, err := m.store.Get(spawned.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.Cwd != spawned.Cwd || stored.WorktreeBranch != "am/audit-the-poller" || stored.Name != "audit the poller" {
		t.Fatalf("store did not keep the path and follow the name: %+v", stored)
	}
	if _, err := os.Stat(spawned.Cwd); err != nil {
		t.Fatalf("worktree directory moved: %v", err)
	}
	if _, _, found := m.hooks.ReadName(spawned.ID); found {
		t.Fatal("the name file should be consumed")
	}
	assertPaneStayedOnSpawnPath(t, m, spawned.ID, spawned.Cwd)
}

func TestPendingRenameLetsAgentKeepWorking(t *testing.T) {
	m := buildModel(t)
	repo := seedRepo(t)
	spawned := createWorktreeSession(t, m, "claude-7a72", repo)

	tmp := t.TempDir()
	goFile := filepath.Join(tmp, "go")
	outFile := filepath.Join(tmp, "out")
	logFile := filepath.Join(spawned.Cwd, "agent.log")
	cmd := exec.Command("sh", "-c", `exec 3>>agent.log
while [ ! -f "$1" ]; do
  git status --porcelain >&3 || exit 1
  git rev-parse --abbrev-ref HEAD >&3 || exit 1
  sleep 0.05
done
echo still-working > wip.txt
git add wip.txt
git commit --author="test <test@test>" -m "agent kept working" >/dev/null
git rev-parse --abbrev-ref HEAD > "$2"
pwd >> "$2"`, "agent", goFile, outFile)
	cmd.Dir = spawned.Cwd
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(logFile); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("agent loop never started")
		}
		time.Sleep(20 * time.Millisecond)
	}

	writeName(t, m, spawned.ID, "audit the poller")
	sess := spawned
	if err := m.poller.applyPendingRename(&sess); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if err := os.WriteFile(goFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("release process: %v", err)
	}
	select {
	case err := <-waited:
		if err != nil {
			t.Fatalf("agent after rename: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("agent did not finish after rename")
	}

	if _, err := os.Stat(filepath.Join(spawned.Cwd, "wip.txt")); err != nil {
		t.Fatalf("spawn-time path lost the agent's write: %v", err)
	}
	assertPaneStayedOnSpawnPath(t, m, spawned.ID, spawned.Cwd)
	head, err := exec.Command("git", "-C", spawned.Cwd, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil || strings.TrimSpace(string(head)) != "am/audit-the-poller" {
		t.Fatalf("spawn-time path HEAD = %q err=%v", strings.TrimSpace(string(head)), err)
	}
}

func TestPendingRenameOnATakenWorktreeNameStopsAfterOneReport(t *testing.T) {
	m := buildModel(t)
	repo := seedRepo(t)
	spawned := createWorktreeSession(t, m, "mover", repo)
	createWorktreeSession(t, m, "taken", repo)
	request := writeName(t, m, spawned.ID, "taken")

	sess := spawned
	if err := m.poller.applyPendingRename(&sess); err == nil {
		t.Fatal("a taken worktree name should report why")
	}
	if _, _, found := m.hooks.ReadName(spawned.ID); found {
		t.Fatal("the name file must be consumed so later polls are not stuck on it")
	}
	stored, err := m.store.Get(spawned.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.Name != "mover" || stored.Cwd != spawned.Cwd || stored.WorktreeBranch != spawned.WorktreeBranch {
		t.Fatalf("refused rename still moved something: %+v", stored)
	}
	verdict, found, err := m.hooks.ReadNameResult(spawned.ID, request)
	if err != nil || !found || verdict.Refusal == nil || verdict.Requested != "taken" || !strings.Contains(verdict.Refusal.Error(), "branch already exists") {
		t.Fatalf("the agent that asked must hear the refusal: %+v found=%v err=%v", verdict, found, err)
	}

	// The next poll runs clean, so one bad name does not stall the loop.
	if err := m.poller.applyPendingRename(&sess); err != nil {
		t.Fatalf("second pass: %v", err)
	}
}

func TestPendingRenameReportsTheAppliedName(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "claude-7a72", t.TempDir(), "")
	sess := m.sessionRows()[0]
	request := writeName(t, m, sess.ID, "audit   the poller")

	if err := m.poller.applyPendingRename(&sess); err != nil {
		t.Fatalf("rename: %v", err)
	}
	verdict, found, err := m.hooks.ReadNameResult(sess.ID, request)
	if err != nil || !found || verdict.Refusal != nil || verdict.Applied != "audit the poller" || verdict.Requested != "audit the poller" {
		t.Fatalf("result = %+v, %v, %v; want the asked and applied names", verdict, found, err)
	}
}

// A session whose agent has checked out another branch, or sits detached
// mid-rebase, still takes its new name: the am/ branch is left as it is,
// the way a hand-renamed one is.
func TestPendingRenameOnAWorktreeOffItsBranchTakesTheName(t *testing.T) {
	cases := []struct {
		label    string
		leave    []string
		wantHead string
	}{
		{"switched", []string{"switch", "-c", "pr-11442"}, "pr-11442"},
		{"detached", []string{"checkout", "--detach"}, "HEAD"},
	}
	for _, testCase := range cases {
		t.Run(testCase.label, func(t *testing.T) {
			m := buildModel(t)
			repo := seedRepo(t)
			spawned := createWorktreeSession(t, m, "pr-11442-rebase", repo)
			runGit(t, spawned.Cwd, testCase.leave...)
			before := gitOutput(t, spawned.Cwd, "rev-parse", "HEAD")
			request := writeName(t, m, spawned.ID, "SCT-11-cpu-perf")

			sess := spawned
			if err := m.poller.applyPendingRename(&sess); err != nil {
				t.Fatalf("rename: %v", err)
			}
			stored, err := m.store.Get(spawned.ID)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if stored.Name != "SCT-11-cpu-perf" {
				t.Fatalf("name = %q, want the new name", stored.Name)
			}
			if stored.WorktreeBranch != spawned.WorktreeBranch || stored.Cwd != spawned.Cwd {
				t.Fatalf("a worktree off its branch must be left alone: %+v", stored)
			}
			if head := gitOutput(t, spawned.Cwd, "rev-parse", "--abbrev-ref", "HEAD"); head != testCase.wantHead {
				t.Fatalf("checkout = %q, want %q", head, testCase.wantHead)
			}
			if after := gitOutput(t, spawned.Cwd, "rev-parse", "HEAD"); after != before {
				t.Fatalf("the worktree moved from %q to %q", before, after)
			}
			verdict, found, err := m.hooks.ReadNameResult(spawned.ID, request)
			if err != nil || !found || verdict.Refusal != nil || verdict.Applied != "SCT-11-cpu-perf" || verdict.Requested != "SCT-11-cpu-perf" {
				t.Fatalf("result = %+v, %v, %v", verdict, found, err)
			}
		})
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

// A verdict the manager cannot write keeps the rename claimed, so the
// agent waiting on it hears the answer on a later poll instead of timing
// out on a rename that was in fact applied.
func TestPendingRenameKeepsItsClaimUntilTheVerdictLands(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "claude-7a72", t.TempDir(), "")
	sess := m.sessionRows()[0]
	request := writeName(t, m, sess.ID, "audit the poller")
	// A directory in the verdict's place is what an unwritable result
	// looks like from here: the claim is still takeable, the answer is not.
	blocked := m.hooks.NameResultFile(sess.ID, request)
	if err := os.MkdirAll(blocked, 0o755); err != nil {
		t.Fatalf("block the result: %v", err)
	}

	if err := m.poller.applyPendingRename(&sess); err == nil {
		t.Fatal("a verdict that cannot be written should fail the pass")
	}
	stored, err := m.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.Name != "audit the poller" {
		t.Fatalf("name = %q, want the rename applied", stored.Name)
	}

	if err := os.Remove(blocked); err != nil {
		t.Fatalf("unblock: %v", err)
	}
	if err := m.poller.applyPendingRename(&sess); err != nil {
		t.Fatalf("second pass: %v", err)
	}
	verdict, found, err := m.hooks.ReadNameResult(sess.ID, request)
	if err != nil || !found || verdict.Refusal != nil || verdict.Requested != "audit the poller" || verdict.Applied != "audit the poller" {
		t.Fatalf("result = %+v, %v, %v; want the answer the first pass could not write", verdict, found, err)
	}
	if _, _, found, _ := m.hooks.ClaimName(sess.ID); found {
		t.Fatal("the answered rename should no longer be claimed")
	}
}

// writeName queues a rename the way the subcommand does, and returns the
// request its answer comes back under.
func writeName(t *testing.T, m *Model, id, name string) string {
	t.Helper()
	path := m.hooks.NameFile(id)
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

func writeHookStatus(t *testing.T, m *Model, id, state string) {
	t.Helper()
	path := m.hooks.StatusFile(id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir hooks dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(state), 0o644); err != nil {
		t.Fatalf("write hook status: %v", err)
	}
}

func deriveStatus(t *testing.T, m *Model, sess store.Session, pane string, agentAlive bool) string {
	t.Helper()
	got, err := m.poller.derivePaneStatus(sess, pane, agentAlive, map[string]uint64{})
	if err != nil {
		t.Fatalf("derivePaneStatus: %v", err)
	}
	return got
}

func TestHookStatusDerivesFinishedAndIdleWhenAcked(t *testing.T) {
	m := buildModel(t)
	sess := store.Session{ID: "hooked01", Tool: "claude-hooked"}
	pane := "some output\n❯ \n"
	writeHookStatus(t, m, sess.ID, status.Finished)

	if got := deriveStatus(t, m, sess, pane, true); got != status.Finished {
		t.Fatalf("hook finished should derive finished, got %q", got)
	}

	sess.Acked = true
	if got := deriveStatus(t, m, sess, pane, true); got != status.Idle {
		t.Fatalf("acked hook finished should derive idle, got %q", got)
	}
}

func TestHookWorkingWinsOverUnmatchedPane(t *testing.T) {
	m := buildModel(t)
	sess := store.Session{ID: "hooked02", Tool: "claude-hooked"}
	writeHookStatus(t, m, sess.ID, status.Working)

	pane := "plain streaming text no rule matches\n❯ \n"
	if got := deriveStatus(t, m, sess, pane, true); got != status.Working {
		t.Fatalf("hook working should win, got %q", got)
	}
}

func TestHookFinishedUpgradesToWaitingOnQuestionTurn(t *testing.T) {
	m := buildModel(t)
	sess := store.Session{ID: "hooked03", Tool: "claude-hooked"}
	writeHookStatus(t, m, sess.ID, status.Finished)

	pane := "Do you want me to proceed?\n\n✻ Baked for 5s\n\n❯ \n"
	if got := deriveStatus(t, m, sess, pane, true); got != status.Waiting {
		t.Fatalf("question turn should upgrade hook finished to waiting, got %q", got)
	}
}

func TestHookFinishedUpgradesToErroredOnErrorLine(t *testing.T) {
	m := buildModel(t)
	sess := store.Session{ID: "hooked04", Tool: "claude-hooked"}
	writeHookStatus(t, m, sess.ID, status.Finished)

	pane := "error: something broke\n❯ \n"
	if got := deriveStatus(t, m, sess, pane, true); got != status.Errored {
		t.Fatalf("error line should upgrade hook finished to errored, got %q", got)
	}
}

func TestHookWorkingUpgradesToWaitingOnPaneMatch(t *testing.T) {
	m := buildModel(t)
	sess := store.Session{ID: "hooked05", Tool: "claude-hooked"}
	writeHookStatus(t, m, sess.ID, status.Working)

	pane := "Enter to confirm\n❯ \n"
	if got := deriveStatus(t, m, sess, pane, true); got != status.Waiting {
		t.Fatalf("waiting pane verdict should upgrade hook working, got %q", got)
	}
}

func TestHookWorkingReconcilesToFinishedOnEndedTurn(t *testing.T) {
	m := buildModel(t)
	sess := store.Session{ID: "hooked07", Tool: "claude-hooked", Status: status.Working}
	writeHookStatus(t, m, sess.ID, status.Working)

	pane := "here is the result\n\n✻ Baked for 5s\n\n❯ \n"
	if got := deriveStatus(t, m, sess, pane, true); got != status.Finished {
		t.Fatalf("stale working hook over an ended turn should reconcile to finished, got %q", got)
	}
}

func TestHookWorkingReconcilesPastClaudeUpdateBanner(t *testing.T) {
	m := buildModel(t)
	defaultEngine(t, m)
	m.poller.statusSources["claude"] = hooks.StatusSourceClaude
	sess := store.Session{ID: "hooked-update", Tool: "claude", Status: status.Working}
	writeHookStatus(t, m, sess.ID, status.Working)

	pane := "done\n\n✻ Sautéed for 2m 58s · done 23:03\n" +
		"✔ Update installed · Restart to update\n" +
		"────\n❯\u00a0\n────\n  ⏵⏵ auto mode on (shift+tab to cycle)"
	if got := deriveStatus(t, m, sess, pane, true); got != status.Finished {
		t.Fatalf("update banner left stale working hook at %q, want finished", got)
	}
}

// Claude fires Stop when the main agent stops responding, so a turn that
// leaves background agents running reports finished while they work. The
// pane still shows the wait, and that verdict has to win.
func TestHookErroredReconcilesToPaneVerdict(t *testing.T) {
	m := buildModel(t)
	sess := store.Session{ID: "hooked-err", Tool: "claude-hooked"}
	writeHookStatus(t, m, sess.ID, status.Errored)

	if got := deriveStatus(t, m, sess, "Enter to confirm\n❯ \n", true); got != status.Waiting {
		t.Fatalf("waiting pane should override hook errored, got %q", got)
	}
	if got := deriveStatus(t, m, sess,
		"⏺ Security agent done. 2 left.\n✻ Waiting for 2 background agents to finish\n❯ \n", true); got != status.Working {
		t.Fatalf("working pane should override hook errored, got %q", got)
	}
	if got := deriveStatus(t, m, sess, "here is the result\n\n✻ Baked for 5s\n\n❯ \n", true); got != status.Finished {
		t.Fatalf("finished pane should override hook errored, got %q", got)
	}

	sess.Acked = true
	if got := deriveStatus(t, m, sess, "here is the result\n\n✻ Baked for 5s\n\n❯ \n", true); got != status.Idle {
		t.Fatalf("acked finished pane should idle over hook errored, got %q", got)
	}
}

func TestHookFinishedUpgradesToErroredOnUsageLimit(t *testing.T) {
	m := buildModel(t)
	sess := store.Session{ID: "hooked-limit", Tool: "claude-hooked"}
	writeHookStatus(t, m, sess.ID, status.Finished)

	pane := "  ⎿  You've hit your weekly limit · resets 1am (Asia/Jerusalem)\n\n" +
		"✻ Churned for 2h 0m 54s\n❯ \n"
	if got := deriveStatus(t, m, sess, pane, true); got != status.Errored {
		t.Fatalf("a usage limit should read as errored, got %q", got)
	}
}

func TestHookFinishedUpgradesToWorkingWhileBackgroundAgentsRun(t *testing.T) {
	m := buildModel(t)
	sess := store.Session{ID: "hooked08", Tool: "claude-hooked"}
	writeHookStatus(t, m, sess.ID, status.Finished)

	pane := "⏺ Security agent done. 2 left.\n✻ Waiting for 2 background agents to finish\n❯ \n"
	if got := deriveStatus(t, m, sess, pane, true); got != status.Working {
		t.Fatalf("background agents still running should upgrade hook finished to working, got %q", got)
	}

	sess.Acked = true
	if got := deriveStatus(t, m, sess, pane, true); got != status.Working {
		t.Fatalf("an acked session with background agents running should still read working, got %q", got)
	}
}

// A background shell outlives its turn the same way, and Stop fires the
// moment the main agent stops responding, so the hook reports finished
// while the shell runs and the notification for it would fire early.
func TestHookFinishedUpgradesToWorkingWhileBackgroundShellsRun(t *testing.T) {
	m := buildModel(t)
	sess := store.Session{ID: "hooked10", Tool: "claude-hooked"}
	writeHookStatus(t, m, sess.ID, status.Finished)

	pane := "⏺ ok\n✻ Worked for 3s · 1 shell still running\n❯ \n"
	if got := deriveStatus(t, m, sess, pane, true); got != status.Working {
		t.Fatalf("a running background shell should upgrade hook finished to working, got %q", got)
	}
}

// The wait line disappears once the agents drain, and the completed turn
// below it must settle back to the hook's own verdict.
func TestHookFinishedStaysFinishedOnceBackgroundAgentsDrain(t *testing.T) {
	m := buildModel(t)
	sess := store.Session{ID: "hooked09", Tool: "claude-hooked"}
	writeHookStatus(t, m, sess.ID, status.Finished)

	pane := "⏺ all agents reported\n✻ Worked for 5s\n❯ \n"
	if got := deriveStatus(t, m, sess, pane, true); got != status.Finished {
		t.Fatalf("drained background agents should read finished, got %q", got)
	}
}

func TestStaleHookFileFallsBackToPaneRules(t *testing.T) {
	m := buildModel(t)
	sess := store.Session{ID: "hooked06", Tool: "claude-hooked"}
	writeHookStatus(t, m, sess.ID, status.Working)

	pane := "shell prompt after a crash\n❯ \n"
	if got := deriveStatus(t, m, sess, pane, false); got != status.Idle {
		t.Fatalf("dead agent should fall back to pane rules, got %q", got)
	}
	if _, ok := m.hooks.Read(sess.ID); ok {
		t.Fatal("stale hook status file should be removed")
	}
}

func seedRegionHash(t *testing.T, m *Model, sess store.Session, pane string) {
	t.Helper()
	region, ok := m.poller.engine.ActivityRegion(sess.Tool, ansi.Strip(pane))
	if !ok {
		t.Fatal("pane should have an activity region")
	}
	m.poller.paneHashes = map[string]uint64{sess.ID: hashString(region)}
}

func disableQuietEndGrace(t *testing.T) {
	t.Helper()
	prev := quietEndGrace
	quietEndGrace = 0
	t.Cleanup(func() { quietEndGrace = prev })
}

func disableStuckEndGrace(t *testing.T) {
	t.Helper()
	prev := stuckEndGrace
	stuckEndGrace = 0
	t.Cleanup(func() { stuckEndGrace = prev })
}

func defaultEngine(t *testing.T, m *Model) {
	t.Helper()
	cfg, err := config.Default()
	if err != nil {
		t.Fatalf("default config: %v", err)
	}
	engine, err := status.NewEngine(cfg)
	if err != nil {
		t.Fatalf("status engine: %v", err)
	}
	m.poller.engine = engine
}

func TestQuietPaneAfterWorkingDerivesFinished(t *testing.T) {
	disableQuietEndGrace(t)
	m := buildModel(t)
	sess := store.Session{ID: "quiet01", Tool: "claude-hooked", Status: status.Working}
	pane := "final answer with no turn marker\n❯ \n"
	seedRegionHash(t, m, sess, pane)
	if got := deriveStatus(t, m, sess, pane, true); got != status.Finished {
		t.Fatalf("quiet pane after working should derive finished, got %q", got)
	}
}

func TestQuietCodexPaneQuotingInterruptHintDerivesFinished(t *testing.T) {
	disableQuietEndGrace(t)
	m := buildModel(t)
	defaultEngine(t, m)
	sess := store.Session{ID: "quiet-codex", Tool: "codex", Status: status.Working}
	pane := "Output:\n\ntool: mytool\nresult: working\npattern: esc to interrupt\ndefault: idle\n\n› Summarize recent commits\n"
	seedRegionHash(t, m, sess, pane)
	if got := deriveStatus(t, m, sess, pane, true); got != status.Finished {
		t.Fatalf("quiet Codex pane quoting its interrupt hint should derive finished, got %q", got)
	}
}

func TestQuietPaneHoldsWorkingUntilGrace(t *testing.T) {
	prev := quietEndGrace
	quietEndGrace = time.Hour
	t.Cleanup(func() { quietEndGrace = prev })
	m := buildModel(t)
	sess := store.Session{ID: "quiet-hold", Tool: "claude-hooked", Status: status.Working}
	pane := "final answer with no turn marker\n❯ \n"
	seedRegionHash(t, m, sess, pane)
	if got := deriveStatus(t, m, sess, pane, true); got != status.Working {
		t.Fatalf("first quiet poll within grace should stay working, got %q", got)
	}
}

func TestQuietPaneEndingOnQuestionDerivesWaiting(t *testing.T) {
	disableQuietEndGrace(t)
	m := buildModel(t)
	sess := store.Session{ID: "quiet02", Tool: "claude-hooked", Status: status.Working}
	pane := "Which of the two options do you prefer?\n❯ \n"
	seedRegionHash(t, m, sess, pane)
	if got := deriveStatus(t, m, sess, pane, true); got != status.Waiting {
		t.Fatalf("quiet pane ending on a question should derive waiting, got %q", got)
	}
}

func TestQuietPaneAfterIdleStaysIdle(t *testing.T) {
	m := buildModel(t)
	sess := store.Session{ID: "quiet03", Tool: "claude-hooked", Status: status.Idle}
	pane := "old transcript text\n❯ \n"
	seedRegionHash(t, m, sess, pane)
	if got := deriveStatus(t, m, sess, pane, true); got != status.Idle {
		t.Fatalf("quiet pane after idle should stay idle, got %q", got)
	}
}

// An opencode turn that dies before its header row gains a duration leaves
// a bare spinner row behind, which keeps matching the working rule forever.
// Once the region stops changing for the stuck grace the quiet-region path
// settles it from the region's own last content line.
func TestStuckOpencodeSpinnerSettlesFinished(t *testing.T) {
	disableStuckEndGrace(t)
	m := buildModel(t)
	defaultEngine(t, m)
	sess := store.Session{ID: "stuck-oc", Tool: "opencode", Status: status.Working}
	pane := "▣  Build · Ox Alpha Free (Unlimited)\n┃\n╹▀▀▀▀\n"
	seedRegionHash(t, m, sess, pane)
	if got := deriveStatus(t, m, sess, pane, true); got != status.Finished {
		t.Fatalf("stuck spinner past the grace should settle finished, got %q", got)
	}
}

func TestStuckSpinnerHoldsWorkingUntilGrace(t *testing.T) {
	prev := stuckEndGrace
	stuckEndGrace = time.Hour
	t.Cleanup(func() { stuckEndGrace = prev })
	m := buildModel(t)
	defaultEngine(t, m)
	sess := store.Session{ID: "stuck-hold", Tool: "opencode", Status: status.Working}
	pane := "▣  Build · Ox Alpha Free (Unlimited)\n┃\n╹▀▀▀▀\n"
	seedRegionHash(t, m, sess, pane)
	if got := deriveStatus(t, m, sess, pane, true); got != status.Working {
		t.Fatalf("matched spinner within the grace should stay working, got %q", got)
	}
}

// A rule-matched working verdict wins over a resting stored status: a
// session that already read finished or waiting but whose newest turn now
// matches a working rule is working again, on the first stable observation.
func TestMatchedWorkingWinsOverRestingStatus(t *testing.T) {
	m := buildModel(t)
	defaultEngine(t, m)
	pane := "▣  Build · Ox Alpha Free (Unlimited)\n┃\n╹▀▀▀▀\n"
	for i, stored := range []string{status.Finished, status.Waiting} {
		sess := store.Session{ID: fmt.Sprintf("win-%d", i), Tool: "opencode", Status: stored}
		seedRegionHash(t, m, sess, pane)
		if got := deriveStatus(t, m, sess, pane, true); got != status.Working {
			t.Fatalf("stored %s with a matched working pane should read working, got %q", stored, got)
		}
	}
}

// A tool without turn_end matches its rules over the whole pane, so a pane
// can flip from unmatched to rule-matched working while the activity-region
// hash stays put (the change lives below the cutoff, as gemini's status line
// does). The stuck grace must start fresh rather than inherit the unmatched
// quiet timer.
func TestMatchedWorkingAfterQuietRestartsGrace(t *testing.T) {
	prevQuiet, prevStuck := quietEndGrace, stuckEndGrace
	quietEndGrace = time.Hour
	stuckEndGrace = time.Nanosecond
	t.Cleanup(func() {
		quietEndGrace = prevQuiet
		stuckEndGrace = prevStuck
	})
	m := buildModel(t)
	defaultEngine(t, m)
	sess := store.Session{ID: "flip-gemini", Tool: "gemini", Status: status.Working}
	quietPane := "completed output\n> \n"
	spinnerPane := "completed output\n> \nesc to cancel\n"
	seedRegionHash(t, m, sess, quietPane)
	if got := deriveStatus(t, m, sess, quietPane, true); got != status.Working {
		t.Fatalf("quiet unmatched pane within its grace should stay working, got %q", got)
	}
	if got := deriveStatus(t, m, sess, spinnerPane, true); got != status.Working {
		t.Fatalf("a fresh stuck grace should hold working, got %q", got)
	}
}

// pi anchors its activity region at the pane origin, so the region never
// changes and the spinner animates below it. The stuck grace restarts on
// any pane change, and only a pane frozen for the whole grace settles.
func TestSpinnerAnimatingBelowTheRegionStaysWorking(t *testing.T) {
	prev := stuckEndGrace
	stuckEndGrace = time.Minute
	t.Cleanup(func() { stuckEndGrace = prev })
	m := buildModel(t)
	defaultEngine(t, m)
	sess := store.Session{ID: "anim-pi", Tool: "pi", Status: status.Working}
	footer := "\n──────────────────────────────\n~/dev/project (main)\n↑116 ↓26k $2.862 (sub) 6.2%/1.0M (auto)\n"
	still := "── ⠋ Working ─────────────────" + footer
	moved := "── ⠹ Working ─────────────────" + footer
	seedRegionHash(t, m, sess, still)
	expired := quietTimer{since: time.Now().Add(-2 * time.Minute), stuck: true, pane: hashString(still)}
	m.poller.quietSince[sess.ID] = expired
	if got := deriveStatus(t, m, sess, moved, true); got != status.Working {
		t.Fatalf("a spinner frame change past the grace should restart it and stay working, got %q", got)
	}
	m.poller.quietSince[sess.ID] = expired
	if got := deriveStatus(t, m, sess, still, true); got != status.Finished {
		t.Fatalf("a pane frozen for the whole grace should settle finished, got %q", got)
	}
}

// A live agent animates its pane every poll or two; a changing region must
// keep a rule-matched working verdict working no matter how long it runs.
func TestAnimatedMatchedWorkingStaysWorking(t *testing.T) {
	m := buildModel(t)
	defaultEngine(t, m)
	sess := store.Session{ID: "anim-oc", Tool: "opencode", Status: status.Working}
	still := "⠋ listing files\n▣  Build · Ox Alpha Free (Unlimited)\n┃\n╹▀▀▀▀\n"
	moved := "⠙ listing files\n▣  Build · Ox Alpha Free (Unlimited)\n┃\n╹▀▀▀▀\n"
	seedRegionHash(t, m, sess, still)
	if got := deriveStatus(t, m, sess, moved, true); got != status.Working {
		t.Fatalf("an animating matched-working pane should stay working, got %q", got)
	}
}

func TestQuietFinishedPersistsAndAckMapsToIdle(t *testing.T) {
	m := buildModel(t)
	sess := store.Session{ID: "quiet04", Tool: "claude-hooked", Status: status.Finished}
	pane := "final answer with no turn marker\n❯ \n"
	seedRegionHash(t, m, sess, pane)
	if got := deriveStatus(t, m, sess, pane, true); got != status.Finished {
		t.Fatalf("inferred finished should persist while the pane stays quiet, got %q", got)
	}
	sess.Acked = true
	if got := deriveStatus(t, m, sess, pane, true); got != status.Idle {
		t.Fatalf("acked inferred finished should derive idle, got %q", got)
	}
}

func TestChangedRegionStillDerivesWorking(t *testing.T) {
	m := buildModel(t)
	sess := store.Session{ID: "quiet05", Tool: "claude-hooked", Status: status.Working}
	seedRegionHash(t, m, sess, "earlier streaming text\n❯ \n")
	if got := deriveStatus(t, m, sess, "earlier streaming text plus more\n❯ \n", true); got != status.Working {
		t.Fatalf("changed region should derive working, got %q", got)
	}
}

// A post-resize rebaseline (no prior hash) must keep finished instead of
// inventing working from reflowed content or collapsing to idle.
func TestRebaselineKeepsFinishedWithoutFlashingWorking(t *testing.T) {
	m := buildModel(t)
	sess := store.Session{ID: "reflow01", Tool: "claude-hooked", Status: status.Finished}
	before := "final answer line that wraps differently after resize\n❯ \n"
	after := "final answer line that wraps\ndifferently after resize\n❯ \n"
	seedRegionHash(t, m, sess, before)
	// Without clearing, a reflow looks like streaming work.
	if got := deriveStatus(t, m, sess, after, true); got != status.Working {
		t.Fatalf("reflow with a prior hash should look like working (precondition), got %q", got)
	}
	seedRegionHash(t, m, sess, before)
	m.poller.reflowSessions([]string{sess.ID}, func() {})
	if got := deriveStatus(t, m, sess, after, true); got != status.Finished {
		t.Fatalf("rebaseline after resize must keep finished, got %q", got)
	}
}

func TestRebaselineKeepsWaitingAndWorking(t *testing.T) {
	m := buildModel(t)
	pane := "Which option do you prefer?\n❯ \n"
	for _, st := range []string{status.Waiting, status.Working} {
		sess := store.Session{ID: "reflow-" + st, Tool: "claude-hooked", Status: st}
		if got := deriveStatus(t, m, sess, pane, true); got != st {
			t.Fatalf("unseen baseline with status %q: got %q", st, got)
		}
	}
}

func TestRebaselineIdleStaysIdle(t *testing.T) {
	m := buildModel(t)
	sess := store.Session{ID: "reflow-idle", Tool: "claude-hooked", Status: status.Idle}
	pane := "old transcript text\n❯ \n"
	if got := deriveStatus(t, m, sess, pane, true); got != status.Idle {
		t.Fatalf("unseen baseline idle should stay idle, got %q", got)
	}
}

func TestLiveQuietTurnResolvesFinished(t *testing.T) {
	m := buildModel(t)
	m.openForm()
	m.form.name.SetValue("quiet-live")
	m.form.dir.SetValue(t.TempDir())
	for i, name := range sortedToolNames(m.cfg) {
		if name == "quietchat" {
			m.form.toolIndex = i
		}
	}
	pickGroup(t, m, "")
	_, cmd := m.submitForm()
	if m.mode != modeList {
		t.Fatalf("after submit, mode = %v, err = %q", m.mode, m.errBar.text)
	}
	m.applyCmd(t, cmd)
	sess := m.sessionRows()[0]

	send := func(text string) {
		t.Helper()
		if err := m.tmux.SendText(sess.ID, text); err != nil {
			t.Fatalf("send %q: %v", text, err)
		}
	}
	waitStatus := func(want string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			m.applyCmd(t, m.refreshCmd())
			got, err := m.store.Get(sess.ID)
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if got.Status == want {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("status = %q, want %q", got.Status, want)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	send("first answer chunk")
	send("› ask anything")
	m.applyCmd(t, m.refreshCmd())

	send("more streaming output")
	send("› ask anything")
	waitStatus(status.Working)
	waitStatus(status.Finished)
}

func TestRefreshAppliesPendingRename(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "placeholder", t.TempDir(), "")
	sess := m.sessionRows()[0]

	writeName(t, m, sess.ID, "fix auth bug\n")
	m.applyCmd(t, m.refreshCmd())

	got, err := m.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "fix auth bug" {
		t.Fatalf("name = %q, want the agent-chosen name", got.Name)
	}
	if _, err := os.Stat(m.hooks.NameFile(sess.ID)); !os.IsNotExist(err) {
		t.Fatal("applied name file should be consumed")
	}
}

func TestRefreshConsumesGarbageNameFile(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "keeper", t.TempDir(), "")
	sess := m.sessionRows()[0]

	writeName(t, m, sess.ID, "   \n")
	m.applyCmd(t, m.refreshCmd())

	got, err := m.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "keeper" {
		t.Fatalf("whitespace name must not rename, got %q", got.Name)
	}
	if _, err := os.Stat(m.hooks.NameFile(sess.ID)); !os.IsNotExist(err) {
		t.Fatal("garbage name file should still be consumed")
	}
}

func TestRefreshWithStaleSelectionFetchesPreview(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "fresh-one", t.TempDir(), "")
	m.selectSessionRow(t, "fresh-one")
	sess := m.sessionRows()[0]

	_, cmd := m.Update(refreshMsg{sessions: m.sessions, procFor: ""})
	if cmd == nil {
		t.Fatal("stale refresh should schedule an immediate preview fetch")
	}
	if m.poller.selectedID != sess.ID {
		t.Fatalf("poller selectedID = %q want %q", m.poller.selectedID, sess.ID)
	}

	m.preview = "existing"
	if _, cmd := m.Update(refreshMsg{sessions: m.sessions, procFor: sess.ID, preview: "pane text"}); cmd != nil {
		t.Fatal("matching refresh should not schedule extra work")
	}
	if m.preview != "pane text" {
		t.Fatalf("preview = %q want %q", m.preview, "pane text")
	}
}

func TestSweepPastesReportsSweepError(t *testing.T) {
	orig := sweepStalePastes
	defer func() { sweepStalePastes = orig }()
	sweepStalePastes = func() error { return errors.New("permission denied") }

	m := &Model{}
	msg, ok := m.sweepPastes().(pasteSweepMsg)
	if !ok {
		t.Fatalf("want pasteSweepMsg, got %T", m.sweepPastes())
	}
	if msg.err == nil || msg.err.Error() != "permission denied" {
		t.Fatalf("got %v", msg.err)
	}
}

func TestPasteSweepMsgSurfacesErrorOnce(t *testing.T) {
	m := buildModel(t)
	m.Update(pasteSweepMsg{err: errors.New("permission denied")})
	if m.errBar.text == "" {
		t.Fatal("a failed sweep must reach the user")
	}
	m.errBar.text = ""
	m.Update(pasteSweepMsg{})
	if m.errBar.text != "" {
		t.Fatalf("a clean sweep must stay silent, got %q", m.errBar.text)
	}
}

func TestPasteSweepTickSweepsAgainAndRearms(t *testing.T) {
	m := buildModel(t)
	_, cmd := m.Update(pasteSweepTickMsg{})
	if cmd == nil {
		t.Fatal("tick must return work")
	}
	// A manager left open for weeks only keeps sweeping if the tick both
	// sweeps and re-arms, so the batch must carry two commands. Running the
	// timer itself here would wait out the real interval.
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("want a batch, got %T", cmd())
	}
	if len(batch) != 2 {
		t.Fatalf("want sweep plus re-arm, got %d commands", len(batch))
	}
}

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
	panes := map[string]tmux.Pane{"sess-A": {PID: 123}, "sess-B": {PID: 456}}
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

// A restarted codex session still has its old rollout sitting in the same
// directory, written moments before the restart and so inside the capture
// window. Capture must bind the fresh conversation, not walk the session
// straight back into the context the restart dropped.
func TestCaptureAgentSessionIDsSkipsARetiredConversation(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	cwd := t.TempDir()

	created := time.Now().Add(-time.Hour)
	restarted := time.Now().Add(-time.Second)
	// The retired rollout's last write lands two seconds before the restart,
	// inside the clock slack a plain capture window would allow.
	oldMtime := restarted.Add(-2 * time.Second)
	writeCodexRollout(t, filepath.Join(codexHome, "sessions", "rollout-old.jsonl"), "old-id", cwd, oldMtime)
	writeCodexRollout(t, filepath.Join(codexHome, "sessions", "rollout-new.jsonl"), "new-id", cwd, restarted.Add(time.Second))

	if err := st.CreateSession(store.Session{ID: "sess", Name: "s", Tool: "codex", Cwd: cwd, Group: "g", Status: "idle", CreatedAt: created, AgentSessionID: "old-id"}); err != nil {
		t.Fatal(err)
	}
	if err := st.RestartAgent("sess", "", restarted); err != nil {
		t.Fatal(err)
	}
	// The pre-launch snapshot holds what the store had before the restart;
	// the fresh rollout is unseen, so it qualifies.
	if err := st.SetRelaunchSnapshot("sess", map[string]int64{"old-id": oldMtime.UnixNano()}); err != nil {
		t.Fatal(err)
	}
	sess, err := st.Get("sess")
	if err != nil {
		t.Fatal(err)
	}

	p := &poller{store: st, sessionStores: map[string]string{"codex": "codex"}, recaptureSeen: map[string]recaptureSighting{}}
	// First sighting stores the candidate without binding.
	if captured, err := p.captureAgentSessionIDs([]store.Session{sess}, map[string]tmux.Pane{"sess": {PID: 42}}); err != nil || captured != 0 {
		t.Fatalf("first pass captured %d err=%v, want 0 without a bind", captured, err)
	}
	// The second consecutive pass agrees and binds.
	if captured, err := p.captureAgentSessionIDs([]store.Session{sess}, map[string]tmux.Pane{"sess": {PID: 42}}); err != nil || captured != 1 {
		t.Fatalf("second pass captured %d err=%v, want 1", captured, err)
	}
	got, err := st.Get("sess")
	if err != nil {
		t.Fatal(err)
	}
	if got.AgentSessionID != "new-id" {
		t.Fatalf("captured %q, want new-id", got.AgentSessionID)
	}
	if len(got.RelaunchSnapshot) != 0 {
		t.Fatalf("a bound conversation must clear the snapshot, still holds %v", got.RelaunchSnapshot)
	}
}

// Capture reads a tool's session store from a snapshot and can take minutes,
// so a restart can land while a pass is still looking. The answer it comes
// back with names the conversation the restart dropped, and writing it would
// walk the row straight back into the context the user just left.
func TestCaptureAgentSessionIDsDropsAnAnswerARestartOutran(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	cwd := t.TempDir()
	created := time.Now().Add(-time.Hour)
	writeCodexRollout(t, filepath.Join(codexHome, "sessions", "rollout-old.jsonl"), "old-id", cwd, created)

	if err := st.CreateSession(store.Session{ID: "sess", Name: "s", Tool: "codex", Cwd: cwd, Group: "g", Status: "idle", CreatedAt: created}); err != nil {
		t.Fatal(err)
	}
	// The pass reads the session before the restart: no id yet, so it is a
	// capture candidate and the old rollout is the answer it will find.
	snapshot, err := st.Get("sess")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RestartAgent("sess", "", time.Now()); err != nil {
		t.Fatal(err)
	}

	p := &poller{store: st, sessionStores: map[string]string{"codex": "codex"}}
	captured, err := p.captureAgentSessionIDs([]store.Session{snapshot}, map[string]tmux.Pane{"sess": {PID: 7}})
	if err != nil {
		t.Fatal(err)
	}
	if captured != 0 {
		t.Fatalf("captured %d, want the stale answer dropped", captured)
	}
	got, err := st.Get("sess")
	if err != nil {
		t.Fatal(err)
	}
	if got.AgentSessionID != "" {
		t.Fatalf("restarted session bound to %q, want it left for the next pass", got.AgentSessionID)
	}
}

// A revived session resumes whatever conversation its picker opened, so the
// relaunch cannot use the earliest-write tie-break: several conversations
// touched after it means several candidates, and binding one anyway would
// gamble on the wrong context.
func TestCaptureAgentSessionIDsRefusesAnAmbiguousResume(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	cwd := t.TempDir()

	created := time.Now().Add(-time.Hour)
	relaunched := time.Now().Add(-time.Second)
	writeCodexRollout(t, filepath.Join(codexHome, "sessions", "rollout-1.jsonl"), "id-1", cwd, relaunched.Add(time.Second))
	writeCodexRollout(t, filepath.Join(codexHome, "sessions", "rollout-2.jsonl"), "id-2", cwd, relaunched.Add(2*time.Second))

	if err := st.CreateSession(store.Session{ID: "sess", Name: "s", Tool: "codex", Cwd: cwd, Group: "g", Status: "idle", CreatedAt: created}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetAgentLaunchedAt("sess", relaunched); err != nil {
		t.Fatal(err)
	}
	// The snapshot holds an empty pre-launch state, so both conversations
	// written after it qualify and neither may win.
	if err := st.SetRelaunchSnapshot("sess", map[string]int64{}); err != nil {
		t.Fatal(err)
	}
	sess, err := st.Get("sess")
	if err != nil {
		t.Fatal(err)
	}

	p := &poller{store: st, sessionStores: map[string]string{"codex": "codex"}, recaptureSeen: map[string]recaptureSighting{}}
	captured, err := p.captureAgentSessionIDs([]store.Session{sess}, map[string]tmux.Pane{"sess": {PID: 42}})
	if err != nil {
		t.Fatal(err)
	}
	if captured != 0 {
		t.Fatalf("captured %d, want the ambiguous resume refused", captured)
	}
	got, err := st.Get("sess")
	if err != nil {
		t.Fatal(err)
	}
	if got.AgentSessionID != "" {
		t.Fatalf("session bound to %q, want it left for the next pass", got.AgentSessionID)
	}
}

// With a single conversation whose activity outran the pre-launch snapshot
// the resumed session has exactly one answer, and two agreeing passes bind.
func TestCaptureAgentSessionIDsBindsTheSingleResumedConversation(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	cwd := t.TempDir()

	created := time.Now().Add(-time.Hour)
	relaunched := time.Now().Add(-time.Second)
	// The conversation sits untouched through the pre-launch snapshot, then
	// turns again after the relaunch: the snapshot's value is what the
	// resumed write must outrun.
	preMtime := relaunched.Add(-time.Hour)
	writeCodexRollout(t, filepath.Join(codexHome, "sessions", "rollout-1.jsonl"), "resumed-id", cwd, preMtime)

	if err := st.CreateSession(store.Session{ID: "sess", Name: "s", Tool: "codex", Cwd: cwd, Group: "g", Status: "idle", CreatedAt: created}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetAgentLaunchedAt("sess", relaunched); err != nil {
		t.Fatal(err)
	}
	if err := st.SetRelaunchSnapshot("sess", map[string]int64{"resumed-id": preMtime.UnixNano()}); err != nil {
		t.Fatal(err)
	}
	writeCodexRollout(t, filepath.Join(codexHome, "sessions", "rollout-1.jsonl"), "resumed-id", cwd, relaunched.Add(time.Second))
	sess, err := st.Get("sess")
	if err != nil {
		t.Fatal(err)
	}

	p := &poller{store: st, sessionStores: map[string]string{"codex": "codex"}, recaptureSeen: map[string]recaptureSighting{}}
	if captured, err := p.captureAgentSessionIDs([]store.Session{sess}, map[string]tmux.Pane{"sess": {PID: 42}}); err != nil || captured != 0 {
		t.Fatalf("first pass captured %d err=%v, want 0 without a bind", captured, err)
	}
	if captured, err := p.captureAgentSessionIDs([]store.Session{sess}, map[string]tmux.Pane{"sess": {PID: 42}}); err != nil || captured != 1 {
		t.Fatalf("second pass captured %d err=%v, want 1", captured, err)
	}
	got, err := st.Get("sess")
	if err != nil {
		t.Fatal(err)
	}
	if got.AgentSessionID != "resumed-id" {
		t.Fatalf("captured %q, want resumed-id", got.AgentSessionID)
	}
}

// Two picker relaunches in one store and directory have indistinguishable
// activity. A choice made in the second pane must not bind to the first row.
func TestCaptureAgentSessionIDsRefusesSharedRelaunchScope(t *testing.T) {
	p, first := newTestPollerWithSession(t)
	p.sessionStores = map[string]string{"codex": "codex"}
	p.recaptureSeen = map[string]recaptureSighting{}
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)

	firstLaunch := time.Now().Add(-time.Second)
	secondLaunch := firstLaunch.Add(time.Millisecond)
	if err := p.store.SetAgentLaunchedAt(first.ID, firstLaunch); err != nil {
		t.Fatal(err)
	}
	if err := p.store.SetRelaunchSnapshot(first.ID, map[string]int64{}); err != nil {
		t.Fatal(err)
	}
	second := store.Session{ID: "sess-2", Name: "two", Tool: "codex", Cwd: first.Cwd, Group: "g", Status: "idle"}
	if err := p.store.CreateSession(second); err != nil {
		t.Fatal(err)
	}
	if err := p.store.SetAgentLaunchedAt(second.ID, secondLaunch); err != nil {
		t.Fatal(err)
	}
	if err := p.store.SetRelaunchSnapshot(second.ID, map[string]int64{}); err != nil {
		t.Fatal(err)
	}
	writeCodexRollout(t, filepath.Join(codexHome, "sessions", "rollout-selected.jsonl"), "selected-in-second", first.Cwd, secondLaunch.Add(time.Second))

	first, err := p.store.Get(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err = p.store.Get(second.ID)
	if err != nil {
		t.Fatal(err)
	}
	panes := map[string]tmux.Pane{first.ID: {PID: 41}, second.ID: {PID: 42}}
	for pass := 0; pass < 2; pass++ {
		if captured, err := p.captureAgentSessionIDs([]store.Session{first, second}, panes); err != nil || captured != 0 {
			t.Fatalf("pass %d captured %d err=%v, want no bind", pass+1, captured, err)
		}
	}
	for _, id := range []string{first.ID, second.ID} {
		got, err := p.store.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if got.AgentSessionID != "" {
			t.Fatalf("session %s bound %q, want no id", id, got.AgentSessionID)
		}
	}
}

// The relaunch snapshot lands before the launch timestamp, and a poll in
// that window must not fall back to normal capture: the snapshot already
// marks the relaunch, so a conversation merely predating it stays refused.
func TestCaptureAgentSessionIDsRefusesCaptureInTheStampingWindow(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	cwd := t.TempDir()

	created := time.Now().Add(-time.Hour)
	// Written a second before the row's birth: inside the slack a plain
	// capture window admits, so the stale path would bind it.
	oldMtime := created.Add(-time.Second)
	writeCodexRollout(t, filepath.Join(codexHome, "sessions", "rollout-old.jsonl"), "old-id", cwd, oldMtime)

	if err := st.CreateSession(store.Session{ID: "sess", Name: "s", Tool: "codex", Cwd: cwd, Group: "g", Status: "idle", CreatedAt: created}); err != nil {
		t.Fatal(err)
	}
	// The snapshot is persisted but the launch timestamp is not yet: the
	// pane-creation window the poll can observe.
	if err := st.SetRelaunchSnapshot("sess", map[string]int64{"old-id": oldMtime.UnixNano()}); err != nil {
		t.Fatal(err)
	}
	sess, err := st.Get("sess")
	if err != nil {
		t.Fatal(err)
	}

	p := &poller{store: st, sessionStores: map[string]string{"codex": "codex"}, recaptureSeen: map[string]recaptureSighting{}}
	if captured, err := p.captureAgentSessionIDs([]store.Session{sess}, map[string]tmux.Pane{"sess": {PID: 42}}); err != nil || captured != 0 {
		t.Fatalf("captured %d err=%v, want the stamping window refused", captured, err)
	}
	got, err := st.Get("sess")
	if err != nil {
		t.Fatal(err)
	}
	if got.AgentSessionID != "" {
		t.Fatalf("session bound %q, want no id before the launch stamp", got.AgentSessionID)
	}
}

func TestStableRecaptureDoesNotReuseASightingFromAnOlderLaunch(t *testing.T) {
	p := &poller{recaptureSeen: map[string]recaptureSighting{}}
	firstLaunch := time.Now()
	if p.stableRecapture("sess", firstLaunch, "candidate") {
		t.Fatal("first sighting should not bind")
	}
	if p.stableRecapture("sess", firstLaunch.Add(time.Second), "candidate") {
		t.Fatal("a new launch must start its own stability check")
	}
}

// A sighting is only the first of two agreeing passes: when the next pass
// finds ambiguity instead, the sighting is dropped rather than letting the
// earlier candidate slide through on a stale vote.
func TestCaptureAgentSessionIDsClearsTheSightingWhenAmbiguityFollows(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	cwd := t.TempDir()

	created := time.Now().Add(-time.Hour)
	relaunched := time.Now().Add(-time.Second)
	preMtime := relaunched.Add(-time.Hour)
	writeCodexRollout(t, filepath.Join(codexHome, "sessions", "rollout-1.jsonl"), "id-1", cwd, preMtime)
	writeCodexRollout(t, filepath.Join(codexHome, "sessions", "rollout-2.jsonl"), "id-2", cwd, preMtime)

	if err := st.CreateSession(store.Session{ID: "sess", Name: "s", Tool: "codex", Cwd: cwd, Group: "g", Status: "idle", CreatedAt: created}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetAgentLaunchedAt("sess", relaunched); err != nil {
		t.Fatal(err)
	}
	if err := st.SetRelaunchSnapshot("sess", map[string]int64{"id-1": preMtime.UnixNano(), "id-2": preMtime.UnixNano()}); err != nil {
		t.Fatal(err)
	}
	sess, err := st.Get("sess")
	if err != nil {
		t.Fatal(err)
	}

	p := &poller{store: st, sessionStores: map[string]string{"codex": "codex"}, recaptureSeen: map[string]recaptureSighting{}}
	writeCodexRollout(t, filepath.Join(codexHome, "sessions", "rollout-1.jsonl"), "id-1", cwd, relaunched.Add(time.Second))
	if captured, err := p.captureAgentSessionIDs([]store.Session{sess}, map[string]tmux.Pane{"sess": {PID: 42}}); err != nil || captured != 0 {
		t.Fatalf("first pass captured %d err=%v, want 0 without a bind", captured, err)
	}
	if p.recaptureSeen["sess"].agentID != "id-1" {
		t.Fatalf("first pass must store the id-1 sighting, got %+v", p.recaptureSeen["sess"])
	}
	// The second conversation turns too: the pass now sees two candidates
	// and must drop the stored sighting instead of honoring it.
	writeCodexRollout(t, filepath.Join(codexHome, "sessions", "rollout-2.jsonl"), "id-2", cwd, relaunched.Add(2*time.Second))
	if captured, err := p.captureAgentSessionIDs([]store.Session{sess}, map[string]tmux.Pane{"sess": {PID: 42}}); err != nil || captured != 0 {
		t.Fatalf("ambiguous pass captured %d err=%v, want 0", captured, err)
	}
	if _, still := p.recaptureSeen["sess"]; still {
		t.Fatal("an ambiguous pass must clear the stored sighting")
	}
	got, err := st.Get("sess")
	if err != nil {
		t.Fatal(err)
	}
	if got.AgentSessionID != "" {
		t.Fatalf("session bound to %q, want it left for the next pass", got.AgentSessionID)
	}
}

// A relaunched session with no pre-launch snapshot predates snapshot
// capture, and recapture must refuse rather than guess from a bare cutoff.
func TestCaptureAgentSessionIDsRefusesARelaunchWithoutASnapshot(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	cwd := t.TempDir()

	created := time.Now().Add(-time.Hour)
	relaunched := time.Now().Add(-time.Second)
	writeCodexRollout(t, filepath.Join(codexHome, "sessions", "rollout-1.jsonl"), "some-id", cwd, relaunched.Add(time.Second))

	if err := st.CreateSession(store.Session{ID: "sess", Name: "s", Tool: "codex", Cwd: cwd, Group: "g", Status: "idle", CreatedAt: created}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetAgentLaunchedAt("sess", relaunched); err != nil {
		t.Fatal(err)
	}
	sess, err := st.Get("sess")
	if err != nil {
		t.Fatal(err)
	}

	p := &poller{store: st, sessionStores: map[string]string{"codex": "codex"}, recaptureSeen: map[string]recaptureSighting{}}
	if captured, err := p.captureAgentSessionIDs([]store.Session{sess}, map[string]tmux.Pane{"sess": {PID: 42}}); err != nil || captured != 0 {
		t.Fatalf("captured %d err=%v, want 0 without a snapshot", captured, err)
	}
	got, err := st.Get("sess")
	if err != nil {
		t.Fatal(err)
	}
	if got.AgentSessionID != "" {
		t.Fatalf("session bound to %q, want it refused", got.AgentSessionID)
	}
}

// A spawn keeps the normal capture path: the exact-one guard answers resumed
// sessions only, so an old conversation whose rollout was touched after the
// launch still binds by earliest write, unchanged.
func TestCaptureAgentSessionIDsKeepsTheEarliestWriteForASpawn(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	cwd := t.TempDir()

	launch := time.Now().Add(-time.Second)
	// The old conversation's rollout was touched just after the launch, a
	// fresh one a moment later: the spawn binds the earliest write, which is
	// the old conversation here.
	writeCodexRollout(t, filepath.Join(codexHome, "sessions", "rollout-old.jsonl"), "old-id", cwd, launch.Add(time.Second))
	writeCodexRollout(t, filepath.Join(codexHome, "sessions", "rollout-new.jsonl"), "new-id", cwd, launch.Add(2*time.Second))

	if err := st.CreateSession(store.Session{ID: "sess", Name: "s", Tool: "codex", Cwd: cwd, Group: "g", Status: "idle", CreatedAt: launch}); err != nil {
		t.Fatal(err)
	}
	sess, err := st.Get("sess")
	if err != nil {
		t.Fatal(err)
	}

	p := &poller{store: st, sessionStores: map[string]string{"codex": "codex"}}
	captured, err := p.captureAgentSessionIDs([]store.Session{sess}, map[string]tmux.Pane{"sess": {PID: 42}})
	if err != nil {
		t.Fatal(err)
	}
	if captured != 1 {
		t.Fatalf("captured %d, want 1", captured)
	}
	got, err := st.Get("sess")
	if err != nil {
		t.Fatal(err)
	}
	if got.AgentSessionID != "old-id" {
		t.Fatalf("captured %q, want old-id", got.AgentSessionID)
	}
}

// The heartbeat is what tells a sender whether a manager is home, and it
// rides every poll pass. Stamping it on each one is a write transaction
// every couple of seconds for as long as the manager stays open, so it is
// allowed to age instead: its reader treats a stamp as fresh for far longer
// than one poll.
func TestThePollLeavesTheHeartbeatAloneBetweenStamps(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.refreshCmd())
	first, err := m.store.Setting(store.PollerHeartbeatKey)
	if err != nil {
		t.Fatal(err)
	}
	if first == "" {
		t.Fatal("the first poll left no heartbeat, so no sender can tell a manager is running")
	}

	m.applyCmd(t, m.refreshCmd())
	second, err := m.store.Setting(store.PollerHeartbeatKey)
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Fatalf("the poll behind it rewrote the heartbeat: %q then %q", first, second)
	}

	m.poller.heartbeatAt = time.Now().Add(-store.PollerHeartbeatPeriod - time.Second)
	m.applyCmd(t, m.refreshCmd())
	third, err := m.store.Setting(store.PollerHeartbeatKey)
	if err != nil {
		t.Fatal(err)
	}
	if third == first {
		t.Fatal("an aged heartbeat was never restamped, so a running manager reads as closed")
	}
}

// Taking the launch prompt clears the composer, so a directive delivered
// before then is discarded and has to wait for the prompt to reach output.
func TestPendingInputWaitsForTheLaunchPrompt(t *testing.T) {
	m := buildModel(t)
	if err := m.spawnSession("slow-take-tool", "slow-take-tool-abcd", t.TempDir(), "", "/compact", true, false); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	sess := m.sessionRows()[0]

	// The input line is drawn from the first frame, so without the wait the
	// directive would be gone by now.
	for tries := 0; tries < 3; tries++ {
		if !sessionHasPendingInput(t, m, sess.ID, launch.DeferredRenameDirective) {
			pane, _ := m.tmux.CapturePane(sess.ID)
			t.Fatalf("directive sent before the prompt was taken; pane:\n%s", pane)
		}
		time.Sleep(50 * time.Millisecond)
		m.applyCmd(t, m.refreshCmd())
	}

	deadline := time.Now().Add(5 * time.Second)
	for sessionHasPendingInput(t, m, sess.ID, launch.DeferredRenameDirective) {
		if time.Now().After(deadline) {
			pane, _ := m.tmux.CapturePane(sess.ID)
			t.Fatalf("directive never sent after the prompt was taken; pane:\n%s", pane)
		}
		time.Sleep(100 * time.Millisecond)
		m.applyCmd(t, m.refreshCmd())
	}
	pane, err := m.tmux.CapturePane(sess.ID)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if !strings.Contains(pane, "agent-manager rename") {
		t.Fatalf("pane should hold the directive, got:\n%s", pane)
	}
}

func TestPendingInputWaitsForBetweenTurn(t *testing.T) {
	m := buildModel(t)
	if err := m.spawnSession("ready-tool", "ready-tool-abcd", t.TempDir(), "", "", true, false); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	sess, err := m.store.Get(m.sessionRows()[0].ID)
	if err != nil {
		t.Fatal(err)
	}

	sent, err := m.poller.maybeSendPendingInputWhenReady(sess, "❯ ", status.Working, true)
	if err != nil {
		t.Fatalf("send while working: %v", err)
	}
	if sent {
		t.Fatal("pending input was delivered during a live turn")
	}
	queued, err := m.store.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(queued.PendingInputs) == 0 {
		t.Fatal("pending input was consumed during a live turn")
	}

	sent, err = m.poller.maybeSendPendingInputWhenReady(sess, "❯ ", status.Idle, true)
	if err != nil {
		t.Fatalf("send while idle: %v", err)
	}
	if !sent {
		t.Fatal("pending input was not delivered between turns")
	}
}

// A prompt that never reaches the pane, because it scrolled out or the agent
// never drew it, must not hold pending input past the grace.
func TestLaunchPromptTakenGivesUpAfterTheGrace(t *testing.T) {
	fresh := store.Session{LaunchPrompt: "/compact plan the sprint", CreatedAt: time.Now()}
	if launchPromptTaken(fresh, "no prompt here") {
		t.Fatal("pending input released before the prompt showed")
	}
	if !launchPromptTaken(fresh, "❯ /compact plan the sprint\nworking") {
		t.Fatal("prompt in the region should release pending input")
	}
	stale := store.Session{LaunchPrompt: fresh.LaunchPrompt, CreatedAt: time.Now().Add(-launchPromptGrace - time.Second)}
	if !launchPromptTaken(stale, "no prompt here") {
		t.Fatal("pending input still held after the grace")
	}
}

// guestPoller is a second manager reading the same store while talking to
// a tmux server of its own: the shape that stamped dead over every live
// session, so their own manager alerted the user on every flip back.
func guestPoller(t *testing.T, st *store.Store) *poller {
	t.Helper()
	driver, err := tmux.NewWithSocket("amguest" + strconv.FormatInt(time.Now().UnixNano(), 36))
	if err != nil {
		t.Fatalf("tmux: %v", err)
	}
	return &poller{store: st, tmux: driver, hooks: hooks.NewManager(t.TempDir()), interval: time.Second}
}

func TestGuestManagerLeavesAnotherServersSessionsAlone(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "watched", t.TempDir(), "")
	sess := m.sessionRows()[0]
	m.poller.refreshOnce()
	before, err := m.store.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}

	guest := guestPoller(t, m.store)
	if msg, failed := guest.refreshOnce().(errMsg); failed {
		t.Fatalf("the guest poll has to run for this to prove anything: %v", msg.err)
	}

	after, err := m.store.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != before.Status {
		t.Fatalf("guest manager rewrote status %q as %q", before.Status, after.Status)
	}
	if after.TmuxSocket != before.TmuxSocket {
		t.Fatalf("guest manager claimed the session: %q, want %q", after.TmuxSocket, before.TmuxSocket)
	}

	// The spam came from the owner reading the guest's dead stamp as a
	// transition on the next poll.
	rec := &notifyRecorder{}
	m.poller.notifyFn = rec.fn()
	m.poller.refreshOnce()
	settle()
	if calls := rec.all(); len(calls) != 0 {
		t.Fatalf("a poll beside a second manager should not alert, got %v", calls)
	}
}

func TestManagerStillMarksItsOwnSessionsDead(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "doomed-own", t.TempDir(), "")
	sess := m.sessionRows()[0]
	m.poller.refreshOnce()
	if err := m.tmux.Kill(sess.ID); err != nil {
		t.Fatal(err)
	}
	m.poller.refreshOnce()

	got, err := m.store.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != status.Dead {
		t.Fatalf("status = %q, want dead once the pane on this server is gone", got.Status)
	}
}

func TestManagerClaimsUnstampedSessionsOnItsServer(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "unstamped", t.TempDir(), "")
	sess := m.sessionRows()[0]
	if err := m.store.SetTmuxSocket(sess.ID, ""); err != nil {
		t.Fatal(err)
	}
	m.poller.refreshOnce()

	got, err := m.store.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.TmuxSocket != m.tmux.SocketPath() {
		t.Fatalf("socket = %q, want this manager's %q", got.TmuxSocket, m.tmux.SocketPath())
	}
}

// Rows that predate the column have no server to compare against, so they
// belong to whichever manager holds the heartbeat.
func TestUnstampedSessionsFollowTheLeadingManager(t *testing.T) {
	m := buildModel(t)
	sess := store.Session{ID: "legacy-1", Name: "legacy", Tool: "claude", Cwd: t.TempDir(), Status: status.Working}
	if err := m.store.CreateSession(sess); err != nil {
		t.Fatal(err)
	}
	if err := m.store.SetTmuxSocket(sess.ID, ""); err != nil {
		t.Fatal(err)
	}
	if err := m.store.SetSetting(store.PollerSocketKey, "/tmp/another-manager/agentmgr"); err != nil {
		t.Fatal(err)
	}
	stampHeartbeat(t, m.store, time.Now())
	m.poller.heartbeatAt = time.Time{}
	m.poller.refreshOnce()

	got, err := m.store.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != status.Working {
		t.Fatalf("status = %q, want the leading manager's %q left alone", got.Status, status.Working)
	}

	// The claim runs on the heartbeat's cadence, so age this manager's own
	// stamp as well to reach the poll that takes the store over.
	stampHeartbeat(t, m.store, time.Now().Add(-2*store.PollerHeartbeatStale))
	m.poller.heartbeatAt = time.Time{}
	m.poller.refreshOnce()

	got, err = m.store.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != status.Dead {
		t.Fatalf("status = %q, want dead once no other manager is home", got.Status)
	}
}

func stampHeartbeat(t *testing.T, st *store.Store, at time.Time) {
	t.Helper()
	if err := st.SetSetting(store.PollerHeartbeatKey, strconv.FormatInt(at.UnixNano(), 10)); err != nil {
		t.Fatal(err)
	}
}

// Claiming a row is also the moment to write its status: until the claim
// the row was anyone's, so a manager that cannot see this pane may have
// stamped it since this pass read the list. The write goes out even when
// the derived status matches what this pass listed.
func TestClaimingASessionWritesItsStatus(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "claimed-back", t.TempDir(), "")
	sess := m.sessionRows()[0]
	m.poller.refreshOnce()

	// Back to how a row that predates the column looks, with its pane still
	// running here.
	if err := m.store.SetTmuxSocket(sess.ID, ""); err != nil {
		t.Fatal(err)
	}
	before, err := m.store.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}

	m.poller.refreshOnce()

	after, err := m.store.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.TmuxSocket != m.tmux.SocketPath() {
		t.Fatalf("socket = %q, want the claim on %q", after.TmuxSocket, m.tmux.SocketPath())
	}
	if !after.LastStatusAt.After(before.LastStatusAt) {
		t.Fatalf("status was not rewritten on the claiming pass: %s then %s",
			before.LastStatusAt, after.LastStatusAt)
	}
	if after.Status == status.Dead {
		t.Fatal("a session whose pane runs here must not be left dead")
	}
}

func TestLastMeaningfulPaneLineSkipsChrome(t *testing.T) {
	pane := "❯ Add a limiter\n\n\x1b[38;5;240m● Running tests\x1b[0m\n╰────────╯\n   ✶ \n\n"
	if got := lastMeaningfulPaneLine(pane); got != "● Running tests" {
		t.Fatalf("last meaningful line = %q", got)
	}
	if got := lastMeaningfulPaneLine("\n╭──╮\n│  │\n╰──╯\n"); got != "" {
		t.Fatalf("a pane of borders should yield nothing, got %q", got)
	}
}
