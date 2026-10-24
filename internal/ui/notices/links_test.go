package notices

import (
	"strings"
	"testing"
)

func TestBugReportURLPrefillsVersion(t *testing.T) {
	got := BugReportURL("v0.2.0")
	if !strings.HasPrefix(got, "https://github.com/YoanWai/agent-manager/issues/new") {
		t.Fatalf("bug report should open the new-issue page, got %q", got)
	}
	if !strings.Contains(got, "template=bug_report.yml") {
		t.Fatalf("bug report should use the bug form, got %q", got)
	}
	if !strings.Contains(got, "version=v0.2.0") {
		t.Fatalf("issue URL should carry the version, got %q", got)
	}
}

func TestFeatureRequestURLUsesFeatureTemplate(t *testing.T) {
	got := FeatureRequestURL()
	if !strings.Contains(got, "template=feature_request.yml") {
		t.Fatalf("feature request should use the feature form, got %q", got)
	}
	if strings.Contains(got, "template=bug_report.yml") {
		t.Fatalf("feature request opened the bug form: %q", got)
	}
}

func TestSupportRequestURLPrefillsTheCLIFields(t *testing.T) {
	got := SupportRequestURL()
	if !strings.HasPrefix(got, RepoURL+"/issues/new?body=") {
		t.Fatalf("support request should open a prefilled issue, got %q", got)
	}
	for _, want := range []string{"CLI+name", "How+to+launch+it", "Config+and+tool+support"} {
		if !strings.Contains(got, want) {
			t.Fatalf("support request is missing %q: %q", want, got)
		}
	}
}

func TestStagedOpensOnAClearList(t *testing.T) {
	var log Log
	if got := log.Staged("feed-new", false, true); got.Open != "feed-new" || got.Hold != "" {
		t.Fatalf("a clear list should open the notice, got %+v", got)
	}
}

func TestStagedHoldsWhileTheListIsBusy(t *testing.T) {
	var log Log
	got := log.Staged("feed-new", false, false)
	if got.Open != "" || got.Hold != "feed-new" {
		t.Fatalf("a busy list should hold the notice, got %+v", got)
	}
}

func TestStagedLeavesAnOpenPanelToTheCaller(t *testing.T) {
	var log Log
	if got := log.Staged("feed-new", true, true); got != (Stage{}) {
		t.Fatalf("an open panel stages nothing, got %+v", got)
	}
}

func TestStagedIgnoresAChangeThatAddedNothing(t *testing.T) {
	var log Log
	if got := log.Staged("", false, true); got != (Stage{}) {
		t.Fatalf("no added notice stages nothing, got %+v", got)
	}
}

func TestTakeClearsTheHeldNotice(t *testing.T) {
	log := Log{Held: "feed-new"}
	if got := log.Take(); got != "feed-new" {
		t.Fatalf("Take = %q, want the held notice", got)
	}
	if log.Held != "" {
		t.Fatalf("Take left %q held", log.Held)
	}
}

func TestAddedIsTheNoticeTheChangeIntroduced(t *testing.T) {
	before := IDs([]Notice{{ID: "welcome"}})
	now := []Notice{{ID: "welcome"}, {ID: "feed-new"}}
	if got := Added(before, now); got != "feed-new" {
		t.Fatalf("Added = %q, want feed-new", got)
	}
	if got := Added(IDs(now), now); got != "" {
		t.Fatalf("a change that added nothing reported %q", got)
	}
}

func TestHoldsOnlyReportsAListedNotice(t *testing.T) {
	notices := []Notice{{ID: "feed-new"}}
	if !Holds(notices, "feed-new") {
		t.Fatal("a listed notice should be held")
	}
	if Holds(notices, "dismissed-already") {
		t.Fatal("a notice that left the list must not open")
	}
}

func TestQueuedFindsARunningOrWaitingDismissal(t *testing.T) {
	requests := []DismissRequest{{ID: "welcome"}}
	if !Queued(requests, "welcome") {
		t.Fatal("a queued dismissal should be found")
	}
	if Queued(requests, "feed-new") {
		t.Fatal("an unqueued id must not report queued")
	}
}

func TestFailedDismissalReopensOnlyBeforeTheUserMovesOn(t *testing.T) {
	request := DismissRequest{ID: "welcome", ForegroundGen: 7, Modal: true}
	stale := FailedDismissal(request, errString("disk full"), false, 8, false, true)
	if stale.Reopen != "" {
		t.Fatalf("a key pressed since accepting must not reopen the card: %+v", stale)
	}
	if stale.Restore != "welcome" || stale.Err == nil {
		t.Fatalf("the dismissal should still restore and report: %+v", stale)
	}
	fresh := FailedDismissal(request, errString("disk full"), false, 7, false, true)
	if fresh.Reopen != "welcome" {
		t.Fatalf("a clear list before any key should reopen the card: %+v", fresh)
	}
	quitting := FailedDismissal(request, errString("disk full"), true, 7, false, true)
	if quitting.Reopen != "" {
		t.Fatalf("a quitting manager must not reopen a card: %+v", quitting)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
