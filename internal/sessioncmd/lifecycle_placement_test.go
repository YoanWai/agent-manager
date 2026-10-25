package sessioncmd

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/launch"
	"github.com/YoanWai/agent-manager/internal/store"
)

func TestLifecycleNewSessionRejectsArchivedPlacement(t *testing.T) {
	for _, operation := range []string{"spawn", "launch"} {
		for _, placement := range []string{"group", "ancestor", "parent", "session"} {
			t.Run(operation+"/"+placement, func(t *testing.T) {
				h := newSessionHarness(t)
				runtime := harnessRuntime(t, h)
				lifecycle, err := NewLifecycle(runtime)
				if err != nil {
					t.Fatal(err)
				}
				sess := store.Session{ID: "blocked", Name: "blocked", Tool: "echoer", Cwd: t.TempDir()}
				if placement == "session" {
					sess.Archived = true
				} else if placement == "parent" {
					parent := store.Session{ID: "archived-parent", Name: "parent", Tool: "echoer", Cwd: sess.Cwd, Archived: true}
					if err := h.store.CreateSession(parent); err != nil {
						t.Fatal(err)
					}
					sess.ParentID = parent.ID
				} else {
					if err := h.store.CreateGroup("closed", sess.Cwd); err != nil {
						t.Fatal(err)
					}
					sess.Group = "closed"
					if placement == "ancestor" {
						if err := h.store.CreateGroup("closed/child", sess.Cwd); err != nil {
							t.Fatal(err)
						}
						sess.Group = "closed/child"
					}
					if err := h.store.SetGroupArchived("closed", true); err != nil {
						t.Fatal(err)
					}
				}
				tool := runtime.Config.Tools[sess.Tool]
				if operation == "spawn" {
					_, err = lifecycle.Spawn(SpawnRequest{Session: sess, Tool: tool, Plan: launch.Plan{Command: "sleep 30"}, Pane: PaneSize{80, 24}})
				} else {
					_, err = lifecycle.Launch(LaunchRequest{Session: sess, Tool: tool, BaseCommand: "sleep 30", Pane: PaneSize{80, 24}})
				}
				if err == nil || !strings.Contains(err.Error(), "archived") {
					t.Errorf("creation error = %v, want archived placement refused", err)
				}
				if h.driver.Exists(sess.ID) {
					t.Error("refused placement created a running pane")
				}
				if _, err := h.store.Get(sess.ID); !errors.Is(err, sql.ErrNoRows) {
					t.Errorf("refused placement persisted a row: %v", err)
				}
			})
		}
	}
}

func TestLifecycleLaunchRefusesArchivedPlacementFromAnchor(t *testing.T) {
	for _, placement := range []string{"archived-group", "archived-anchor"} {
		t.Run(placement, func(t *testing.T) {
			h := newSessionHarness(t)
			runtime := harnessRuntime(t, h)
			lifecycle, err := NewLifecycle(runtime)
			if err != nil {
				t.Fatal(err)
			}
			anchor := store.Session{ID: "anchor", Name: "anchor", Tool: "echoer", Cwd: t.TempDir()}
			if err := h.store.CreateSession(anchor); err != nil {
				t.Fatal(err)
			}
			if placement == "archived-group" {
				if err := h.store.CreateGroup("closed", anchor.Cwd); err != nil {
					t.Fatal(err)
				}
				if err := h.store.SetGroupArchived("closed", true); err != nil {
					t.Fatal(err)
				}
				if err := h.store.MoveSession(anchor.ID, "closed"); err != nil {
					t.Fatal(err)
				}
			} else if err := h.store.SetArchived(anchor.ID, true); err != nil {
				t.Fatal(err)
			}
			child := store.Session{ID: "child", Name: "child", Tool: "echoer", Cwd: anchor.Cwd}
			_, err = lifecycle.Launch(LaunchRequest{Session: child, Tool: runtime.Config.Tools[child.Tool], BaseCommand: "sleep 30", Pane: PaneSize{80, 24}, BesideSessionID: anchor.ID})
			if err == nil || !strings.Contains(err.Error(), "archived") {
				t.Errorf("launch beside archived placement error = %v", err)
			}
			if h.driver.Exists(child.ID) {
				t.Error("failed placement left pane running")
			}
			if _, err := h.store.Get(child.ID); !errors.Is(err, sql.ErrNoRows) {
				t.Errorf("failed placement left row: %v", err)
			}
		})
	}
}

func TestLifecyclePlacementCommitRechecksAfterPreflight(t *testing.T) {
	h := newSessionHarness(t)
	lifecycle, err := NewLifecycle(harnessRuntime(t, h))
	if err != nil {
		t.Fatal(err)
	}
	sess := store.Session{ID: "child", Name: "child", Tool: "echoer", Cwd: t.TempDir(), Group: "closing"}
	if err := h.store.CreateGroup(sess.Group, sess.Cwd); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.ValidatePlacement(sess); err != nil {
		t.Fatal(err)
	}
	if err := h.store.SetGroupArchived(sess.Group, true); err != nil {
		t.Fatal(err)
	}
	if err := h.store.CreateActiveSession(sess); err == nil || !strings.Contains(err.Error(), "archived") {
		t.Fatalf("commit accepted placement archived after preflight: %v", err)
	}
	if _, err := h.store.Get(sess.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("failed commit left row: %v", err)
	}
}
