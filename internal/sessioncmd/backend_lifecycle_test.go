package sessioncmd

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
)

func harnessRuntime(t *testing.T, h *sessionHarness) Runtime {
	t.Helper()
	cfg, err := h.sessions.loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	return Runtime{
		Config:   cfg,
		Store:    h.store,
		Driver:   h.driver,
		Hooks:    hooks.NewManager(h.sessions.configDir),
		Snapshot: h.store.SetSnapshot,
	}
}

func TestBorrowBackendKeepsItsStoreOpenAndFailsClosed(t *testing.T) {
	h := newSessionHarness(t)
	backend, err := BorrowBackend(harnessRuntime(t, h))
	if err != nil {
		t.Fatal(err)
	}
	cli := NewSessionsWithBackend(backend, CLIVocabulary())
	mcp := NewSessionsWithBackend(backend, MCPVocabulary())

	for name, tc := range map[string]struct {
		sessions *Sessions
		want     string
	}{
		"cli": {sessions: cli, want: CLIVocabulary().ListSessions},
		"mcp": {sessions: mcp, want: MCPVocabulary().ListSessions},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := tc.sessions.Read(h.caller.ID, ""); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("target error = %v, want vocabulary %q", err, tc.want)
			}
		})
	}
	if _, err := cli.List(h.caller.ID); err != nil {
		t.Fatalf("first command: %v", err)
	}
	if _, err := mcp.List(h.caller.ID); err != nil {
		t.Fatalf("second command reused a closed store: %v", err)
	}
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.ListSessions(true); err != nil {
		t.Fatalf("closing a borrowed backend closed its store: %v", err)
	}
	if _, err := cli.List(h.caller.ID); err == nil || !strings.Contains(err.Error(), "backend is closed") {
		t.Fatalf("command after close = %v", err)
	}
}

func TestLifecycleLaunchRollsBackPaneAndHookWhenStoreRejectsRow(t *testing.T) {
	h := newSessionHarness(t)
	runtime := harnessRuntime(t, h)
	lifecycle, err := NewLifecycle(runtime)
	if err != nil {
		t.Fatal(err)
	}
	sess := store.Session{ID: "duplicate", Name: "duplicate", Tool: "echoer", Cwd: t.TempDir(), Status: status.Starting}
	if err := h.store.CreateSession(sess); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(runtime.Hooks.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runtime.Hooks.StatusFile(sess.ID), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.Launch(LaunchRequest{
		Session: sess, Tool: runtime.Config.Tools[sess.Tool], BaseCommand: "sleep 30", Pane: PaneSize{Width: 80, Height: 24},
	}); err == nil {
		t.Fatal("duplicate row launch succeeded")
	}
	if h.driver.Exists(sess.ID) {
		t.Fatal("failed launch left its pane running")
	}
	if _, err := os.Stat(runtime.Hooks.StatusFile(sess.ID)); !os.IsNotExist(err) {
		t.Fatalf("failed launch left its hook file: %v", err)
	}
}

func TestLifecycleLaunchReturnsLabelFailureAfterPersisting(t *testing.T) {
	h := newSessionHarness(t)
	runtime := harnessRuntime(t, h)
	lifecycle, err := NewLifecycle(runtime)
	if err != nil {
		t.Fatal(err)
	}
	want := errors.New("label unavailable")
	lifecycle.setLabel = func(string, string) error { return want }
	sess := store.Session{ID: "labelerr", Name: "label-error", Tool: "echoer", Cwd: t.TempDir(), Status: status.Starting}
	result, err := lifecycle.Launch(LaunchRequest{
		Session: sess, Tool: runtime.Config.Tools[sess.Tool], BaseCommand: "sleep 30", Pane: PaneSize{Width: 80, Height: 24},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(result.LabelError, want) {
		t.Fatalf("label error = %v, want %v", result.LabelError, want)
	}
	if _, err := h.store.Get(sess.ID); err != nil || !h.driver.Exists(sess.ID) {
		t.Fatalf("label failure rolled back a successful launch: row err=%v pane=%v", err, h.driver.Exists(sess.ID))
	}
}

func TestLifecycleSnapshotsEveryPaneBeforeMultiArchiveKillsOne(t *testing.T) {
	h := newSessionHarness(t)
	first, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "first", Prompt: "FIRST-SNAPSHOT"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "second", Prompt: "SECOND-SNAPSHOT"})
	if err != nil {
		t.Fatal(err)
	}
	waitForSessionOutput(t, h.sessions, h.caller.ID, first.ID, "FIRST-SNAPSHOT")
	waitForSessionOutput(t, h.sessions, h.caller.ID, second.ID, "SECOND-SNAPSHOT")
	firstRow, _ := h.store.Get(first.ID)
	secondRow, _ := h.store.Get(second.ID)
	runtime := harnessRuntime(t, h)
	var initialSnapshots int
	runtime.Snapshot = func(id, pane string) error {
		if initialSnapshots < 2 && (!h.driver.Exists(first.ID) || !h.driver.Exists(second.ID)) {
			t.Fatalf("snapshot %d ran after a pane was killed", initialSnapshots+1)
		}
		initialSnapshots++
		return h.store.SetSnapshot(id, pane)
	}
	lifecycle, err := NewLifecycle(runtime)
	if err != nil {
		t.Fatal(err)
	}
	result, err := lifecycle.ArchiveForHuman(ArchiveSelection{Sessions: []store.Session{firstRow, secondRow}})
	if err != nil {
		t.Fatal(err)
	}
	if initialSnapshots < 2 || !result.Archived {
		t.Fatalf("archive result = %+v, snapshots = %d", result, initialSnapshots)
	}
	if h.driver.Exists(first.ID) || h.driver.Exists(second.ID) {
		t.Fatal("human archive left a selected pane running")
	}
}

func TestLifecycleArchivePartialResultKeepsKilledSessionUnarchived(t *testing.T) {
	h := newSessionHarness(t)
	first, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "first"})
	if err != nil {
		t.Fatal(err)
	}
	firstRow, _ := h.store.Get(first.ID)
	missing := store.Session{ID: "missing1", Name: "missing", Tool: "echoer", Cwd: t.TempDir()}
	if err := h.driver.Create(missing.ID, missing.Cwd, "sleep 30", nil, 80, 24); err != nil {
		t.Fatal(err)
	}
	defer h.driver.Kill(missing.ID)
	runtime := harnessRuntime(t, h)
	runtime.Snapshot = func(id, pane string) error {
		if id == missing.ID {
			return nil
		}
		return h.store.SetSnapshot(id, pane)
	}
	lifecycle, err := NewLifecycle(runtime)
	if err != nil {
		t.Fatal(err)
	}

	result, err := lifecycle.ArchiveForHuman(ArchiveSelection{Sessions: []store.Session{firstRow, missing}})
	if err == nil {
		t.Fatal("archive unexpectedly succeeded with a missing row")
	}
	if len(result.Sessions) != 1 || result.Sessions[0].ID != first.ID {
		t.Fatalf("partial archive = %+v", result)
	}
	if result.Sessions[0].Archived {
		t.Fatal("killed session was reported archived before its archive write")
	}
	stored, getErr := h.store.Get(first.ID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if stored.Archived || stored.Status != status.Dead {
		t.Fatalf("durable partial archive = %+v", stored)
	}
}

