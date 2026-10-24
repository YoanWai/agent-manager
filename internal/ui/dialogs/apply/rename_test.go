package apply

import "testing"

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
