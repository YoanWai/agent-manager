package review

import (
	"maps"

	git "github.com/YoanWai/agent-manager/internal/git/value"
)

// StoredScope reads a scope saved as a review preference; anything unknown
// opens on uncommitted changes.
func StoredScope(stored string) git.Scope {
	switch stored {
	case "branch":
		return git.ScopeBranch
	case "last_commit", "commit":
		return git.ScopeCommit
	case "staged":
		return git.ScopeStaged
	default:
		return git.ScopeUncommitted
	}
}

// ScopeName is the name StoredScope reads scope back from.
func ScopeName(scope git.Scope) string {
	switch scope {
	case git.ScopeBranch:
		return "branch"
	case git.ScopeCommit:
		return "commit"
	case git.ScopeStaged:
		return "staged"
	default:
		return "uncommitted"
	}
}

func (m *Model) CycleScope() (LoadRequest, bool) {
	if !m.active || m.target.ID == "" {
		return LoadRequest{}, false
	}
	m.scope = m.scope.Next()
	if m.scope == git.ScopeCommit {
		m.commitPin = CommitPin{Unreviewed: true}
	}
	return m.reloadScope(), true
}

func (m *Model) SelectRepo(root string) (LoadRequest, bool) {
	if !m.active || m.target.ID == "" || root == "" {
		return LoadRequest{}, false
	}
	m.repoSel = root
	m.resetCommitScope(CommitPin{Unreviewed: true})
	m.gen++
	m.loading = true
	m.errText = ""
	m.set = zeroDiffSet()
	m.fileIdx = 0
	m.scroll = 0
	m.cursorLine = 0
	m.fileLoading = make(map[int]bool)
	m.reanchor = nil
	return LoadRequest{Target: m.target, Scope: m.scope, Generation: m.gen, RepoWanted: root, Resolve: true, Restored: maps.Clone(m.stateLoaded), Commit: m.commitLoad()}, true
}

func (m *Model) SelectBase(ref string) (LoadRequest, bool) {
	if !m.active || m.target.ID == "" {
		return LoadRequest{}, false
	}
	// The commit scope stays: the base bounds its commit list.
	if m.scope != git.ScopeCommit {
		m.scope = scopeBranch()
	}
	m.gen++
	m.loading = true
	m.errText = ""
	m.set = zeroDiffSet()
	m.fileIdx = 0
	m.scroll = 0
	m.cursorLine = 0
	m.fileLoading = make(map[int]bool)
	m.reanchor = nil
	return LoadRequest{
		Target: m.target, Scope: m.scope, Generation: m.gen,
		RepoWanted: m.repoSel, RepoRoot: m.repoSel,
		RepoRoots: append([]string(nil), m.repoRoots...), Resolve: m.repoSel == "" || len(m.repoRoots) == 0,
		Restored: maps.Clone(m.stateLoaded), BaseOverride: &ref, Commit: m.commitLoad(),
	}, true
}

func (m *Model) RefreshRequest() (ProbeRequest, bool) {
	if !m.active || m.loading || m.set.Repo.Root == "" || m.annotationOpen || m.sendConfirm {
		return ProbeRequest{}, false
	}
	m.probeTick++
	if m.probeTick%2 != 0 {
		return ProbeRequest{}, false
	}
	return ProbeRequest{Target: m.target, Scope: m.scope, RepoSelected: m.repoSel, GitRoot: m.set.Repo.Root, Commit: m.set.Commit}, true
}

func (m *Model) ApplyProbe(result ProbeResult) (LoadRequest, bool) {
	if !m.active || result.TargetID != m.target.ID || result.Scope != m.scope || result.RepoSelected != m.repoSel {
		return LoadRequest{}, false
	}
	if m.loading || result.Fingerprint == 0 || result.Fingerprint == m.fingerprint {
		return LoadRequest{}, false
	}
	m.gen++
	m.loading = true
	return LoadRequest{Target: m.target, Scope: m.scope, Generation: m.gen, RepoWanted: m.repoSel, Refresh: true, Resolve: true, Restored: maps.Clone(m.stateLoaded), Commit: m.commitLoad()}, true
}
