// Package launch is the runtime behind the New Session form and the quick
// bar: the spawn and group creation they run through the lifecycle service,
// the catalog and worktree answers they read, the base ref they branch from,
// and the ledger a kept answer lives in between opens. The surfaces in
// package spawn paint it; the root keeps what needs a surface on screen.
package launch

import "time"

// State is what the form and the quick bar remember across opens: what each
// CLI answered, which directories can host a worktree, what the last spawn
// picked, and each repo's branches and last fetch. None of it is persisted;
// a CLI upgrade or a fresh manager run asks again.
type State struct {
	WorktreeRepos map[string]RepoAnswer
	BaseRefs      map[string]BaseRefsAnswer
	BaseFetches   map[BaseFetchKey]BaseFetch
	Catalogs      map[string]*CatalogState
	// LastSpawnTool and LastSpawnWorktree are the CLI and the worktree
	// toggle the last spawn used, so the next form opens on them.
	LastSpawnTool     string
	LastSpawnWorktree bool
}

// CachedWorktree is the last repo verdict observed for dir, kept for paint
// and the effective toggle until a probe replaces it.
func (s *State) CachedWorktree(dir string) (bool, bool) {
	answer, ok := s.WorktreeRepos[dir]
	return answer.Capable, ok
}

// FreshWorktree is that verdict only while it is recent enough for an
// explicit toggle to act on.
func (s *State) FreshWorktree(dir string, now time.Time) (bool, bool) {
	answer, ok := s.WorktreeRepos[dir]
	if !ok || now.Sub(answer.At) >= WorktreeLookupTTL {
		return false, false
	}
	return answer.Capable, true
}

func (s *State) RememberWorktree(dir string, capable bool, now time.Time) {
	if s.WorktreeRepos == nil {
		s.WorktreeRepos = map[string]RepoAnswer{}
	}
	s.WorktreeRepos[dir] = RepoAnswer{Capable: capable, At: now}
}

// ForgetWorktree drops the memo so the next look asks git again. Opening
// the form or the quick bar calls it.
func (s *State) ForgetWorktree() { s.WorktreeRepos = nil }

func (s *State) RememberSpawn(tool string, worktree bool) {
	s.LastSpawnTool = tool
	s.LastSpawnWorktree = worktree
}
