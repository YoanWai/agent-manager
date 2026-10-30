package ui

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/testfixture"
	tea "github.com/charmbracelet/bubbletea"
)

func TestArchiveReconcilesDurableRowsBeforeLaterFailure(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	first := store.Session{ID: "cafe", Name: "first", Tool: "claude", Cwd: t.TempDir(), Status: status.Dead}
	if err := m.services.store.CreateSession(first); err != nil {
		t.Fatal(err)
	}
	loadStoredRows(t, m)
	m.confirm.confirmTarget = confirmTarget{action: actionArchive, sessions: []store.Session{first, {ID: "beef", Status: status.Dead}}}
	if err := m.archiveConfirmed(); err == nil {
		t.Fatal("expected missing second row failure")
	}
	got, _ := m.sessionByID(first.ID)
	if !got.Archived {
		t.Fatal("completed archive remains in active inventory")
	}
	if mark := m.ledger.gone[first.ID]; !mark.archived {
		t.Fatal("completed archive lacks stale-poll guard")
	}
}

func TestRestoreReconcilesDurableRowsBeforeLaterFailure(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	first := store.Session{ID: newID(), Name: "first", Tool: "claude", Cwd: t.TempDir(), Status: status.Dead, Archived: true}
	second := store.Session{ID: newID(), Name: "second", Tool: "claude", Cwd: filepath.Join(t.TempDir(), "missing"), Status: status.Dead, Archived: true}
	for _, sess := range []store.Session{first, second} {
		if err := m.services.store.CreateSession(sess); err != nil {
			t.Fatal(err)
		}
	}
	loadStoredRows(t, m)
	m.confirm.confirmTarget = confirmTarget{action: actionRestore, sessions: []store.Session{first, second}}
	err := m.restoreConfirmed()
	if err == nil {
		t.Fatal("expected second revive failure")
	}
	got, _ := m.sessionByID(first.ID)
	if got.Archived {
		t.Fatalf("completed restore remains in archived inventory; restore failed with: %v", err)
	}
	untouched, _ := m.sessionByID(second.ID)
	if !untouched.Archived {
		t.Fatal("failed restore changed untouched row")
	}
}

