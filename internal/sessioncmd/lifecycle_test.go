package sessioncmd

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/launch"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
)

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
		Session: sess, Tool: runtime.Config.CLIs[sess.Tool], BaseCommand: "sleep 30", Pane: PaneSize{Width: 80, Height: 24},
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
		Session: sess, Tool: runtime.Config.CLIs[sess.Tool], BaseCommand: "sleep 30", Pane: PaneSize{Width: 80, Height: 24},
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
	// Restore reads each row again, so the missing directory has to be the
	// stored one: the second session gets its own, removed after archiving.
	secondDir := t.TempDir()
	second, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "second", Directory: secondDir})
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
	if err := os.RemoveAll(secondDir); err != nil {
		t.Fatal(err)
	}
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
	_, err = lifecycle.Launch(LaunchRequest{Session: sess, Tool: runtime.Config.CLIs[sess.Tool], BaseCommand: "sleep 30", Pane: PaneSize{Width: 80, Height: 24}})
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

func TestLifecycleSpawnHandsBackTheWorktreeWhenLaunchFails(t *testing.T) {
	h := newSessionHarness(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := newTestGitRepo(t, t.TempDir())
	runtime := harnessRuntime(t, h)
	driver, err := git.New()
	if err != nil {
		t.Fatal(err)
	}
	runtime.Git = driver
	lifecycle, err := NewLifecycle(runtime)
	if err != nil {
		t.Fatal(err)
	}
	taken := store.Session{ID: "taken", Name: "taken", Tool: "echoer", Cwd: t.TempDir(), Status: status.Starting}
	if err := h.store.CreateSession(taken); err != nil {
		t.Fatal(err)
	}
	_, err = lifecycle.Spawn(SpawnRequest{
		Session:  store.Session{ID: taken.ID, Name: "spawned", Tool: "echoer", Cwd: repo.root},
		Tool:     runtime.Config.CLIs["echoer"],
		Plan:     launch.Plan{Command: "sleep 30"},
		Pane:     PaneSize{Width: 80, Height: 24},
		Worktree: &WorktreeRequest{Base: repo.baseRef},
	})
	if err == nil {
		t.Fatal("spawn onto a taken id succeeded")
	}
	worktrees, err := exec.Command("git", "-C", repo.root, "worktree", "list", "--porcelain").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(worktrees), "worktree ") != 1 {
		t.Fatalf("failed spawn left its worktree:\n%s", worktrees)
	}
}

// A launch whose time the row cannot record names an agent no revive would
// recognize, so the pane started for it goes too.
func TestRelaunchKillsTheNewPaneWhenItsLaunchCannotPersist(t *testing.T) {
	h := newSessionHarness(t)
	runtime := harnessRuntime(t, h)
	lifecycle, err := NewLifecycle(runtime)
	if err != nil {
		t.Fatal(err)
	}
	sess := store.Session{ID: "phoenix1", Name: "phoenix", Tool: "echoer", Cwd: t.TempDir(), Status: status.Dead}
	if err := h.store.CreateSession(sess); err != nil {
		t.Fatal(err)
	}
	tool := runtime.Config.CLIs[sess.Tool]
	bind := func(launchedAt time.Time) error {
		if err := h.store.Delete(sess.ID); err != nil {
			return err
		}
		return h.store.SetAgentLaunchedAt(sess.ID, launchedAt)
	}
	_, err = lifecycle.launchExisting(sess, tool, "sleep 30", PaneSize{Width: 80, Height: 24}, false, false, bind)
	if !errors.Is(err, store.ErrSessionGone) {
		t.Fatalf("relaunch error = %v, want ErrSessionGone", err)
	}
	if h.driver.Exists(sess.ID) {
		t.Fatal("the pane started for a launch the row never recorded is still running")
	}
}

