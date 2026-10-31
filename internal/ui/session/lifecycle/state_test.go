package lifecycle

import (
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
)

func session(id string) store.Session { return store.Session{ID: id, Name: id} }

// A listing captured before a launch carries the row over, so a poll cannot
// blink a session this run just spawned off screen.
func TestPendingLaunchSurvivesListingCapturedBefore(t *testing.T) {
	t.Parallel()
	state := State{}
	launchedAt := time.Now()
	state.RecordLaunch("new", launchedAt)

	got := state.KeepPendingLaunches(nil, []store.Session{session("new")}, launchedAt.Add(-time.Second))
	if len(got) != 1 || got[0].ID != "new" {
		t.Fatalf("kept %+v, want the launched row", got)
	}
}

// The first listing to read the store after a launch is the authority on it,
// whether it reports the row or its absence.
func TestListingAfterLaunchRetiresThePendingRow(t *testing.T) {
	t.Parallel()
	state := State{}
	launchedAt := time.Now()
	state.RecordLaunch("new", launchedAt)

	got := state.KeepPendingLaunches(nil, []store.Session{session("new")}, launchedAt.Add(time.Second))
	if len(got) != 0 {
		t.Fatalf("kept %+v, want none", got)
	}
	if _, still := state.Launched["new"]; still {
		t.Fatal("a listing that postdates the launch must retire it")
	}
}

// A stale copy of a deleted row is dropped and a stale copy of an archive has
// its flag corrected, so the row cannot blink back for a frame.
func TestStaleListingIsReconciledAgainstThisRun(t *testing.T) {
	t.Parallel()
	state := State{}
	state.MarkSession("gone", GoneMark{Deleted: true})
	state.MarkSession("archived", GoneMark{Archived: true})

	got := state.DropRecentlyRemoved([]store.Session{session("gone"), session("archived")}, time.Now().Add(-time.Minute))
	if len(got) != 1 || got[0].ID != "archived" || !got[0].Archived {
		t.Fatalf("kept %+v, want the archive with its flag", got)
	}
}

// A listing that postdates a change retires the marker instead.
func TestFreshListingRetiresTheMarkers(t *testing.T) {
	t.Parallel()
	state := State{}
	state.MarkSession("archived", GoneMark{Archived: true})
	listedAt := time.Now().Add(time.Minute)

	got := state.DropRecentlyRemoved([]store.Session{session("archived")}, listedAt)
	if len(got) != 1 {
		t.Fatalf("kept %+v", got)
	}
	if len(state.Gone) != 0 {
		t.Fatalf("markers outlived a listing that postdates them: %v", state.Gone)
	}
}

// A deleted group's header cannot hang back onto the tree, and its metadata
// goes with it.
func TestDeletedGroupLeavesTheListing(t *testing.T) {
	t.Parallel()
	state := State{}
	state.MarkGroup("grp", GoneMark{Deleted: true})
	listing := Listing{
		Groups:         []string{"grp", "other"},
		GroupPaths:     map[string]string{"grp": "/tmp/grp", "other": "/tmp/other"},
		GroupWorktrees: map[string]string{"grp": "on"},
		GroupBases:     map[string]string{"grp": "main"},
		ArchivedGroups: map[string]bool{"grp": true},
	}

	listing.ListedAt = time.Now().Add(-time.Minute)
	got := state.ReconcileListing(listing)
	if len(got.Groups) != 1 || got.Groups[0] != "other" {
		t.Fatalf("groups = %v", got.Groups)
	}
	if _, ok := got.GroupPaths["grp"]; ok {
		t.Fatal("deleted group kept its path")
	}
	if _, ok := got.GroupBases["grp"]; ok {
		t.Fatal("deleted group kept its base")
	}
}
