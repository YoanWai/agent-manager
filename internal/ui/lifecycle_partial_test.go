package ui

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
)

func TestArchiveReconcilesDurableRowsBeforeLaterFailure(t *testing.T) {
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
	m := buildModel(t)
	first := store.Session{ID: "cafe", Name: "first", Tool: "claude", Cwd: t.TempDir(), Status: status.Dead, Archived: true}
	second := store.Session{ID: "beef", Name: "second", Tool: "claude", Cwd: filepath.Join(t.TempDir(), "missing"), Status: status.Dead, Archived: true}
	for _, sess := range []store.Session{first, second} {
		if err := m.services.store.CreateSession(sess); err != nil {
			t.Fatal(err)
		}
	}
	loadStoredRows(t, m)
	m.confirm.confirmTarget = confirmTarget{action: actionRestore, sessions: []store.Session{first, second}}
	if err := m.restoreConfirmed(); err == nil {
		t.Fatal("expected second revive failure")
	}
	got, _ := m.sessionByID(first.ID)
	if got.Archived {
		t.Fatal("completed restore remains in archived inventory")
	}
	untouched, _ := m.sessionByID(second.ID)
	if !untouched.Archived {
		t.Fatal("failed restore changed untouched row")
	}
}

func TestFailedGroupRestoreDoesNotClearMembership(t *testing.T) {
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
	sess := store.Session{ID: "cafe", Name: "first", Group: "zone", Tool: "claude", Cwd: t.TempDir(), Status: status.Dead, Archived: true}
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
}