func TestLifecycleRefusesArchivedRelaunchFromStaleRow(t *testing.T) {
	for _, operation := range []string{"revive", "restart"} {
		for _, live := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/live=%v", operation, live), func(t *testing.T) {
				h := newSessionHarness(t)
				created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "archived"})
				if err != nil {
					t.Fatal(err)
				}
				listed, err := h.store.Get(created.ID)
				if err != nil {
					t.Fatal(err)
				}
				if !live {
					if err := h.driver.Kill(listed.ID); err != nil {
						t.Fatal(err)
					}
				}
				if err := h.store.SetArchived(listed.ID, true); err != nil {
					t.Fatal(err)
				}
				lifecycle, err := NewLifecycle(harnessRuntime(t, h))
				if err != nil {
					t.Fatal(err)
				}
				if operation == "revive" {
					_, err = lifecycle.Revive(listed, PaneSize{Width: 80, Height: 24})
				} else {
					_, err = lifecycle.Restart(listed, PaneSize{Width: 80, Height: 24})
				}
				if err == nil || !strings.Contains(err.Error(), "archived; restore") {
					t.Fatalf("error = %v", err)
				}
				stored, err := h.store.Get(listed.ID)
				if err != nil {
					t.Fatal(err)
				}
				if !stored.Archived || !stored.AgentLaunchedAt.Equal(listed.AgentLaunchedAt) || stored.AgentSessionID != listed.AgentSessionID || h.driver.Exists(listed.ID) != live {
					t.Fatalf("relaunch changed archived row: before=%+v after=%+v live=%v", listed, stored, h.driver.Exists(listed.ID))
				}
			})
		}
	}
}

