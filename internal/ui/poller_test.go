package ui

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
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
	nameFile := m.hooks.NameFile(sess.ID)
	if err := os.MkdirAll(filepath.Dir(nameFile), 0o755); err != nil {
		t.Fatalf("hooks dir: %v", err)
	}
	if err := os.WriteFile(nameFile, []byte("renamed"), 0o644); err != nil {
		t.Fatalf("write name file: %v", err)
	}

	if err := m.poller.applyPendingRename(&sess); err != nil {
		t.Fatalf("rename of a deleted session should not fail the pass: %v", err)
	}
	if _, found := m.hooks.ReadName(sess.ID); found {
		t.Fatal("the name file should be consumed instead of retried every poll")
	}
}

func TestPendingRenameMovesTheWorktree(t *testing.T) {
	m := buildModel(t)
	repo := seedRepo(t)
	spawned := createWorktreeSession(t, m, "claude-7a72", repo)
	writeName(t, m, spawned.ID, "audit the poller")

	sess := spawned
	if err := m.poller.applyPendingRename(&sess); err != nil {
		t.Fatalf("rename: %v", err)
	}

	wantDir := filepath.Join(filepath.Dir(spawned.Cwd), "audit-the-poller")
	if sess.Cwd != wantDir || sess.WorktreeBranch != "am/audit-the-poller" {
		t.Fatalf("session did not follow the name: %+v", sess)
	}
	if sess.Name != "audit the poller" {
		t.Fatalf("name = %q", sess.Name)
	}
	stored, err := m.store.Get(spawned.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.Cwd != wantDir || stored.WorktreeBranch != "am/audit-the-poller" || stored.Name != "audit the poller" {
		t.Fatalf("store did not follow the name: %+v", stored)
	}
	if _, err := os.Stat(wantDir); err != nil {
		t.Fatalf("worktree directory did not follow: %v", err)
	}
	if _, found := m.hooks.ReadName(spawned.ID); found {
		t.Fatal("the name file should be consumed")
	}
}

func TestPendingRenameOnATakenWorktreeNameStopsAfterOneReport(t *testing.T) {
	m := buildModel(t)
	repo := seedRepo(t)
	spawned := createWorktreeSession(t, m, "mover", repo)
	createWorktreeSession(t, m, "taken", repo)
	writeName(t, m, spawned.ID, "taken")

	sess := spawned
	if err := m.poller.applyPendingRename(&sess); err == nil {
		t.Fatal("a taken worktree name should report why")
	}
	if _, found := m.hooks.ReadName(spawned.ID); found {
		t.Fatal("the name file must be consumed so later polls are not stuck on it")
	}
	stored, err := m.store.Get(spawned.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.Name != "mover" || stored.Cwd != spawned.Cwd || stored.WorktreeBranch != spawned.WorktreeBranch {
		t.Fatalf("refused rename still moved something: %+v", stored)
	}

	// The next poll runs clean, so one bad name does not stall the loop.
	if err := m.poller.applyPendingRename(&sess); err != nil {
		t.Fatalf("second pass: %v", err)
	}
}

func writeName(t *testing.T, m *Model, id, name string) {
	t.Helper()
	path := m.hooks.NameFile(id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("hooks dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(name), 0o644); err != nil {
		t.Fatalf("write name file: %v", err)
	}
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
	cfg, err := config.Default()
	if err != nil {
		t.Fatalf("default config: %v", err)
	}
	m.poller.engine, err = status.NewEngine(cfg)
	if err != nil {
		t.Fatalf("status engine: %v", err)
	}
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

func writePendingName(t *testing.T, m *Model, id, name string) {
	t.Helper()
	path := m.hooks.NameFile(id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir hooks dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(name), 0o644); err != nil {
		t.Fatalf("write name: %v", err)
	}
}

func TestRefreshAppliesPendingRename(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "placeholder", t.TempDir(), "")
	sess := m.sessionRows()[0]

	writePendingName(t, m, sess.ID, "fix auth bug\n")
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

	writePendingName(t, m, sess.ID, "   \n")
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
	// inside the clock slack the capture window allows.
	writeCodexRollout(t, filepath.Join(codexHome, "sessions", "rollout-old.jsonl"), "old-id", cwd, restarted.Add(-2*time.Second))
	writeCodexRollout(t, filepath.Join(codexHome, "sessions", "rollout-new.jsonl"), "new-id", cwd, restarted.Add(time.Second))

	if err := st.CreateSession(store.Session{ID: "sess", Name: "s", Tool: "codex", Cwd: cwd, Group: "g", Status: "idle", CreatedAt: created, AgentSessionID: "old-id"}); err != nil {
		t.Fatal(err)
	}
	if err := st.RestartAgent("sess", "", restarted); err != nil {
		t.Fatal(err)
	}
	sess, err := st.Get("sess")
	if err != nil {
		t.Fatal(err)
	}

	p := &poller{store: st, sessionStores: map[string]string{"codex": "codex"}}
	if _, err := p.captureAgentSessionIDs([]store.Session{sess}, map[string]int{"sess": 42}); err != nil {
		t.Fatal(err)
	}
	got, err := st.Get("sess")
	if err != nil {
		t.Fatal(err)
	}
	if got.AgentSessionID != "new-id" {
		t.Fatalf("captured %q, want new-id", got.AgentSessionID)
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
	captured, err := p.captureAgentSessionIDs([]store.Session{snapshot}, map[string]int{"sess": 7})
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
