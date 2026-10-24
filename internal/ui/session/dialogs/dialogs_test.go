package dialogs

import (
	uilifecycle "github.com/YoanWai/agent-manager/internal/ui/session/lifecycle"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
)

// view is the decisions' reads over rows the test holds, with the live
// projection as a set of ids.
type view struct {
	sessions []store.Session
	listed   []store.Session
	groups   []string
	archived bool
	live     map[string]bool
}

func (v view) Sessions() []store.Session { return v.sessions }
func (v view) Listed() []store.Session   { return v.listed }
func (v view) Groups() []string          { return v.groups }
func (v view) ArchivedView() bool        { return v.archived }
func (v view) Live(id string) bool       { return v.live[id] }

func row(sess store.Session) Row { return Row{Kind: SessionRow, Group: sess.Group, Sess: sess} }

func TestKillTargetsALiveRowAndTheShellsUnderIt(t *testing.T) {
	t.Parallel()
	parent := store.Session{ID: "a", Name: "agent", Status: status.Working}
	child := store.Session{ID: "b", Name: "shell", ParentID: "a", Tool: "terminal"}
	v := view{sessions: []store.Session{parent, child}, live: map[string]bool{"a": true, "b": true}}
	intent := Kill(v, row(parent))
	if intent.Notice != "" || intent.Card.Action == "" {
		t.Fatalf("intent = %+v, want a kill card", intent)
	}
	if len(intent.Card.Sessions) != 2 {
		t.Fatalf("kill covers %d rows, want the agent and its shell", len(intent.Card.Sessions))
	}
	if intent.Card.Selection.RootID != "a" || intent.Card.Selection.Kind != uilifecycle.SelectSession {
		t.Fatalf("selection = %+v, want the parent row", intent.Card.Selection)
	}
}

func TestKillRefusesARowWithNoLiveWindow(t *testing.T) {
	t.Parallel()
	dead := store.Session{ID: "a", Name: "ghost", Status: status.Dead}
	intent := Kill(view{sessions: []store.Session{dead}}, row(dead))
	if !strings.Contains(intent.Notice, "already dead") {
		t.Fatalf("notice = %q, want a refusal", intent.Notice)
	}
}

func TestArchiveSkipsTheArchivedViewAndRootRow(t *testing.T) {
	t.Parallel()
	sess := store.Session{ID: "a", Name: "agent"}
	if intent := Archive(view{archived: true, sessions: []store.Session{sess}}, row(sess)); !intent.Nothing() {
		t.Fatalf("archive in the archived view = %+v, want nothing", intent)
	}
	root := Row{Kind: RootRow}
	if intent := Archive(view{}, root); intent.Notice == "" {
		t.Fatal("archive on the root row should refuse")
	}
}

func TestDeleteOfAGroupCountsItsSubgroupsAndRows(t *testing.T) {
	t.Parallel()
	sessions := []store.Session{
		{ID: "a", Group: "work"},
		{ID: "b", Group: "work/api"},
		{ID: "c", Group: "other"},
	}
	v := view{sessions: sessions, groups: []string{"work", "work/api"}}
	intent := Delete(v, Row{Kind: GroupRow, Group: "work"})
	if len(intent.Card.Sessions) != 2 {
		t.Fatalf("group delete covers %d rows, want the subtree only", len(intent.Card.Sessions))
	}
	if !strings.Contains(intent.Card.Label, "1 subgroups, 2 sessions") {
		t.Fatalf("label = %q", intent.Card.Label)
	}
}

func TestReviveOfSeveralDeadRowsAsksFirst(t *testing.T) {
	t.Parallel()
	dead := []store.Session{
		{ID: "a", Name: "one", Status: status.Dead},
		{ID: "b", Name: "two", Status: status.Dead},
	}
	intent := ReviveAll(dead)
	if !intent.Card.Batch || len(intent.Card.Sessions) != 2 {
		t.Fatalf("intent = %+v, want one batch card over both rows", intent)
	}
	if intent.Run {
		t.Fatal("a batch must not run before its card is answered")
	}
}

func TestReviveOfOneDeadRowRunsWithoutACard(t *testing.T) {
	t.Parallel()
	dead := store.Session{ID: "a", Name: "one", Status: status.Dead}
	intent := ReviveAll([]store.Session{dead})
	// The batch still refuses a row that came back to life meanwhile.
	if !intent.Run || intent.AllowLive {
		t.Fatalf("intent = %+v, want the run itself, refusing live rows", intent)
	}
	if len(intent.Card.Sessions) != 1 || intent.Card.Sessions[0].ID != "a" {
		t.Fatalf("run covers %+v, want the one dead row", intent.Card.Sessions)
	}
}

func TestCancelEndNeedsASessionAskingForIt(t *testing.T) {
	t.Parallel()
	if _, _, ok := CancelEnd(Row{Kind: SessionRow, Sess: store.Session{ID: "a"}}, true); ok {
		t.Fatal("a session that asked for nothing has nothing to withdraw")
	}
	asking := store.Session{ID: "a", Name: "agent", AfterTurn: "archive"}
	id, name, ok := CancelEnd(Row{Kind: SessionRow, Sess: asking}, true)
	if !ok || id != "a" || name != "agent" {
		t.Fatalf("cancel end = %q %q %v", id, name, ok)
	}
	if _, _, ok := CancelEnd(Row{Kind: GroupRow}, true); ok {
		t.Fatal("a group row never carries an after-turn request")
	}
}
