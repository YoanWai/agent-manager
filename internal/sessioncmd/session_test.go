package sessioncmd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/google/uuid"
)

type sessionHarness struct {
	driver    *tmux.Driver
	store     *store.Store
	sessions  *Sessions
	terminals *Terminals
	caller    store.Session
}

// sessionConfig gives the harness one agent CLI whose command echoes the
// prompt it launched with, so a spawn's own pane proves the prompt reached
// it, plus a shell block the agent tools must refuse.
const sessionConfig = `[tools.claude]
command = "echo"
revive_command = "echo resumed"
default_status = "idle"
activity_cutoff = "(?m)^\u276f"

[tools.flagged]
command = "echo"
prompt_flag = "-n"
default_status = "idle"
activity_cutoff = "(?m)^\u276f"

[tools.blind]
command = "echo"
default_status = "idle"

[tools.terminal]
command = ""
shell = true
default_status = "idle"
`

func newSessionHarness(t *testing.T) *sessionHarness {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	configDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(sessionConfig), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	driver, err := tmux.NewWithSocket("amsesstest-" + uuid.NewString()[:8])
	if err != nil {
		t.Fatalf("tmux driver: %v", err)
	}
	st, err := store.Open(filepath.Join(configDir, "state.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	callerDir := t.TempDir()
	caller := store.Session{
		ID:     uuid.NewString()[:8],
		Name:   "calling-agent",
		Tool:   "claude",
		Cwd:    callerDir,
		Group:  "backend",
		Status: status.Idle,
	}
	if err := st.CreateGroup("backend", callerDir); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := driver.Create(caller.ID, caller.Cwd, "", nil, 80, 24); err != nil {
		t.Fatalf("create caller pane: %v", err)
	}
	if err := st.CreateSession(caller); err != nil {
		_ = driver.Kill(caller.ID)
		t.Fatalf("create caller row: %v", err)
	}
	newDriver := func() (*tmux.Driver, error) { return driver, nil }
	h := &sessionHarness{
		driver:    driver,
		store:     st,
		caller:    caller,
		sessions:  newSessions(configDir, MCPVocabulary(), newDriver, git.New),
		terminals: newTerminals(configDir, MCPVocabulary(), newDriver),
	}
	t.Cleanup(func() {
		sessions, _ := st.ListSessions(true)
		for _, sess := range sessions {
			_ = driver.Kill(sess.ID)
		}
		if out, err := exec.Command("tmux", "-L", driver.SocketName(), "kill-server").CombinedOutput(); err != nil && !strings.Contains(string(out), "no server running") {
			t.Errorf("kill test tmux server: %v: %s", err, strings.TrimSpace(string(out)))
		}
		_ = st.Close()
	})
	return h
}

func waitForSessionOutput(t *testing.T, sessions *Sessions, callerID, targetID, marker string) SessionScreen {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		screen, err := sessions.Read(callerID, targetID)
		if err == nil && strings.Contains(screen.Output, marker) {
			return screen
		}
		time.Sleep(25 * time.Millisecond)
	}
	screen, err := sessions.Read(callerID, targetID)
	t.Fatalf("session never showed %q: output=%q err=%v", marker, screen.Output, err)
	return SessionScreen{}
}

func TestSessionsCreateCarriesNamePromptAndTargetWithRealTmux(t *testing.T) {
	h := newSessionHarness(t)
	created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{
		Name:   "payments-retry-fix",
		Prompt: "fix the retry backoff",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Name != "payments-retry-fix" || created.Tool != "claude" || !created.Running {
		t.Fatalf("created identity = %+v", created)
	}
	if created.Group != h.caller.Group || !sameTerminalPath(created.Directory, h.caller.Cwd) {
		t.Fatalf("created target = %+v, caller = %+v", created, h.caller)
	}
	stored, err := h.store.Get(created.ID)
	if err != nil {
		t.Fatalf("stored session: %v", err)
	}
	if stored.Name != created.Name || stored.Tool != "claude" || stored.Status != status.Starting {
		t.Fatalf("stored row = %+v", stored)
	}
	// echo prints what the launch command handed it, so the pane proves the
	// prompt rode the command line rather than being dropped.
	waitForSessionOutput(t, h.sessions, h.caller.ID, created.ID, "fix the retry backoff")
}

func TestSessionsCreateAutoNamesAndAsksForARename(t *testing.T) {
	h := newSessionHarness(t)
	created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Prompt: "build the api"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Name != "claude-"+created.ID[:4] {
		t.Fatalf("auto-named session = %q", created.Name)
	}
	waitForSessionOutput(t, h.sessions, h.caller.ID, created.ID, "build the api")
}

func TestSessionsCreateRejectsShellsUnknownToolsAndFlagPrompts(t *testing.T) {
	h := newSessionHarness(t)
	if _, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Tool: "terminal"}); err == nil ||
		!strings.Contains(err.Error(), "create_terminal") {
		t.Fatalf("shell tool error = %v", err)
	}
	if _, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Tool: "nope"}); err == nil ||
		!strings.Contains(err.Error(), "not configured") {
		t.Fatalf("unknown tool error = %v", err)
	}
	if _, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Prompt: "--help"}); err == nil ||
		!strings.Contains(err.Error(), "read it as a flag") {
		t.Fatalf("flag-like prompt error = %v", err)
	}
	// A tool that takes its prompt behind a flag can carry one safely, and a
	// bullet list is an ordinary way to write a task.
	if _, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Tool: "flagged", Prompt: "- do the thing"}); err != nil {
		t.Fatalf("a flagged tool should accept a prompt starting with a dash: %v", err)
	}
	missing := "missing-group"
	if _, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Group: &missing}); err == nil ||
		!strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("unknown group error = %v", err)
	}
}