func TestFailedGroupRestoreStopsOnlyPanesItStarted(t *testing.T) {
	h := newSessionHarness(t)
	group := "zone"
	if err := h.store.CreateGroup(group, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	var members []store.Session
	for _, name := range []string{"preexisting", "first", "missing"} {
		created, err := h.sessions.Create("", CreateSessionOptions{Name: name, Group: &group, Directory: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		row, err := h.store.Get(created.ID)
		if err != nil {
			t.Fatal(err)
		}
		members = append(members, row)
	}
	lifecycle, err := NewLifecycle(harnessRuntime(t, h))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.ArchiveForHuman(ArchiveSelection{Sessions: members, GroupPath: group}); err != nil {
		t.Fatal(err)
	}
	for i := range members {
		members[i], err = h.store.Get(members[i].ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := h.driver.Create(members[0].ID, members[0].Cwd, "sleep 30", nil, 80, 24); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(members[2].Cwd); err != nil {
		t.Fatal(err)
	}
	if err := h.store.SetSnapshot(members[1].ID, "ARCHIVED WORK"); err != nil {
		t.Fatal(err)
	}

	result, err := lifecycle.RestoreForHuman(ArchiveSelection{Sessions: members, GroupPath: group}, PaneSize{Width: 80, Height: 24})
	if err == nil {
		t.Fatal("restore unexpectedly succeeded")
	}
	if !h.driver.Exists(members[0].ID) {
		t.Error("rollback killed a preexisting pane")
	}
	if h.driver.Exists(members[1].ID) {
		t.Error("failed group restore left newly started pane running while archived")
	}
	if snapshot, err := h.store.Snapshot(members[1].ID); err != nil || snapshot != "ARCHIVED WORK" {
		t.Errorf("rollback overwrote archived work: %q %v", snapshot, err)
	}

	for _, row := range members {
		stored, err := h.store.Get(row.ID)
		if err != nil || !stored.Archived {
			t.Errorf("archive membership changed: %+v %v", stored, err)
		}
	}
	for _, row := range result.Sessions {
		if row.ID == members[1].ID && row.Status != status.Dead {
			t.Errorf("rollback result incorrectly reports %s", row.Status)
		}
	}
}

func TestRestoreFailureAfterPaneCreationStopsThePane(t *testing.T) {
	for _, column := range []string{"status", "acked"} {
		t.Run(column, func(t *testing.T) {
			h := newSessionHarness(t)
			if err := h.store.CreateGroup("zone", t.TempDir()); err != nil {
				t.Fatal(err)
			}
			sess := store.Session{ID: "restore-failure", Name: "restore-failure", Tool: "echoer", Cwd: t.TempDir(), Group: "zone", Archived: true, Status: status.Dead}
			if err := h.store.CreateSession(sess); err != nil {
				t.Fatal(err)
			}
			if err := h.store.SetGroupArchived("zone", true); err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("sqlite", filepath.Join(h.sessions.configDir, "state.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			trigger := fmt.Sprintf("CREATE TRIGGER refuse_restore BEFORE UPDATE OF %s ON sessions WHEN NEW.id = 'restore-failure' AND NEW.%s = %s BEGIN SELECT RAISE(ABORT, 'restore write refused'); END", column, column, map[string]string{"status": "'starting'", "acked": "0"}[column])
			if _, err := db.Exec(trigger); err != nil {
				t.Fatal(err)
			}
			lifecycle, err := NewLifecycle(harnessRuntime(t, h))
			if err != nil {
				t.Fatal(err)
			}
			_, err = lifecycle.RestoreForHuman(ArchiveSelection{Sessions: []store.Session{sess}, GroupPath: "zone"}, PaneSize{80, 24})
			if err == nil || !strings.Contains(err.Error(), "restore write refused") {
				t.Fatalf("restore error: %v", err)
			}
			if h.driver.Exists(sess.ID) {
				t.Fatal("failed restore left its newly created archived pane running")
			}
		})
	}
}

func TestFailedGroupRestoreReportsRollbackFailure(t *testing.T) {
	h := newSessionHarness(t)
	if err := h.store.CreateGroup("zone", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	first := store.Session{ID: "first-rollback", Name: "first", Tool: "echoer", Cwd: t.TempDir(), Group: "zone", Status: status.Dead, Archived: true}
	second := store.Session{ID: "second-rollback", Name: "second", Tool: "echoer", Cwd: t.TempDir(), Group: "zone", Status: status.Dead, Archived: true}
	for _, sess := range []store.Session{first, second} {
		if err := h.store.CreateSession(sess); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.store.SetGroupArchived("zone", true); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(second.Cwd); err != nil {
		t.Fatal(err)
	}
	lifecycle, err := NewLifecycle(harnessRuntime(t, h))
	if err != nil {
		t.Fatal(err)
	}
	rollbackErr := errors.New("fixture cannot stop pane")
	lifecycle.killPane = func(string) error { return rollbackErr }
	result, err := lifecycle.RestoreForHuman(ArchiveSelection{Sessions: []store.Session{first, second}, GroupPath: "zone"}, PaneSize{80, 24})
	if !errors.Is(err, rollbackErr) || !strings.Contains(err.Error(), "working directory no longer exists") || !strings.Contains(err.Error(), "stop failed restore of first") {
		t.Fatalf("error lost original or cleanup failure: %v", err)
	}
	if !h.driver.Exists(first.ID) || len(result.Sessions) != 1 || result.Sessions[0].Status != status.Starting || !result.Sessions[0].Archived {
		t.Fatalf("rollback result hid running archived pane: %+v", result)
	}
}

func TestLifecycleRelaunchReportsDeletedRow(t *testing.T) {
	h := newSessionHarness(t)
	lifecycle, err := NewLifecycle(harnessRuntime(t, h))
	if err != nil {
		t.Fatal(err)
	}
	sess := store.Session{ID: "gone-restore"}
	for _, operation := range []string{"revive", "restart", "restore"} {
		switch operation {
		case "revive":
			_, err = lifecycle.Revive(sess, PaneSize{80, 24})
		case "restart":
			_, err = lifecycle.Restart(sess, PaneSize{80, 24})
		case "restore":
			_, err = lifecycle.RestoreForHuman(ArchiveSelection{Sessions: []store.Session{sess}}, PaneSize{80, 24})
		}
		if !errors.Is(err, store.ErrSessionGone) {
			t.Errorf("%s deleted row: %v", operation, err)
		}
	}
}
