package sessioncmd

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
)

func TestLifecycleRestoreArchiveWriteFailureStopsOnlyUncommittedNewPane(t *testing.T) {
	for _, live := range []bool{false, true} {
		name := "new pane"
		if live {
			name = "existing pane"
		}
		t.Run(name, func(t *testing.T) {
			h := newSessionHarness(t)
			first := store.Session{ID: "restore-first", Name: "first", Tool: "echoer", Cwd: t.TempDir(), Status: status.Dead, Archived: true}
			failed := store.Session{ID: "restore-failed", Name: "failed", Tool: "echoer", Cwd: t.TempDir(), Status: status.Dead, Archived: true}
			for _, sess := range []store.Session{first, failed} {
				if err := h.store.CreateSession(sess); err != nil {
					t.Fatal(err)
				}
			}
			if live {
				if err := h.driver.Create(failed.ID, failed.Cwd, "sleep 30", nil, 80, 24); err != nil {
					t.Fatal(err)
				}
			}
			if err := h.store.SetSnapshot(failed.ID, "ARCHIVED WORK"); err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("sqlite", filepath.Join(h.sessions.configDir, "state.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err := db.Exec("CREATE TRIGGER refuse_unarchive BEFORE UPDATE OF archived ON sessions WHEN NEW.id = 'restore-failed' AND NEW.archived = 0 BEGIN SELECT RAISE(ABORT, 'archive write refused'); END"); err != nil {
				t.Fatal(err)
			}
			lifecycle, err := NewLifecycle(harnessRuntime(t, h))
			if err != nil {
				t.Fatal(err)
			}

			result, err := lifecycle.RestoreForHuman(ArchiveSelection{Sessions: []store.Session{first, failed}}, PaneSize{80, 24})
			if err == nil || !strings.Contains(err.Error(), "archive write refused") {
				t.Fatalf("restore error = %v", err)
			}
			if !h.driver.Exists(first.ID) {
				t.Error("rollback stopped a successfully restored session")
			}
			if h.driver.Exists(failed.ID) != live {
				t.Errorf("failed restore pane exists = %v, want %v", h.driver.Exists(failed.ID), live)
			}
			if len(result.Sessions) != 2 || result.Sessions[0].Archived || !result.Sessions[1].Archived || (!live && result.Sessions[1].Status != status.Dead) {
				t.Errorf("partial restore = %+v", result)
			}
			for _, sess := range []store.Session{first, failed} {
				stored, err := h.store.Get(sess.ID)
				if err != nil {
					t.Fatal(err)
				}
				if stored.Archived != (sess.ID == failed.ID) {
					t.Errorf("durable archive state = %+v", stored)
				}
				if !live && sess.ID == failed.ID && stored.Status != status.Dead {
					t.Errorf("failed restore status = %s, want dead", stored.Status)
				}
			}
			if snapshot, err := h.store.Snapshot(failed.ID); err != nil || snapshot != "ARCHIVED WORK" {
				t.Errorf("rollback overwrote archived work: %q %v", snapshot, err)
			}
		})
	}
}
