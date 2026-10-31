package lifecycle

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
)

func TestKillForgetsEveryRowWhoseWindowItEnded(t *testing.T) {
	t.Parallel()
	result := Result{
		Changed: []store.Session{{ID: "a", Status: "dead"}, {ID: "b", Status: "dead"}},
	}
	change := Changes(Request{Target: Target{Action: Kill}}, result, nil)
	if len(change.Forget) != 2 {
		t.Fatalf("forget = %v, want both killed rows", change.Forget)
	}
	if len(change.Statuses) != 2 {
		t.Fatalf("statuses = %d, want both rows", len(change.Statuses))
	}
}

func TestArchiveEditKeepsOnlyTheRowsTheActionWrote(t *testing.T) {
	t.Parallel()
	result := Result{Archive: sessioncmd.ArchiveResult{
		GroupChanged: true,
		GroupPath:    "work",
		Sessions: []store.Session{
			{ID: "a", Archived: true},
			{ID: "b", Archived: false},
		},
	}}
	change := Changes(Request{Target: Target{Action: Archive}}, result, nil)
	edit := change.Edit
	if edit == nil || !edit.Archive || edit.Group != "work" {
		t.Fatalf("edit = %+v, want an archive of work", edit)
	}
	if len(edit.Rows) != 1 || !edit.Rows["a"] {
		t.Fatalf("rows = %v, want only the row the store wrote", edit.Rows)
	}
	if len(change.Forget) != 1 || change.Forget[0] != "a" {
		t.Fatalf("forget = %v, want the archived row", change.Forget)
	}
}

func TestRestoreEditKeepsOnlyTheRowsThatCameBack(t *testing.T) {
	t.Parallel()
	result := Result{Archive: sessioncmd.ArchiveResult{Sessions: []store.Session{
		{ID: "a", Archived: false},
		{ID: "b", Archived: true},
	}}}
	edit := Changes(Request{Target: Target{Action: Restore}}, result, nil).Edit
	if edit == nil || edit.Archive {
		t.Fatalf("edit = %+v, want a restore", edit)
	}
	if len(edit.Rows) != 1 || !edit.Rows["a"] {
		t.Fatalf("rows = %v, want only the restored row", edit.Rows)
	}
}

func TestBatchFailureIsReportedOnlyWhenTheRunSaidNothing(t *testing.T) {
	t.Parallel()
	request := Request{Target: Target{Action: Kill, Batch: true}}
	quiet := Changes(request, Result{}, errFake{})
	if quiet.Refusal == nil || !quiet.Refusal.Batch {
		t.Fatalf("refusal = %+v, want the batch failure", quiet.Refusal)
	}
	spoken := Changes(request, Result{Notice: "killed 1, first error: boom"}, errFake{})
	if spoken.Refusal != nil {
		t.Fatalf("refusal = %+v, want none after the run named it", spoken.Refusal)
	}
}

func TestSingleRelaunchFailureCarriesTheRowsToRetry(t *testing.T) {
	t.Parallel()
	request := Request{Target: Target{Action: Revive, Sessions: []store.Session{{ID: "a"}, {ID: "b"}}}}
	change := Changes(request, Result{Failed: []store.Session{{ID: "b"}}}, errFake{})
	refusal := change.Refusal
	if refusal == nil || !refusal.ViaLaunchHint || refusal.Retry == nil {
		t.Fatalf("refusal = %+v, want a retry through the fix card", refusal)
	}
	if len(refusal.Retry.Target.Sessions) != 1 || refusal.Retry.Target.Sessions[0].ID != "b" {
		t.Fatalf("retry rows = %v, want the row that failed", refusal.Retry.Target.Sessions)
	}
}

func TestAfterTurnClearsTheRowAndNamesWhatItKept(t *testing.T) {
	t.Parallel()
	apply := AfterTurn(AfterTurnRequest{ID: "a", Name: "agent", Cancel: true}, AfterTurnResult{Cleared: true}, nil)
	if apply.ClearID != "a" || apply.Done != "agent" {
		t.Fatalf("apply = %+v, want the row cleared and named", apply)
	}
	cleared := AfterTurn(AfterTurnRequest{ID: "a"}, AfterTurnResult{Cleared: true}, nil)
	if cleared.Done != "" {
		t.Fatalf("done = %q, want silence for a clear nobody asked about", cleared.Done)
	}
}

func TestAcknowledgeKeepsAnArchivedRowsPreservedStatus(t *testing.T) {
	t.Parallel()
	if _, ok := Acknowledge(store.Session{ID: "a", Archived: true, Status: "finished"}, true); ok {
		t.Fatal("an archived row keeps its preserved status")
	}
	if _, ok := Acknowledge(store.Session{ID: "a", Status: "working"}, true); ok {
		t.Fatal("only a finished row is acknowledged")
	}
	if id, ok := Acknowledge(store.Session{ID: "a", Status: "finished"}, true); !ok || id != "a" {
		t.Fatalf("acknowledge = %q %v, want the finished row", id, ok)
	}
}

func TestSubgroupsListsOnlyThePathBelow(t *testing.T) {
	t.Parallel()
	groups := []string{"work", "work/api", "work/api/v2", "worker"}
	got := Subgroups(groups, "work")
	if len(got) != 2 || got[0] != "work/api" || got[1] != "work/api/v2" {
		t.Fatalf("subgroups = %v", got)
	}
}

func TestDropGroupsAndRemoveSessionKeepTheRest(t *testing.T) {
	t.Parallel()
	groups := DropGroups([]string{"a", "b", "c"}, []string{"b"})
	if len(groups) != 2 || groups[0] != "a" || groups[1] != "c" {
		t.Fatalf("groups = %v", groups)
	}
	rows := RemoveSession([]store.Session{{ID: "a"}, {ID: "b"}}, "a")
	if len(rows) != 1 || rows[0].ID != "b" {
		t.Fatalf("rows = %v", rows)
	}
}

type errFake struct{}

func (errFake) Error() string { return "boom" }
