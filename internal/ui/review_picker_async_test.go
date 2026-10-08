package ui

import (
	"errors"
	"reflect"
	"testing"

	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	tea "github.com/charmbracelet/bubbletea"
)

type scriptedReviewPickerReader struct {
	worktrees []git.Worktree
	refs      []string
	base      string
	resolved  map[string]string
	err       error
	calls     []string
}

func (r *scriptedReviewPickerReader) Worktrees(root string) ([]git.Worktree, error) {
	r.calls = append(r.calls, "worktrees:"+root)
	return append([]git.Worktree(nil), r.worktrees...), r.err
}

func (r *scriptedReviewPickerReader) BranchRefs(root string) ([]string, error) {
	r.calls = append(r.calls, "branches:"+root)
	return append([]string(nil), r.refs...), r.err
}

func (r *scriptedReviewPickerReader) ReviewBase(targetID, root string) (string, error) {
	r.calls = append(r.calls, "base:"+targetID+":"+root)
	return r.base, r.err
}

func (r *scriptedReviewPickerReader) Canonical(root string) string {
	r.calls = append(r.calls, "canonical:"+root)
	if resolved := r.resolved[root]; resolved != "" {
		return resolved
	}
	return root
}

func seedReviewPickerModel(t *testing.T, m *Model, targetID, root string) {
	t.Helper()
	m.mode = modeDiff
	m.gens.foreground = 19
	m.review = uireview.New(false)
	request := m.review.Open(uireview.Target{ID: targetID, Name: targetID, Cwd: root}, git.ScopeUncommitted, root)
	m.review.ApplyLoad(uireview.LoadResult{
		TargetID: targetID, Scope: git.ScopeUncommitted, Generation: request.Generation,
		RepoRoot: root, RepoRoots: []string{root}, Set: diff.Set{},
	})
}

func TestReviewPickerReadsRunOnlyInsideCommands(t *testing.T) {
	t.Run("branches", func(t *testing.T) {
		m := &Model{}
		seedReviewPickerModel(t, m, "s1", "/raw/repo")
		reader := &scriptedReviewPickerReader{
			worktrees: []git.Worktree{{Root: "/actual/main", Branch: "main"}, {Root: "/actual/feature", Branch: "feature"}},
			resolved:  map[string]string{"/raw/repo": "/actual/feature"},
		}

		cmd := m.openBranchPickWithReader(reader)
		if cmd == nil {
			t.Fatal("branch picker did not return a read command")
		}
		if len(reader.calls) != 0 || m.mode != modeDiff {
			t.Fatalf("open path performed I/O or changed screens: calls=%v mode=%v", reader.calls, m.mode)
		}

		msg := cmd().(reviewPickerLoadedMsg)
		wantCalls := []string{"worktrees:/raw/repo", "canonical:/raw/repo", "canonical:/actual/main", "canonical:/actual/feature"}
		if !reflect.DeepEqual(reader.calls, wantCalls) {
			t.Fatalf("worker calls = %v, want %v", reader.calls, wantCalls)
		}
		m.handleReviewPickerLoaded(msg)
		if m.mode != modeRepoPick || m.reviewNav.picker.cursor != 1 {
			t.Fatalf("branch result did not open on the canonical current worktree: mode=%v cursor=%d", m.mode, m.reviewNav.picker.cursor)
		}
	})

	t.Run("bases", func(t *testing.T) {
		m := &Model{}
		seedReviewPickerModel(t, m, "s2", "/raw/repo")
		reader := &scriptedReviewPickerReader{
			refs: []string{"main", "feature"}, base: "feature",
			resolved: map[string]string{"/raw/repo": "/actual/repo"},
		}

		cmd := m.openBasePickWithReader(reader)
		if cmd == nil {
			t.Fatal("base picker did not return a read command")
		}
		if len(reader.calls) != 0 || m.mode != modeDiff {
			t.Fatalf("open path performed I/O or changed screens: calls=%v mode=%v", reader.calls, m.mode)
		}

		msg := cmd().(reviewPickerLoadedMsg)
		wantCalls := []string{"branches:/raw/repo", "canonical:/raw/repo", "base:s2:/actual/repo"}
		if !reflect.DeepEqual(reader.calls, wantCalls) {
			t.Fatalf("worker calls = %v, want %v", reader.calls, wantCalls)
		}
		m.handleReviewPickerLoaded(msg)
		if m.mode != modeRepoPick || m.reviewNav.picker.cursor != 2 || m.reviewNav.picker.storeRoot != "/actual/repo" {
			t.Fatalf("base result = mode %v cursor %d store root %q", m.mode, m.reviewNav.picker.cursor, m.reviewNav.picker.storeRoot)
		}
	})
}

