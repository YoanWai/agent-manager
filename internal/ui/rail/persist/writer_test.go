package persist

import (
	"errors"
	"testing"

	"github.com/YoanWai/agent-manager/internal/store"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
)

var errLabel = errors.New("status bar refused the label")

// fakeLabels records the status-bar labels a committed change refreshes.
type fakeLabels struct {
	live   map[string]bool
	labels map[string]string
	fail   error
}

func (l *fakeLabels) Exists(id string) bool { return l.live[id] }

func (l *fakeLabels) SetLabel(id, label string) error {
	if l.fail != nil {
		return l.fail
	}
	l.labels[id] = label
	return nil
}

func TestPlacementRefusedWhenTheSourceMovedUnderneath(t *testing.T) {
	st := openStore(t)
	if err := st.CreateSession(store.Session{ID: "mover", Name: "mover", Tool: "claude", Cwd: "/tmp"}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := st.CreateGroup("work", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	labels := &fakeLabels{live: map[string]bool{}, labels: map[string]string{}}
	writer := NewWriter(st, labels)

	mutation := uirail.Mutation{Kind: uirail.PlaceSession, SessionID: "mover", Group: "work"}
	stale := Precondition{Verify: true, SourceGroup: "elsewhere"}
	if _, err := writer.Apply(mutation, stale); err == nil {
		t.Fatal("placement from a stale source was accepted")
	}

	result, err := writer.Apply(mutation, Precondition{Verify: true})
	if err != nil {
		t.Fatalf("place session: %v", err)
	}
	if len(result.Sessions) != 1 || result.Sessions[0].Group != "work" {
		t.Fatalf("placement reported %v", result.Sessions)
	}
}

func TestPlacementAlreadyAtItsDestinationWritesNothing(t *testing.T) {
	st := openStore(t)
	if err := st.CreateSession(store.Session{ID: "mover", Name: "mover", Tool: "claude", Cwd: "/tmp", Group: "work"}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	labels := &fakeLabels{live: map[string]bool{"mover": true}, labels: map[string]string{}}
	result, err := NewWriter(st, labels).Apply(
		uirail.Mutation{Kind: uirail.PlaceSession, SessionID: "mover", Group: "work"},
		Precondition{Verify: true, SourceGroup: "work"},
	)
	if err != nil {
		t.Fatalf("place session: %v", err)
	}
	if len(result.Sessions) != 1 || len(labels.labels) != 0 {
		t.Fatalf("already-placed row reported %v and relabelled %v", result.Sessions, labels.labels)
	}
}

func TestGroupMoveReportsTheNewPathAndRelabelsItsPanes(t *testing.T) {
	st := openStore(t)
	if err := st.CreateGroup("work/api", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := st.CreateSession(store.Session{ID: "one", Name: "one", Tool: "claude", Cwd: "/tmp", Group: "work/api"}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	labels := &fakeLabels{live: map[string]bool{"one": true}, labels: map[string]string{}}
	result, err := NewWriter(st, labels).Apply(uirail.Mutation{Kind: uirail.MoveGroup, Path: "work/api", Group: "infra"}, Precondition{})
	if err != nil {
		t.Fatalf("move group: %v", err)
	}
	if result.NewPath != "infra/api" {
		t.Fatalf("new path = %q, want infra/api", result.NewPath)
	}
	if labels.labels["one"] != "infra/api · one" {
		t.Fatalf("label = %q, want the moved group", labels.labels["one"])
	}
}

func TestLabelFailureIsAWarningAfterTheWrite(t *testing.T) {
	st := openStore(t)
	if err := st.CreateGroup("work", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := st.CreateSession(store.Session{ID: "one", Name: "one", Tool: "claude", Cwd: "/tmp"}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	labels := &fakeLabels{live: map[string]bool{"one": true}, labels: map[string]string{}, fail: errLabel}
	result, err := NewWriter(st, labels).Apply(
		uirail.Mutation{Kind: uirail.PlaceSession, SessionID: "one", Group: "work"},
		Precondition{},
	)
	if err != nil {
		t.Fatalf("committed placement reported %v", err)
	}
	if result.Warning != errLabel {
		t.Fatalf("warning = %v, want the label failure", result.Warning)
	}
	rows, err := st.SessionsInSubtree("work")
	if err != nil || len(rows) != 1 {
		t.Fatalf("placement did not commit: %v rows, %v", len(rows), err)
	}
}