func TestLifecycleRestoreReportsCompletedSessionsBeforeFailure(t *testing.T) {
	h := newSessionHarness(t)
	first, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "first"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "second"})
	if err != nil {
		t.Fatal(err)
	}
	runtime := harnessRuntime(t, h)
	lifecycle, err := NewLifecycle(runtime)
	if err != nil {
		t.Fatal(err)
	}
	firstRow, _ := h.store.Get(first.ID)
	secondRow, _ := h.store.Get(second.ID)
	if _, err := lifecycle.ArchiveForHuman(ArchiveSelection{Sessions: []store.Session{firstRow, secondRow}}); err != nil {
		t.Fatal(err)
	}
	firstRow, _ = h.store.Get(first.ID)
	secondRow, _ = h.store.Get(second.ID)
	secondRow.Cwd = filepath.Join(t.TempDir(), "missing")
	result, err := lifecycle.RestoreForHuman(ArchiveSelection{Sessions: []store.Session{firstRow, secondRow}}, PaneSize{Width: 80, Height: 24})
	if err == nil || !strings.Contains(err.Error(), "working directory no longer exists") {
		t.Fatalf("restore failure = %v", err)
	}
	if len(result.Sessions) != 1 || result.Sessions[0].ID != first.ID {
		t.Fatalf("partial restore = %+v", result)
	}
	firstStored, _ := h.store.Get(first.ID)
	secondStored, _ := h.store.Get(second.ID)
	if firstStored.Archived || !h.driver.Exists(first.ID) || !secondStored.Archived || h.driver.Exists(second.ID) {
		t.Fatalf("partial state: first=%+v firstPane=%v second=%+v secondPane=%v", firstStored, h.driver.Exists(first.ID), secondStored, h.driver.Exists(second.ID))
	}
}

