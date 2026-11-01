package apply

import (
	"errors"
	"testing"

	"github.com/YoanWai/agent-manager/internal/store"
)

// realDirs is the probe a rename needs: a pass-through default path and the
// plain directory test, without the manager's git-backed preflight.
type realDirs struct{}

func (realDirs) Resolve(raw string, fallbacks []string) (string, bool) {
	if raw != "" {
		return raw, true
	}
	if len(fallbacks) > 0 {
		return fallbacks[0], true
	}
	return "", false
}

func (realDirs) ResolveDir(string) bool { return true }

// The worker commits the durable stages in order, and the outcome says
// exactly which stages landed.
func TestRenameWorkerCommitsStagesInOrder(t *testing.T) {
	t.Parallel()
	st := openStore(t)
	dir := t.TempDir()
	if err := st.CreateGroup("old/inner", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	result, err := Rename(Sources{Store: st}, realDirs{}, Request{
		Kind:     Group,
		OldGroup: "old",
		NewGroup: "fresh",
		Dir:      dir,
		Worktree: "on",
		Name:     "fresh",
	})
	if err != nil {
		t.Fatalf("worker: %v", err)
	}
	if !result.GroupRenamed || !result.PathSet || !result.WorktreeSet || !result.BaseSet {
		t.Fatalf("stages not all committed: %+v", result)
	}
	groups, _ := st.Groups()
	seen := map[string]bool{}
	for _, g := range groups {
		seen[g.Name] = true
	}
	if !seen["fresh"] || !seen["fresh/inner"] {
		t.Fatalf("store not renamed: %v", groups)
	}
}

// Retyping a row into the browser checks what a browser tab needs before
// anything is stored, so a row never carries a tool it cannot run.
func TestRenameIntoTheBrowserChecksItsPrerequisitesFirst(t *testing.T) {
	t.Parallel()
	st := openStore(t)
	if err := st.CreateSession(store.Session{ID: "agent", Name: "before", Tool: "claude", Cwd: t.TempDir()}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	captured, err := st.Get("agent")
	if err != nil {
		t.Fatal(err)
	}
	missing := errors.New("terminal-browser is not installed")
	request := Request{Kind: Session, SessID: "agent", Sess: captured, Name: "after", Tool: "browser", CheckBrowserReady: true}

	result, err := Rename(Sources{Store: st, Panes: noPanes{}, BrowserReady: func() error { return missing }}, fixedProbe{}, request)
	if !errors.Is(err, missing) {
		t.Fatalf("rename into the browser without it = %v, want %v", err, missing)
	}
	if result.NameRenamed || result.ToolChanged {
		t.Fatalf("a refused retype committed stages: %+v", result)
	}
	stored, err := st.Get("agent")
	if err != nil || stored.Name != "before" || stored.Tool != "claude" {
		t.Fatalf("refused retype stored %q / %q, %v", stored.Name, stored.Tool, err)
	}

	result, err = Rename(Sources{Store: st, Panes: noPanes{}, BrowserReady: func() error { return nil }}, fixedProbe{}, request)
	if err != nil || !result.ToolChanged {
		t.Fatalf("rename into a ready browser = %+v, %v", result, err)
	}
}