func TestFailedGroupRestoreDoesNotClearMembership(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dbPath := filepath.Join(t.TempDir(), "state.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	m.services.store = st
	m.services.setSnapshot = st.SetSnapshot
	m.services.lifecycle, err = sessioncmd.NewLifecycle(sessioncmd.Runtime{Config: m.services.cfg, Store: st, Driver: m.services.tmux, Hooks: m.services.hooks, Snapshot: st.SetSnapshot})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.services.store.CreateGroup("zone", ""); err != nil {
		t.Fatal(err)
	}
	sess := store.Session{ID: newID(), Name: "first", Group: "zone", Tool: "claude", Cwd: t.TempDir(), Status: status.Dead, Archived: true}
	if err := m.services.store.CreateSession(sess); err != nil {
		t.Fatal(err)
	}
	if err := m.services.store.SetGroupArchived("zone", true); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TRIGGER refuse_group_restore BEFORE UPDATE OF archived ON groups BEGIN SELECT RAISE(ABORT, 'group write refused'); END`); err != nil {
		t.Fatal(err)
	}
	loadStoredRows(t, m)
	m.confirm.confirmTarget = confirmTarget{action: actionRestore, isGroup: true, path: "zone", sessions: []store.Session{sess}}
	if err := m.restoreConfirmed(); err == nil {
		t.Fatal("expected group write failure")
	}
	got, _ := m.sessionByID(sess.ID)
	if !got.Archived || !m.workspace.archivedGroups["zone"] {
		t.Fatal("failed group write cleared local membership")
	}
	if m.services.tmux.Exists(sess.ID) {
		t.Fatal("failed group write left a newly started archived pane")
	}
	if _, ok := m.ledger.launched[sess.ID]; ok {
		t.Fatal("failed group write retained a launch receipt")
	}

}

func TestGroupRestoreRetryKeepsScopeAndRollsBackFailedLaunches(t *testing.T) {
	for _, failFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("first=%v", failFirst), func(t *testing.T) {
			m := buildModel(t)
			seedGroups(t, m, "zone", "zone/nested")
			missingTool := "codex"
			m.services.cfg.Tools["claude"] = config.Tool{Command: testfixture.Cat(), DefaultStatus: status.Idle}
			m.services.cfg.Tools[missingTool] = config.Tool{Command: "am-missing-cli-564", DefaultStatus: status.Idle}
			first := store.Session{ID: newID(), Name: "first", Group: "zone", Tool: "claude", Cwd: t.TempDir(), Status: status.Dead, Archived: true}
			second := store.Session{ID: newID(), Name: "second", Group: "zone/nested", Tool: missingTool, Cwd: t.TempDir(), Status: status.Dead, Archived: true}
			if failFirst {
				first.Tool, second.Tool = second.Tool, first.Tool
			}
			sleeper := store.Session{ID: newID(), Name: "sleeper", Group: "zone", Tool: "claude", Cwd: t.TempDir(), Status: status.Dead}
			for _, sess := range []store.Session{first, second, sleeper} {
				if err := m.services.store.CreateSession(sess); err != nil {
					t.Fatal(err)
				}
			}
			if err := m.services.store.SetGroupArchived("zone/nested", true); err != nil {
				t.Fatal(err)
			}
			m.rail.SetArchived(true)
			m.applyCmd(t, m.refreshCmd())
			m.selectGroupRow(t, "zone")
			m.restoreSelected()
			m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
			m.drainEffects(t)
			for attempt := 0; attempt < 2; attempt++ {
				if m.mode != modeLaunchHint {
					t.Fatalf("attempt %d mode=%v error=%s", attempt, m.mode, m.errBar.text)
				}
				for _, sess := range []store.Session{first, second} {
					stored, err := m.services.store.Get(sess.ID)
					if err != nil || !stored.Archived || m.services.tmux.Exists(sess.ID) {
						t.Fatalf("failed restore left pane/membership: %+v %v", stored, err)
					}
					if _, ok := m.ledger.launched[sess.ID]; ok {
						t.Fatal("rolled-back pane retained a launch receipt")
					}
				}
				if m.launchHint.fix.effectRetry == nil {
					t.Fatal("missing group retry")
				}
				retry := m.launchHint.fix.effectRetry
				m.mode = modeList
				if attempt == 1 {
					m.services.cfg.Tools[missingTool] = config.Tool{Command: testfixture.Cat(), DefaultStatus: status.Idle}
				}
				m.enqueueEffect(retry, 0, false)
				m.drainEffects(t)
			}
			for _, sess := range []store.Session{first, second} {
				stored, err := m.services.store.Get(sess.ID)
				if err != nil || stored.Archived || !m.services.tmux.Exists(sess.ID) {
					t.Fatalf("group retry incomplete: %+v %v", stored, err)
				}
			}
			groups, err := m.services.store.Groups()
			if err != nil {
				t.Fatal(err)
			}
			for _, group := range groups {
				if (group.Name == "zone" || group.Name == "zone/nested") && group.Archived {
					t.Fatalf("group still archived: %+v", group)
				}
			}
			if m.services.tmux.Exists(sleeper.ID) {
				t.Fatal("restore revived intentionally killed active sibling")
			}
		})
	}
}

func TestNestedRestoreAncestorFailureRollsBackAndRetries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	m := buildModelWithStorePath(t, path)
	seedGroups(t, m, "a", "a/b")
	dead := store.Session{ID: newID(), Name: "dead", Tool: "claude", Cwd: t.TempDir(), Group: "a/b", Status: status.Dead}
	live := store.Session{ID: newID(), Name: "live", Tool: "claude", Cwd: t.TempDir(), Group: "a/b", Status: status.Idle}
	for _, sess := range []store.Session{dead, live} {
		if err := m.services.store.CreateSession(sess); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.services.store.SetGroupArchived("a", true); err != nil {
		t.Fatal(err)
	}
	if err := m.services.tmux.Create(live.ID, live.Cwd, m.services.cfg.Tools[live.Tool].Command, nil, 80, 24); err != nil {
		t.Fatal(err)
	}
	pid, err := m.services.tmux.PanePID(live.ID)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TRIGGER refuse_ancestor BEFORE UPDATE OF archived ON groups WHEN OLD.name='a' AND NEW.archived=0 BEGIN SELECT RAISE(ABORT,'ancestor refused'); END`); err != nil {
		t.Fatal(err)
	}
	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "a/b")
	m.restoreSelected()
	m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.drainEffects(t)
	if !strings.Contains(m.errBar.text, "ancestor refused") || m.services.tmux.Exists(dead.ID) {
		t.Fatalf("failed restore leaked launch: %s", m.errBar.text)
	}
	for _, sess := range []store.Session{dead, live} {
		stored, err := m.services.store.Get(sess.ID)
		if err != nil || !stored.Archived {
			t.Fatalf("failed ancestor commit unarchived row: %+v %v", stored, err)
		}
	}
	if !m.workspace.archivedGroups["a"] || !m.workspace.archivedGroups["a/b"] {
		t.Fatal("failed commit changed local group flags")
	}
	if _, err := db.Exec(`DROP TRIGGER refuse_ancestor`); err != nil {
		t.Fatal(err)
	}
	m.restoreSelected()
	m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.drainEffects(t)
	if after, err := m.services.tmux.PanePID(live.ID); err != nil || after != pid {
		t.Fatalf("retry replaced preexisting pane: %d %v", after, err)
	}
	m.rail.SetArchived(false)
	if !m.focusSession(dead.ID) || !m.focusSession(live.ID) {
		t.Fatalf("retry still hidden: %s", m.errBar.text)
	}
}