func TestReviewPickerCompletionCannotStealNewerUI(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Model)
	}{
		{"foreground", func(m *Model) { m.gens.foreground++ }},
		{"screen", func(m *Model) { m.mode = modeList }},
		{"quit", func(m *Model) { m.effects.quitting = true }},
		{"review", func(m *Model) { _, _ = m.review.SelectRepo("/other") }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := &Model{}
			seedReviewPickerModel(t, m, "s1", "/repo")
			reader := &scriptedReviewPickerReader{worktrees: []git.Worktree{{Root: "/repo", Branch: "main"}}}
			msg := m.openBranchPickWithReader(reader)().(reviewPickerLoadedMsg)
			tc.mutate(m)
			modeBefore := m.mode
			m.handleReviewPickerLoaded(msg)
			if m.mode != modeBefore || m.mode == modeRepoPick {
				t.Fatalf("stale completion stole the screen: before=%v after=%v", modeBefore, m.mode)
			}
		})
	}
}

func TestReviewPickerEnterRejectsStaleSource(t *testing.T) {
	for _, field := range []string{"generation", "target", "repo"} {
		t.Run(field, func(t *testing.T) {
			m := &Model{}
			seedReviewPickerModel(t, m, "s1", "/repo")
			m.openRepoPick()
			switch field {
			case "generation":
				m.reviewNav.picker.source.generation++
			case "target":
				m.reviewNav.picker.source.targetID = "other"
			case "repo":
				m.reviewNav.picker.source.repoRoot = "/other"
			}
			generation := m.review.Generation()
			cmd := m.reviewNav.picker.handleKey(m, tea.KeyMsg{Type: tea.KeyEnter})
			if cmd != nil || m.review.Generation() != generation {
				t.Fatal("stale picker selection started a review load")
			}
			if m.mode != modeDiff || m.errBar.text == "" {
				t.Fatalf("stale picker was not refused clearly: mode=%v err=%q", m.mode, m.errBar.text)
			}
		})
	}
}

func TestReviewBaseSavePersistsButDoesNotReloadStaleOrQuittingReview(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Model)
	}{
		{"stale", func(m *Model) { _, _ = m.review.SelectRepo("/new-repo") }},
		{"quitting", func(m *Model) { m.effects.quitting = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := buildModel(t)
			createSession(t, m, "base-save-"+tc.name, t.TempDir(), "")
			sess := reviewSessionByName(t, m, "base-save-"+tc.name)
			seedReviewPickerModel(t, m, sess.ID, "/raw/repo")
			state := m.review.Snapshot()
			m.reviewNav.picker.repoPickState = repoPickState{
				kind: pickBase, source: reviewPickerSource{
					generation: state.Generation, targetID: state.SessionID, repoRoot: state.RepoSelected,
				},
				storeRoot: "/canonical/repo",
			}

			cmd := m.selectBase("feature")
			if cmd == nil || m.effects.active == nil {
				t.Fatal("accepted base did not enter the FIFO effect lane")
			}
			tc.mutate(m)
			before := m.review.Snapshot()
			msg := cmd().(effectCompletedMsg)
			updated, follow := m.handleEffectCompleted(msg)
			*m = *updated.(*Model)
			m.drainCmds(t, follow)

			stored, err := m.services.store.ReviewBase(sess.ID, "/canonical/repo")
			if err != nil || stored != "feature" {
				t.Fatalf("captured base was not persisted: base=%q err=%v", stored, err)
			}
			after := m.review.Snapshot()
			if after.Generation != before.Generation || after.RepoSelected != before.RepoSelected || after.Scope != before.Scope {
				t.Fatalf("stale result reloaded review: before=%+v after=%+v", before, after)
			}
		})
	}
}

func TestReviewBaseSaveAllowsOnlyOnePendingRequest(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "base-one", t.TempDir(), "")
	sess := reviewSessionByName(t, m, "base-one")
	seedReviewPickerModel(t, m, sess.ID, "/raw/repo")
	state := m.review.Snapshot()
	m.reviewNav.picker.repoPickState = repoPickState{
		kind: pickBase, source: reviewPickerSource{
			generation: state.Generation, targetID: state.SessionID, repoRoot: state.RepoSelected,
		},
		storeRoot: "/canonical/repo",
	}

	cmd := m.selectBase("feature")
	if cmd == nil {
		t.Fatal("first base save was not accepted")
	}
	if second := m.selectBase("main"); second != nil {
		t.Fatal("second base save started while the first was pending")
	}
	if len(m.effects.pending) != 0 || m.effects.active == nil {
		t.Fatalf("base save jobs active=%v pending=%d, want exactly one", m.effects.active != nil, len(m.effects.pending))
	}
	if m.errBar.text == "" {
		t.Fatal("second base save was not refused clearly")
	}

	msg := cmd().(effectCompletedMsg)
	updated, follow := m.handleEffectCompleted(msg)
	*m = *updated.(*Model)
	m.drainCmds(t, follow)
	stored, err := m.services.store.ReviewBase(sess.ID, "/canonical/repo")
	if err != nil || stored != "feature" {
		t.Fatalf("first base changed after refusal: base=%q err=%v", stored, err)
	}
}