func TestLifecycleRestoreReturnsLabelWarningAfterDurableSuccess(t *testing.T) {
	h := newSessionHarness(t)
	created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "restore"})
	if err != nil {
		t.Fatal(err)
	}
	lifecycle, err := NewLifecycle(harnessRuntime(t, h))
	if err != nil {
		t.Fatal(err)
	}
	row, err := h.store.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.ArchiveForHuman(ArchiveSelection{Sessions: []store.Session{row}}); err != nil {
		t.Fatal(err)
	}
	row, err = h.store.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := errors.New("label unavailable")
	lifecycle.setLabel = func(string, string) error { return want }

	result, err := lifecycle.RestoreForHuman(
		ArchiveSelection{Sessions: []store.Session{row}},
		PaneSize{Width: 80, Height: 24},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(result.LabelError, want) {
		t.Fatalf("label warning = %v, want %v", result.LabelError, want)
	}
	stored, err := h.store.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Archived || !h.driver.Exists(created.ID) {
		t.Fatalf("restore did not persist: archived=%v pane=%v", stored.Archived, h.driver.Exists(created.ID))
	}
}

func TestLifecycleRestorePartialResultKeepsArchiveFlagWhenStoreWriteFails(t *testing.T) {
	h := newSessionHarness(t)
	lifecycle, err := NewLifecycle(harnessRuntime(t, h))
	if err != nil {
		t.Fatal(err)
	}
	sess := store.Session{ID: "restore1", Name: "restore", Tool: "echoer", Cwd: t.TempDir(), Archived: true}
	if err := h.driver.Create(sess.ID, sess.Cwd, "sleep 30", nil, 80, 24); err != nil {
		t.Fatal(err)
	}

	result, err := lifecycle.RestoreForHuman(ArchiveSelection{Sessions: []store.Session{sess}}, PaneSize{Width: 80, Height: 24})
	if err == nil {
		t.Fatal("restore unexpectedly succeeded for a missing row")
	}
	if len(result.Sessions) != 1 || result.Sessions[0].ID != sess.ID {
		t.Fatalf("partial restore = %+v", result)
	}
	if !result.Sessions[0].Archived {
		t.Fatal("restore result cleared archive before the store write succeeded")
	}
}

func TestDeleteForHumanDeletesChildrenBeforeAParentFailure(t *testing.T) {
	h := newSessionHarness(t)
	runtime := harnessRuntime(t, h)
	lifecycle, err := NewLifecycle(runtime)
	if err != nil {
		t.Fatal(err)
	}
	parent := store.Session{ID: "parent01", Name: "parent", Tool: "echoer", Cwd: t.TempDir(), Status: status.Idle}
	child := store.Session{ID: "child001", Name: "child", Tool: "terminal", Cwd: parent.Cwd, Status: status.Idle, ParentID: parent.ID}
	if err := h.store.CreateSession(parent); err != nil {
		t.Fatal(err)
	}
	if err := h.store.CreateSession(child); err != nil {
		t.Fatal(err)
	}
	for _, sess := range []store.Session{parent, child} {
		if err := h.driver.Create(sess.ID, sess.Cwd, "sleep 30", nil, 80, 24); err != nil {
			t.Fatal(err)
		}
	}
	blocked := runtime.Hooks.StatusFile(parent.ID)
	if err := os.MkdirAll(filepath.Join(blocked, "held"), 0o755); err != nil {
		t.Fatal(err)
	}
	result, err := lifecycle.DeleteForHuman(DeleteSelection{Sessions: []store.Session{parent, child}})
	if err == nil {
		t.Fatal("parent hook cleanup unexpectedly succeeded")
	}
	if len(result.Deleted) != 1 || result.Deleted[0].ID != child.ID {
		t.Fatalf("partial delete = %+v", result)
	}
	if _, err := h.store.Get(child.ID); err == nil {
		t.Fatal("child row survived")
	}
	if _, err := h.store.Get(parent.ID); err != nil {
		t.Fatalf("parent row was deleted before its child: %v", err)
	}
}

