package extensions

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/execution"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

func TestBacklogReadsDeliveredReplacementInsteadOfRetainingOldBadge(t *testing.T) {
	sessions := []store.Session{{ID: "target", Name: "worker"}}
	first := execution.ReadOnly(execution.Snapshot{Sessions: sessions, QueuedMessages: map[string]int{"target": 2}})
	if got := DeliveryBacklog(first, 2); !reflect.DeepEqual(got, []string{"worker has 2 queued messages"}) {
		t.Fatal(got)
	}
	next := execution.ReadOnly(execution.Snapshot{Sessions: sessions})
	if got := DeliveryBacklog(next, 2); len(got) != 0 {
		t.Fatal(got)
	}
}

type refusingArchive struct {
	calls []string
	err   error
}

func (a *refusingArchive) Archive(caller, target string, archived bool) (sessioncmd.Session, error) {
	a.calls = append(a.calls, target)
	return sessioncmd.Session{}, a.err
}

func TestArchiveExtensionReportsFailureWithoutRetryOrArchivingSelf(t *testing.T) {
	view := execution.ReadOnly(execution.Snapshot{Sessions: []store.Session{
		{ID: "self", Status: "finished"}, {ID: "working", Status: "working"},
		{ID: "already", Status: "finished", Archived: true}, {ID: "target", Status: "finished"},
	}})
	refused := errors.New("owner unavailable")
	commands := &refusingArchive{err: refused}
	outcomes := ArchiveFinished("self", view, commands)
	if len(outcomes) != 1 || !errors.Is(outcomes[0].Err, refused) || !reflect.DeepEqual(commands.calls, []string{"target"}) {
		t.Fatalf("outcomes=%+v calls=%v", outcomes, commands.calls)
	}
}

func TestViewReturnsSessionValues(t *testing.T) {
	view := execution.ReadOnly(execution.Snapshot{Sessions: []store.Session{{ID: "target", Name: "original"}}})
	for session := range view.Sessions() {
		session.Name = "changed"
	}
	for session := range view.Sessions() {
		if session.Name != "original" {
			t.Fatal(session.Name)
		}
	}
}

func TestViewKeepsObservationWhenPublisherReusesData(t *testing.T) {
	snapshot := execution.Snapshot{Sessions: []store.Session{{ID: "target", Name: "original"}}, QueuedMessages: map[string]int{"target": 2}}
	view := execution.ReadOnly(snapshot)
	snapshot.Sessions[0].Name = "changed"
	snapshot.QueuedMessages["target"] = 0
	if got := DeliveryBacklog(view, 2); !reflect.DeepEqual(got, []string{"original has 2 queued messages"}) {
		t.Fatal(got)
	}
}

func TestArchiveExtensionUsesCanonicalBackendAndNextObservation(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	driver, err := tmux.NewWithSocket(fmt.Sprintf("am-poc-ext-%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"cafe", "beef"} {
		if err := st.CreateSession(store.Session{ID: id, Name: id, Tool: "fake", Status: "finished", Cwd: dir, CreatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	backend, err := sessioncmd.BorrowBackend(sessioncmd.Runtime{
		Config: config.Config{Tools: map[string]config.Tool{"fake": {}}}, Store: st, Driver: driver,
		Hooks: hooks.NewManager(dir), Snapshot: st.SetSnapshot,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { backend.Close() })
	commands := sessioncmd.NewSessionsWithBackend(backend, sessioncmd.CLIVocabulary())
	before, err := st.ListSessions(true)
	if err != nil {
		t.Fatal(err)
	}
	outcomes := ArchiveFinished("dead", execution.ReadOnly(execution.Snapshot{Sessions: before}), commands)
	if len(outcomes) != 2 || outcomes[0].Err == nil || outcomes[1].Err == nil {
		t.Fatalf("invalid caller outcomes=%+v", outcomes)
	}
	unchanged, err := st.Get("beef")
	if err != nil || unchanged.Archived {
		t.Fatalf("refused mutation changed state: %+v err=%v", unchanged, err)
	}
	outcomes = ArchiveFinished("cafe", execution.ReadOnly(execution.Snapshot{Sessions: before}), commands)
	if len(outcomes) != 1 || outcomes[0].Err != nil {
		t.Fatalf("canonical archive outcomes=%+v", outcomes)
	}
	after, err := st.ListSessions(true)
	if err != nil {
		t.Fatal(err)
	}
	if got := ArchiveFinished("cafe", execution.ReadOnly(execution.Snapshot{Sessions: after}), commands); len(got) != 0 {
		t.Fatalf("next observation repeated archive=%+v", got)
	}
}
