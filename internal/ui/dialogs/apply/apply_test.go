package apply

import (
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/store"
)

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestExpandCommandQuotesPlaceholders(t *testing.T) {
	got := ExpandCommand("tool --fork {id} --new {new_id} --name {name} --file {session_file}", "source", "new", "Sam's fork", "/store/session.jsonl")
	want := "tool --fork 'source' --new 'new' --name 'Sam'\\''s fork' --file '/store/session.jsonl'"
	if runtime.GOOS == "windows" {
		// The pane shell is PowerShell, which doubles a quote inside a
		// single-quoted string.
		want = "tool --fork 'source' --new 'new' --name 'Sam''s fork' --file '/store/session.jsonl'"
	}
	if got != want {
		t.Fatalf("fork command = %q, want %q", got, want)
	}
}

func TestValidateForkSourceRefusesWhatTheToolCannotDo(t *testing.T) {
	source := store.Session{Name: "source", AgentSessionID: "conversation"}
	for name, tc := range map[string]struct {
		tool config.Tool
		want string
	}{
		"shell":            {config.Tool{Shell: true}, "is a shell"},
		"no command":       {config.Tool{}, "has no fork_command"},
		"no placeholder":   {config.Tool{ForkCommand: "tool --fork latest"}, "must reference the source"},
		"no conversation":  {config.Tool{ForkCommand: "tool {id}"}, "no captured conversation id"},
		"session file":     {config.Tool{ForkCommand: "tool {session_file}"}, `session_store = "gemini"`},
		"a working source": {config.Tool{ForkCommand: "tool {id}"}, ""},
	} {
		subject := source
		if name == "no conversation" {
			subject.AgentSessionID = ""
		}
		err := ValidateForkSource("tool", tc.tool, subject)
		switch {
		case tc.want == "" && err != nil:
			t.Errorf("%s: unexpected error %v", name, err)
		case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
			t.Errorf("%s: error = %v, want it to say %q", name, err, tc.want)
		}
	}
}

func TestRenameCommitsGroupStagesInOrder(t *testing.T) {
	st := openStore(t)
	if err := st.CreateGroup("old/inner", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	dir := t.TempDir()
	id := "kid"
	if err := st.CreateSession(store.Session{ID: id, Name: "kid", Tool: "claude", Cwd: dir, Group: "old/inner"}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	sources := Sources{Store: st, Panes: noPanes{}}
	outcome, err := Rename(sources, fixedProbe{dir: dir, ok: true}, Request{
		Kind:     Group,
		OldGroup: "old",
		NewGroup: "fresh",
		Dir:      dir,
		Worktree: "on",
		Name:     "fresh",
	})
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if !outcome.GroupRenamed || !outcome.PathSet || !outcome.WorktreeSet {
		t.Fatalf("stages not all committed: %+v", outcome)
	}
	if len(outcome.Sessions) != 1 || outcome.Sessions[0].Group != "fresh/inner" {
		t.Fatalf("relabel set wrong: %+v", outcome.Sessions)
	}
	groups, _ := st.Groups()
	seen := map[string]bool{}
	for _, group := range groups {
		seen[group.Name] = true
	}
	if !seen["fresh"] || !seen["fresh/inner"] {
		t.Fatalf("store not renamed: %v", groups)
	}
}

func TestRenameRefusesAReplacedRow(t *testing.T) {
	st := openStore(t)
	id := "kept"
	if err := st.CreateSession(store.Session{ID: id, Name: "before", Tool: "claude", Cwd: t.TempDir()}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	captured, err := st.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	captured.TmuxSocket = "elsewhere"

	_, err = Rename(Sources{Store: st, Panes: noPanes{}}, fixedProbe{}, Request{
		Kind:   Session,
		SessID: id,
		Sess:   captured,
		Name:   "after",
	})
	if err == nil || !strings.Contains(err.Error(), "was replaced") {
		t.Fatalf("replaced target error = %v", err)
	}
	stored, err := st.Get(id)
	if err != nil || stored.Name != "before" {
		t.Fatalf("replaced target renamed to %q, %v", stored.Name, err)
	}
}

func TestForkRefusesASourceThatRestartedWhileQueued(t *testing.T) {
	st := openStore(t)
	if err := st.CreateSession(store.Session{ID: "source", Name: "source", Tool: "claude", Cwd: t.TempDir()}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	source, err := st.Get("source")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetAgentSessionID("source", "restarted-conversation"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetAgentLaunchedAt("source", time.Now()); err != nil {
		t.Fatal(err)
	}

	_, err = Fork(Sources{Store: st, Config: config.Config{Tools: map[string]config.Tool{"claude": {Command: "cat", ForkCommand: "tool {id}"}}}}, fixedProbe{}, ForkRequest{
		Source: source,
		Name:   "child",
	})
	if err == nil || !strings.Contains(err.Error(), "changed while the fork was in the queue") {
		t.Fatalf("restarted source error = %v", err)
	}
}

func TestForkRefusesAWorkingDirectoryThatDied(t *testing.T) {
	st := openStore(t)
	source := store.Session{ID: "source", Name: "source", Tool: "claude", Cwd: "/nowhere/at/all", AgentSessionID: "conversation"}
	if err := st.CreateSession(source); err != nil {
		t.Fatalf("create session: %v", err)
	}

	_, err := Fork(Sources{Store: st, Config: config.Config{Tools: map[string]config.Tool{"claude": {Command: "cat", ForkCommand: "tool {id}"}}}}, fixedProbe{}, ForkRequest{
		Source: source,
		Name:   "child",
	})
	if err == nil || !strings.Contains(err.Error(), "working directory no longer exists") {
		t.Fatalf("dead directory error = %v", err)
	}
}

func TestForkMintsTheChildItFrozenRequestNames(t *testing.T) {
	request := ForkRequest{Source: store.Session{ID: "source", PendingInputs: []string{"first"}}}
	request.MintChildID()
	if request.ChildID == "" {
		t.Fatal("minted fork has no child id")
	}
	copied := request.Copy()
	request.Source.PendingInputs[0] = "changed"
	if copied.Source.PendingInputs[0] != "first" {
		t.Fatal("copy retained mutable source aliases")
	}
}

// noPanes stands for a manager whose panes are gone.
type noPanes struct{}

func (noPanes) Exists(string) bool            { return false }
func (noPanes) SetLabel(string, string) error { return errors.New("no pane") }

// fixedProbe answers the directory questions with one known path.
type fixedProbe struct {
	dir string
	ok  bool
}

func (p fixedProbe) Resolve(string, []string) (string, bool) { return p.dir, p.ok }

func (p fixedProbe) ResolveDir(string) bool { return p.ok }