func TestSessionsListCoversAgentsOnlyAndMarksTheCaller(t *testing.T) {
	h := newSessionHarness(t)
	if _, err := h.terminals.Create(h.caller.ID, CreateTerminalOptions{}); err != nil {
		t.Fatalf("create terminal: %v", err)
	}
	created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "worker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	listed, err := h.sessions.List(h.caller.ID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("list should hold the caller and the new agent only, got %+v", listed)
	}
	seen := map[string]Session{}
	for _, sess := range listed {
		seen[sess.ID] = sess
	}
	if !seen[h.caller.ID].Self || seen[created.ID].Self {
		t.Fatalf("self marking = %+v", listed)
	}
	if !seen[created.ID].Running || seen[created.ID].Name != "worker" {
		t.Fatalf("listed spawn = %+v", seen[created.ID])
	}
}

func TestSessionsSendAndReadReachTheTargetPane(t *testing.T) {
	h := newSessionHarness(t)
	created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "worker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	sent, err := h.sessions.Send(h.caller.ID, created.ID, "rebase on main")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if sent.MessageID == 0 || sent.QueuePosition != 1 {
		t.Fatalf("send result = %+v", sent)
	}
	// The message is queued, not typed: nothing reaches the pane until a
	// running manager decides the target is at rest.
	screen, err := h.sessions.Read(h.caller.ID, created.ID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if strings.Contains(screen.Output, "rebase on main") {
		t.Fatalf("send must not type into the pane itself, got %q", screen.Output)
	}
	state, err := h.sessions.MessageStatus(h.caller.ID, sent.MessageID)
	if err != nil {
		t.Fatalf("MessageStatus: %v", err)
	}
	if state.State != "queued" {
		t.Fatalf("message state = %+v", state)
	}
	if _, err := h.sessions.Send(h.caller.ID, created.ID, "rebase on main"); err == nil {
		t.Fatal("an identical message should be refused as a duplicate")
	}
	if _, err := h.sessions.Send(h.caller.ID, h.caller.ID, "talking to myself"); err == nil {
		t.Fatal("a session should not message itself")
	}

	if _, err := h.sessions.Send(h.caller.ID, created.ID, "   "); err == nil {
		t.Fatal("an empty message should be refused")
	}
	terminal, err := h.terminals.Create(h.caller.ID, CreateTerminalOptions{})
	if err != nil {
		t.Fatalf("create terminal: %v", err)
	}
	if _, err := h.sessions.Send(h.caller.ID, terminal.ID, "ls"); err == nil ||
		!strings.Contains(err.Error(), "terminal, not an agent") {
		t.Fatalf("sending to a terminal error = %v", err)
	}
}