func TestHumanArchiveOfDeadSessionDoesNotRequireHookCleanup(t *testing.T) {
	h := newSessionHarness(t)
	runtime := harnessRuntime(t, h)
	sess := store.Session{ID: "dead-archive", Name: "dead", Tool: "echoer", Cwd: t.TempDir(), Status: status.Dead}
	if err := h.store.CreateSession(sess); err != nil {
		t.Fatal(err)
	}
	hook := runtime.Hooks.StatusFile(sess.ID)
	if err := os.MkdirAll(hook, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hook, "malformed"), []byte("fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	lifecycle, err := NewLifecycle(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.ArchiveForHuman(ArchiveSelection{Sessions: []store.Session{sess}}); err != nil {
		t.Fatalf("dead archive required hook cleanup: %v", err)
	}
	stored, err := h.store.Get(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.Archived || stored.Status != status.Dead {
		t.Fatalf("dead archive result = %+v", stored)
	}
}

func TestLifecycleLaunchReportsFailedPaneRollback(t *testing.T) {
	h := newSessionHarness(t)
	runtime := harnessRuntime(t, h)
	lifecycle, err := NewLifecycle(runtime)
	if err != nil {
		t.Fatal(err)
	}
	rollbackErr := errors.New("pane removal failed")
	lifecycle.killPane = func(string) error { return rollbackErr }
	db, err := sql.Open("sqlite", filepath.Join(h.sessions.configDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TRIGGER refuse_launch BEFORE INSERT ON sessions WHEN NEW.id = 'rollback-failure' BEGIN SELECT RAISE(ABORT, 'row persistence failed'); END`); err != nil {
		t.Fatal(err)
	}
	sess := store.Session{ID: "rollback-failure", Name: "rollback", Tool: "echoer", Cwd: t.TempDir(), Status: status.Starting}
	_, err = lifecycle.Launch(LaunchRequest{Session: sess, Tool: runtime.Config.Tools[sess.Tool], BaseCommand: "sleep 30", Pane: PaneSize{Width: 80, Height: 24}})
	if !errors.Is(err, rollbackErr) || !strings.Contains(err.Error(), sess.ID) || !strings.Contains(err.Error(), "has no row") || !strings.Contains(err.Error(), "row persistence failed") {
		t.Fatalf("rollback error omitted persistence or surviving pane context: %v", err)
	}
	if !h.driver.Exists(sess.ID) {
		t.Fatal("rollback fixture did not leave a pane")
	}
}

// An agent that asked to be archived once its turn ends goes the way the
// archive key sends it: it and the shells nested under it stop, their last
// screens stay, and the request is spent.
func TestLifecycleEndAfterTurnArchivesTheSessionAndItsShells(t *testing.T) {
	h := newSessionHarness(t)
	created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Tool: "resting", Name: "alpha"})
	if err != nil {
		t.Fatal(err)
	}
	shell, err := h.terminals.Create(created.ID, CreateTerminalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	waitForSessionOutput(t, h.sessions, h.caller.ID, created.ID, "❯")
	lifecycle, err := NewLifecycle(harnessRuntime(t, h))
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.RequestAfterTurn(created.ID, store.AfterTurnArchive, time.Now()); err != nil {
		t.Fatal(err)
	}
	// The request stamps the row working, so nothing ends until a rest.
	if result, err := lifecycle.EndAfterTurn(created.ID); err != nil || result.Action != "" || !h.driver.Exists(created.ID) {
		t.Fatalf("a working session was ended: %+v, %v", result, err)
	}
	if err := h.store.UpdateStatus(created.ID, status.Idle); err != nil {
		t.Fatal(err)
	}
	result, err := lifecycle.EndAfterTurn(created.ID)
	if err != nil || result.Action != store.AfterTurnArchive || len(result.Sessions) != 2 {
		t.Fatalf("EndAfterTurn = %+v, %v", result, err)
	}
	for _, id := range []string{created.ID, shell.ID} {
		got, err := h.store.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if !got.Archived || h.driver.Exists(id) || got.AfterTurn != "" {
			t.Fatalf("%s: archived=%v running=%v pending=%q", got.Name, got.Archived, h.driver.Exists(id), got.AfterTurn)
		}
	}
	if snapshot, _ := h.store.Snapshot(created.ID); !strings.Contains(snapshot, "❯") {
		t.Fatalf("the archived row lost its last screen: %q", snapshot)
	}
}

func TestLifecycleEndAfterTurnKillsAndKeepsTheRow(t *testing.T) {
	h := newSessionHarness(t)
	created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Tool: "resting", Name: "alpha"})
	if err != nil {
		t.Fatal(err)
	}
	lifecycle, err := NewLifecycle(harnessRuntime(t, h))
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.RequestAfterTurn(created.ID, store.AfterTurnKill, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := h.store.UpdateStatus(created.ID, status.Finished); err != nil {
		t.Fatal(err)
	}
	if result, err := lifecycle.EndAfterTurn(created.ID); err != nil || result.Action != store.AfterTurnKill {
		t.Fatalf("EndAfterTurn = %+v, %v", result, err)
	}
	got, err := h.store.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if h.driver.Exists(created.ID) || got.Status != status.Dead || got.Archived || got.AfterTurn != "" {
		t.Fatalf("after its turn: running=%v status=%q archived=%v pending=%q", h.driver.Exists(created.ID), got.Status, got.Archived, got.AfterTurn)
	}
}