func TestNestedRestoreInstallCompletesCapturedGroup(t *testing.T) {
	for _, method := range []string{"key", "mouse"} {
		t.Run(method, func(t *testing.T) {
			command := fakeInstallCommand(t)
			m := buildModel(t)
			m.layout.width, m.layout.height = 160, 40
			seedGroups(t, m, "a", "a/b", "a/b/deep", "a/sibling")
			first := store.Session{ID: newID(), Name: "first", Tool: "claude", Cwd: t.TempDir(), Group: "a/b", Status: status.Dead, Archived: true}
			second := store.Session{ID: newID(), Name: "missing", Tool: installRetryTool, Cwd: t.TempDir(), Group: "a/b/deep", Status: status.Dead, Archived: true}
			sleeper := store.Session{ID: newID(), Name: "sleeper", Tool: "claude", Cwd: t.TempDir(), Group: "a/sibling", Status: status.Dead}
			if err := m.services.store.SetGroupArchived("a", true); err != nil {
				t.Fatal(err)
			}
			for _, sess := range []store.Session{first, second, sleeper} {
				if err := m.services.store.CreateSession(sess); err != nil {
					t.Fatal(err)
				}
			}
			m.services.cfg.Tools[installRetryTool] = config.Tool{Command: "am-fake-cli", DefaultStatus: status.Idle}
			m.rail.SetArchived(true)
			m.applyCmd(t, m.refreshCmd())
			m.selectGroupRow(t, "a/b")
			m.restoreSelected()
			m.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
			m.drainEffects(t)
			if m.mode != modeLaunchHint || m.launchHint.fix.effectRetry == nil {
				t.Fatalf("missing captured retry: mode=%v error=%s", m.mode, m.errBar.text)
			}
			if m.services.tmux.Exists(first.ID) || !m.workspace.archivedGroups["a"] {
				t.Fatal("failed nested restore did not roll back")
			}
			// The fixture replaces only the installer recipe; the refused group request stays intact.
			m.launchHint.fix.command, m.launchHint.fix.script = command, command
			if method == "key" {
				m.applyCmd(t, pressInLaunchHint(t, m, 'i'))
			} else {
				m = clickLabel(t, m, "i install")
			}
			shell := terminalSession(t, m)
			if shell.Group != "" || m.rail.ShowArchived() {
				t.Fatalf("repair shell hidden in archive: %+v", shell)
			}
			waitForInstallToSettle(t, m)
			for _, sess := range []store.Session{first, second} {
				stored, err := m.services.store.Get(sess.ID)
				if err != nil || stored.Archived || !m.services.tmux.Exists(sess.ID) || !m.focusSession(sess.ID) {
					t.Fatalf("group retry did not reveal %s: %+v %v error=%s", sess.Name, stored, err, m.errBar.text)
				}
			}
			if m.services.tmux.Exists(sleeper.ID) {
				t.Fatal("install retry revived unrelated dead session")
			}
			if m.groupEffectivelyArchived("a/b") {
				t.Fatal("retry left archived ancestor")
			}
		})
	}
}