func TestSessionsKillKeepsTheScreenAndReviveBringsItBack(t *testing.T) {
	h := newSessionHarness(t)
	created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "worker", Prompt: "hold the line"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	waitForSessionOutput(t, h.sessions, h.caller.ID, created.ID, "hold the line")

	killed, err := h.sessions.Kill(h.caller.ID, created.ID)
	if err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if killed.Running || killed.Status != status.Dead {
		t.Fatalf("killed session = %+v", killed)
	}
	if h.driver.Exists(created.ID) {
		t.Fatal("killed session still has a pane")
	}
	screen, err := h.sessions.Read(h.caller.ID, created.ID)
	if err != nil {
		t.Fatalf("Read after kill: %v", err)
	}
	if !strings.Contains(screen.Output, "hold the line") {
		t.Fatalf("a killed session should keep its last screen, got %q", screen.Output)
	}

	revived, err := h.sessions.Revive(h.caller.ID, created.ID)
	if err != nil {
		t.Fatalf("Revive: %v", err)
	}
	if !revived.Running || !h.driver.Exists(created.ID) {
		t.Fatalf("revived session = %+v", revived)
	}
	if _, err := h.sessions.Revive(h.caller.ID, created.ID); err == nil ||
		!strings.Contains(err.Error(), "still running") {
		t.Fatalf("reviving a live session error = %v", err)
	}
	if _, err := h.sessions.Kill(h.caller.ID, h.caller.ID); err == nil {
		t.Fatal("a session must not kill itself")
	}
}

func TestSessionsArchiveHidesAndRestores(t *testing.T) {
	h := newSessionHarness(t)
	created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "worker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	archived, err := h.sessions.Archive(h.caller.ID, created.ID, true)
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if !archived.Archived || !archived.Running {
		t.Fatalf("archiving must not stop the pane: %+v", archived)
	}
	stored, err := h.store.Get(created.ID)
	if err != nil || !stored.Archived {
		t.Fatalf("stored archived = %+v err=%v", stored, err)
	}
	restored, err := h.sessions.Archive(h.caller.ID, created.ID, false)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if restored.Archived {
		t.Fatalf("restored session = %+v", restored)
	}
	if _, err := h.sessions.Archive(h.caller.ID, h.caller.ID, true); err == nil {
		t.Fatal("a session must not archive itself")
	}
}

func TestSessionGroupsListAndCreate(t *testing.T) {
	h := newSessionHarness(t)
	created, err := h.sessions.CreateGroup(h.caller.ID, "backend/payments", h.caller.Cwd)
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if created.Path != "backend/payments" || !sameTerminalPath(created.Directory, h.caller.Cwd) {
		t.Fatalf("created group = %+v", created)
	}
	if _, err := h.sessions.CreateGroup(h.caller.ID, "backend/payments", ""); err == nil ||
		!strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate group error = %v", err)
	}
	if _, err := h.sessions.CreateGroup(h.caller.ID, "unknown/child", ""); err == nil ||
		!strings.Contains(err.Error(), "parent group") {
		t.Fatalf("orphan group error = %v", err)
	}
	if _, err := h.sessions.CreateGroup(h.caller.ID, "  ", ""); err == nil {
		t.Fatal("an empty group path should be refused")
	}

	spawned, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "worker", Group: &created.Path})
	if err != nil {
		t.Fatalf("Create in new group: %v", err)
	}
	if spawned.Group != "backend/payments" {
		t.Fatalf("spawn group = %q", spawned.Group)
	}
	groups, err := h.sessions.Groups(h.caller.ID)
	if err != nil {
		t.Fatalf("Groups: %v", err)
	}
	counts := map[string]int{}
	for _, group := range groups {
		counts[group.Path] = group.Sessions
	}
	if counts["backend"] != 1 || counts["backend/payments"] != 1 {
		t.Fatalf("group counts = %+v", counts)
	}
}

