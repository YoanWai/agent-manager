package review

func (m *Model) CycleScope() (LoadRequest, bool) {
	if !m.active || m.target.ID == "" {
		return LoadRequest{}, false
	}
	m.scope = m.scope.Next()
	m.gen++
	m.loading = true
	m.errText = ""
	m.set = zeroDiffSet()
	m.fileIdx = 0
	m.scroll = 0
	m.cursorLine = 0
	m.fileLoading = make(map[int]bool)
	m.reanchor = nil
	request := LoadRequest{
		Target: m.target, Scope: m.scope, Generation: m.gen,
		RepoWanted: m.repoSel, RepoRoot: m.repoSel,
		RepoRoots: append([]string(nil), m.repoRoots...),
		Resolve:   m.repoSel == "" || len(m.repoRoots) == 0,
	}
	return request, true
}

func (m *Model) SelectRepo(root string) (LoadRequest, bool) {
	if !m.active || m.target.ID == "" || root == "" {
		return LoadRequest{}, false
	}
	m.repoSel = root
	m.gen++
	m.loading = true
	m.errText = ""
	m.set = zeroDiffSet()
	m.fileIdx = 0
	m.scroll = 0
	m.cursorLine = 0
	m.fileLoading = make(map[int]bool)
	m.reanchor = nil
	return LoadRequest{Target: m.target, Scope: m.scope, Generation: m.gen, RepoWanted: root, Resolve: true}, true
}

func (m *Model) SelectBase() (LoadRequest, bool) {
	if !m.active || m.target.ID == "" {
		return LoadRequest{}, false
	}
	m.scope = scopeBranch()
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
	return ProbeRequest{Target: m.target, Scope: m.scope, RepoSelected: m.repoSel, GitRoot: m.set.Repo.Root}, true
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
	return LoadRequest{Target: m.target, Scope: m.scope, Generation: m.gen, RepoWanted: m.repoSel, Refresh: true, Resolve: true}, true
}