func TestReviewBaseSaveFailureSurfacesAfterNavigation(t *testing.T) {
	m := &Model{}
	seedReviewPickerModel(t, m, "s1", "/repo")
	m.mode = modeHelp
	_, _ = m.review.SelectRepo("/newer-review")

	cmd := m.applyReviewBase(reviewBaseResult{
		targetID: "s1", sourceRepo: "/repo", generation: 1,
		err: errors.New("persist base failed"),
	})
	if cmd != nil {
		t.Fatal("failed persistence started a review reload")
	}
	if m.mode != modeHelp || m.errBar.text != "persist base failed" {
		t.Fatalf("accepted persistence failure was hidden or stole navigation: mode=%v err=%q", m.mode, m.errBar.text)
	}
}

func TestReviewBaseSaveCompletesBehindReviewHelp(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	repo := gitRepoWithSecondBranch(t)
	openReviewOn(t, m, "base-help", repo)
	session, ok := m.diffSession()
	if !ok {
		t.Fatal("review session missing")
	}
	m.drainCmds(t, m.openBasePick())
	if m.mode != modeRepoPick {
		t.Fatalf("base picker did not open: mode=%v err=%q", m.mode, m.errBar.text)
	}
	cmd := m.selectBase("feature")
	if cmd == nil {
		t.Fatal("base save was not accepted")
	}
	m.mode = modeDiff
	m.openHelp()
	if m.mode != modeHelp || m.help.returnMode != modeDiff {
		t.Fatalf("review help did not open: mode=%v return=%v", m.mode, m.help.returnMode)
	}

	m.drainCmds(t, cmd)
	if m.mode != modeHelp {
		t.Fatalf("base completion stole help: mode=%v", m.mode)
	}
	if m.review.Snapshot().Scope != git.ScopeBranch {
		t.Fatalf("base completion did not continue behind help: scope=%v", m.review.Snapshot().Scope)
	}
	stored, err := m.services.store.ReviewBase(session.ID, m.reviewNav.picker.storeRoot)
	if err != nil || stored != "feature" {
		t.Fatalf("stored base=%q err=%v", stored, err)
	}

	updated, closeCmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	*m = *updated.(*Model)
	m.drainCmds(t, closeCmd)
	if m.mode != modeDiff || m.review.Snapshot().Scope != git.ScopeBranch {
		t.Fatalf("closing help lost completed base: mode=%v scope=%v", m.mode, m.review.Snapshot().Scope)
	}
}

func TestReviewBaseSaveCompletesBehindBranchPicker(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	repo := gitRepoWithSecondBranch(t)
	openReviewOn(t, m, "base-branch-picker", repo)
	m.drainCmds(t, m.openBasePick())
	saveCmd := m.selectBase("feature")
	if saveCmd == nil {
		t.Fatal("base save was not accepted")
	}

	m.mode = modeDiff
	branchCmd := m.openBranchPick()
	if branchCmd == nil {
		t.Fatal("branch picker read was not started")
	}
	m.handleReviewPickerLoaded(branchCmd().(reviewPickerLoadedMsg))
	if m.mode != modeRepoPick {
		t.Fatalf("branch picker did not open: mode=%v", m.mode)
	}
	oldGeneration := m.reviewNav.picker.source.generation

	m.drainCmds(t, saveCmd)
	if m.mode != modeRepoPick || m.review.Snapshot().Scope != git.ScopeBranch {
		t.Fatalf("base completion did not reconcile behind picker: mode=%v scope=%v", m.mode, m.review.Snapshot().Scope)
	}
	if m.review.Generation() == oldGeneration {
		t.Fatal("base completion did not invalidate the older picker generation")
	}
	generation := m.review.Generation()
	updated, enterCmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	if enterCmd != nil {
		m.drainCmds(t, enterCmd)
	}
	if m.review.Generation() != generation || m.errBar.text == "" {
		t.Fatalf("older branch picker remained actionable: generation=%d err=%q", m.review.Generation(), m.errBar.text)
	}
}

func TestReviewSetBaseWorkerUsesCapturedValues(t *testing.T) {
	st := openReviewTestStore(t)
	result, err := runReviewWithWriter(reviewEffectRequest{
		op: reviewOpSetBase, targetID: "s1", repoRoot: "/captured/repo", baseRef: "feature",
		generation: 7, sourceRepo: "/raw/repo",
	}, st, &scriptedReviewWriter{})
	if err != nil {
		t.Fatal(err)
	}
	got := result.(reviewEffectResult).base
	if got.targetID != "s1" || got.repoRoot != "/captured/repo" || got.sourceRepo != "/raw/repo" || got.ref != "feature" || got.generation != 7 || got.err != nil {
		t.Fatalf("base result lost captured identity: %+v", got)
	}
	stored, err := st.ReviewBase("s1", "/captured/repo")
	if err != nil || stored != "feature" {
		t.Fatalf("worker persisted base=%q err=%v", stored, err)
	}
}