func TestSessionsCreateOpensItsOwnWorktreeWhenAsked(t *testing.T) {
	h := newSessionHarness(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "t@t"},
		{"config", "user.name", "t"},
		{"commit", "--allow-empty", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git %v: %v: %s", args, err, out)
		}
	}
	wanted := true
	created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{
		Name:      "worktree-worker",
		Directory: repo,
		Worktree:  &wanted,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Branch == "" {
		t.Fatalf("worktree session = %+v, want a branch", created)
	}
	if sameTerminalPath(created.Directory, repo) {
		t.Fatalf("worktree session should work outside the main checkout, got %q", created.Directory)
	}
	stored, err := h.store.Get(created.ID)
	if err != nil {
		t.Fatalf("stored session: %v", err)
	}
	if stored.WorktreeRepo == "" || stored.WorktreeBranch != created.Branch {
		t.Fatalf("stored worktree = %+v", stored)
	}
}

func TestSendAndWaitRefuseATargetTheManagerNoLongerPolls(t *testing.T) {
	h := newSessionHarness(t)
	created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "worker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := h.sessions.Archive(h.caller.ID, created.ID, true); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	// The pane is still alive, so nothing else would catch this: an archived
	// row is skipped by the poller, and the message would queue forever.
	if _, err := h.sessions.Send(h.caller.ID, created.ID, "rebase on main"); err == nil ||
		!strings.Contains(err.Error(), "archived") {
		t.Fatalf("send to an archived session = %v", err)
	}
	if _, err := h.sessions.Wait(context.Background(), h.caller.ID, created.ID, nil, time.Second); err == nil ||
		!strings.Contains(err.Error(), "archived") {
		t.Fatalf("wait on an archived session = %v", err)
	}
}

func TestSendRefusesAToolTheManagerCannotReadReadinessFrom(t *testing.T) {
	h := newSessionHarness(t)
	created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Tool: "blind", Name: "worker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := h.sessions.Send(h.caller.ID, created.ID, "rebase on main"); err == nil ||
		!strings.Contains(err.Error(), "activity_cutoff") {
		t.Fatalf("send to a tool with no readiness marker = %v", err)
	}
}

// Whether a manager is awake is read off a heartbeat only the poller
// writes. A value that is not a timestamp means something else wrote that
// row, and reporting it as "no manager" would send the caller after the
// wrong problem.
func TestAnUnreadableHeartbeatIsReportedRatherThanReadAsAClosedManager(t *testing.T) {
	h := newSessionHarness(t)
	worker, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "worker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := h.store.SetSetting(store.PollerHeartbeatKey, "just now"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if _, err := h.sessions.Send(h.caller.ID, worker.ID, "rebase on main"); err == nil ||
		!strings.Contains(err.Error(), "poller heartbeat") {
		t.Fatalf("Send with a corrupt heartbeat = %v", err)
	}
}

// A sender follows its own message instead of reading the recipient's
// screen, and the recipient answering is the acknowledgement. Both
// transitions belong to this front; the store tests cover the rows they
// write, and nothing follows one message across the two.
func TestASenderSeesItsMessageDeliveredThenAnswered(t *testing.T) {
	h := newSessionHarness(t)
	worker, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "worker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	sent, err := h.sessions.Send(h.caller.ID, worker.ID, "rebase on main")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	// The queued row is the whole contract with the manager: it names the
	// sender in the envelope it types and claims the row before typing.
	head, queued, err := h.store.HeadMessage(worker.ID)
	if err != nil || !queued {
		t.Fatalf("HeadMessage: %v, queued=%v", err, queued)
	}
	if head.ID != sent.MessageID || head.SenderID != h.caller.ID || head.SenderName != h.caller.Name ||
		head.Body != "rebase on main" || !head.ClaimedAt.IsZero() {
		t.Fatalf("queued row = %+v, caller = %+v", head, h.caller)
	}

	// The manager's poller owns delivery; these are the two writes it makes
	// once it finds the target at rest.
	claimed, err := h.store.ClaimMessage(sent.MessageID, time.Now())
	if err != nil || !claimed {
		t.Fatalf("ClaimMessage: %v, claimed=%v", err, claimed)
	}
	if err := h.store.MarkDelivered(sent.MessageID, time.Now()); err != nil {
		t.Fatalf("MarkDelivered: %v", err)
	}

	state, err := h.sessions.MessageStatus(h.caller.ID, sent.MessageID)
	if err != nil {
		t.Fatalf("MessageStatus: %v", err)
	}
	if state.State != "delivered" || state.DeliveredAt == "" || state.SessionID != worker.ID {
		t.Fatalf("delivered state = %+v", state)
	}

	if _, err := h.sessions.Send(worker.ID, h.caller.ID, "rebased, tests pass"); err != nil {
		t.Fatalf("reply: %v", err)
	}
	state, err = h.sessions.MessageStatus(h.caller.ID, sent.MessageID)
	if err != nil {
		t.Fatalf("MessageStatus after the reply: %v", err)
	}
	if state.State != "answered" {
		t.Fatalf("a reply did not acknowledge the message it answers: %+v", state)
	}
	// Only the sender may follow it; another session asking is told so
	// rather than shown someone else's traffic.
	if _, err := h.sessions.MessageStatus(worker.ID, sent.MessageID); err == nil ||
		!strings.Contains(err.Error(), "was not sent by this session") {
		t.Fatalf("reading another session's message = %v", err)
	}
}

// A message the manager could not type is retired so nothing retypes it,
// which leaves it looking exactly like a delivered one in the queue. The
// sender has no other way to find out it never landed.
func TestASenderIsToldWhenItsMessageWasDropped(t *testing.T) {
	h := newSessionHarness(t)
	worker, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "worker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	sent, err := h.sessions.Send(h.caller.ID, worker.ID, "rebase on main")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if err := h.store.MarkDropped(sent.MessageID, time.Now()); err != nil {
		t.Fatalf("MarkDropped: %v", err)
	}

	state, err := h.sessions.MessageStatus(h.caller.ID, sent.MessageID)
	if err != nil {
		t.Fatalf("MessageStatus: %v", err)
	}
	if state.State != "dropped" || state.DeliveredAt != "" {
		t.Fatalf("dropped message reported as %+v", state)
	}
	if !strings.Contains(state.Reason, "send it again") {
		t.Fatalf("a dropped message does not say what to do about it: %+v", state)
	}
	// A reply must not turn a message that never arrived into an answered one.
	if _, err := h.sessions.Send(worker.ID, h.caller.ID, "rebased, tests pass"); err != nil {
		t.Fatalf("reply: %v", err)
	}
	state, err = h.sessions.MessageStatus(h.caller.ID, sent.MessageID)
	if err != nil {
		t.Fatalf("MessageStatus after the reply: %v", err)
	}
	if state.State != "dropped" {
		t.Fatalf("a dropped message was acknowledged by an unrelated reply: %+v", state)
	}
}

// The delivery gate refuses a pane the tool's own rules call waiting, which
// covers an ordinary question as well as an approval dialog. Nothing types
// into that session until it moves, so a sender told "queued" would sit on
// a handoff that is going nowhere.
func TestASenderSeesAMessageHeldByASessionOnAQuestion(t *testing.T) {
	h := newSessionHarness(t)
	worker, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "worker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	sent, err := h.sessions.Send(h.caller.ID, worker.ID, "rebase on main")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if err := h.store.UpdateStatus(worker.ID, status.Waiting); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	state, err := h.sessions.MessageStatus(h.caller.ID, sent.MessageID)
	if err != nil {
		t.Fatalf("MessageStatus: %v", err)
	}
	if state.State != "held" {
		t.Fatalf("a message behind a question reads as %+v", state)
	}
	if !strings.Contains(state.Reason, worker.ID) || !strings.Contains(state.Reason, "question") {
		t.Fatalf("the hold does not say why: %+v", state)
	}
	// The hold is the recipient's state, not the message's: once it moves on,
	// the same message is an ordinary queued one again.
	if err := h.store.UpdateStatus(worker.ID, status.Working); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	state, err = h.sessions.MessageStatus(h.caller.ID, sent.MessageID)
	if err != nil {
		t.Fatalf("MessageStatus after the session moved: %v", err)
	}
	if state.State != "queued" || state.Reason != "" {
		t.Fatalf("a working recipient still holds its queue: %+v", state)
	}
}
