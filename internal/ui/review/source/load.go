package source

import (
	"cmp"
	"path/filepath"
	"sync"

	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/store"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	tea "github.com/charmbracelet/bubbletea"
)

type Git interface {
	ResolveRepos(cwd string) ([]string, error)
	IsRepoRoot(dir string) bool
	BranchBase(root, override string) (ref, describe string, err error)
	Fingerprint(root string, scope git.Scope, baseRef string) (uint64, error)
	Worktrees(root string) ([]git.Worktree, error)
	BuildSet(root string, scope git.Scope, baseOverride, commit string) (diff.Set, error)
	LoadFile(set diff.Set, index int) diff.FileDiff
	Commits(root, override string) (commits []git.Commit, describe string, history bool, err error)
	Rewrites(root string, olds []string, commits []git.Commit) (map[string]string, error)
}

type SystemGit struct{ Driver *git.Driver }

func (g SystemGit) ResolveRepos(cwd string) ([]string, error) { return g.Driver.ResolveRepos(cwd) }

func (g SystemGit) IsRepoRoot(dir string) bool { return g.Driver.IsRepoRoot(dir) }

func (g SystemGit) BranchBase(root, override string) (string, string, error) {
	return g.Driver.BranchBase(root, override)
}

func (g SystemGit) Fingerprint(root string, scope git.Scope, baseRef string) (uint64, error) {
	return g.Driver.Fingerprint(root, scope, baseRef)
}

func (g SystemGit) Worktrees(root string) ([]git.Worktree, error) { return g.Driver.Worktrees(root) }

func (g SystemGit) BuildSet(root string, scope git.Scope, baseOverride, commit string) (diff.Set, error) {
	return diff.BuildSet(g.Driver, root, scope, baseOverride, commit)
}

func (g SystemGit) LoadFile(set diff.Set, index int) diff.FileDiff {
	return diff.LoadFile(g.Driver, set, index)
}

func (g SystemGit) Commits(root, override string) ([]git.Commit, string, bool, error) {
	return g.Driver.Commits(root, override)
}

func (g SystemGit) Rewrites(root string, olds []string, commits []git.Commit) (map[string]string, error) {
	return g.Driver.Rewrites(root, olds, commits)
}

// NormalizeRequest is the stored state a load found in an older shape, so the
// root can persist it through the effect lane after the load lands.
type NormalizeRequest struct {
	TargetID string
	RepoRoot string
	State    uireview.SavedState
}

// LoadMsg carries one load result and, when the stored state needed
// normalization, the captured normalized state to persist through the effect
// lane so the write orders with every other Review mutation. The load itself
// stays concurrent; only its persistence joins the lane.
type LoadMsg struct {
	Result    uireview.LoadResult
	Normalize *NormalizeRequest
}

// LoadCmd resolves the repos, reads the stored state and builds the diff set.
// groupBase is read by the root, off the update path, so the request a load
// answers keeps the base its target had when the load was accepted.
func LoadCmd(g Git, st Store, req uireview.LoadRequest, groupBase string) tea.Cmd {
	return func() tea.Msg {
		result := uireview.LoadResult{TargetID: req.Target.ID, Scope: req.Scope, Generation: req.Generation, Refresh: req.Refresh}
		roots := append([]string(nil), req.RepoRoots...)
		root := req.RepoRoot
		if req.Resolve {
			var err error
			roots, err = g.ResolveRepos(req.Target.Cwd)
			if err != nil {
				result.Err = err
				return LoadMsg{Result: result}
			}
			idx, found := 0, false
			for i := range roots {
				if roots[i] == req.RepoWanted {
					idx, found = i, true
					break
				}
			}
			if req.RepoWanted != "" && !found {
				if g.IsRepoRoot(req.RepoWanted) {
					roots = append(roots, req.RepoWanted)
					idx = len(roots) - 1
				} else {
					result.MissingRepo = req.RepoWanted
				}
			}
			root = roots[idx]
		}
		result.RepoRoots, result.RepoRoot = roots, root
		var normalize *NormalizeRequest
		key := req.Target.ID + "\x00" + root
		if !req.Restored[key] {
			stored, err := st.ReviewState(req.Target.ID, root)
			if err != nil {
				result.SavedErr = err
			} else {
				state := StateFromStore(stored)
				normalized, changed := uireview.NormalizeSavedState(state)
				if changed {
					// Persistence joins the effect lane so the merge orders
					// with every other Review mutation; the in-memory value
					// uses the normalized shape immediately. A failed persist
					// is retried by the next load: normalization is a pure
					// idempotent function of the stored shape.
					state = normalized
					normalize = &NormalizeRequest{TargetID: req.Target.ID, RepoRoot: root, State: normalized}
				}
				result.Saved, result.SavedLoaded = state, true
			}
		}
		override := ""
		if req.BaseOverride != nil {
			override = *req.BaseOverride
		} else {
			var err error
			override, err = st.ReviewBase(req.Target.ID, Canonical(root))
			if err != nil {
				result.Err = err
				return LoadMsg{Result: result}
			}
		}
		override = cmp.Or(override, groupBase)
		commit := ""
		if req.Scope == git.ScopeCommit {
			var err error
			if commit, err = chooseReviewCommit(g, root, override, req.Commit, &result); err != nil {
				result.Err = err
				return LoadMsg{Result: result}
			}
		}
		set, err := g.BuildSet(root, req.Scope, override, commit)
		result.Set, result.Err = set, err
		if err != nil {
			return LoadMsg{Result: result}
		}
		ref := set.BaseRef
		switch {
		case req.Scope == git.ScopeCommit:
			ref = set.Commit
		case req.Scope == git.ScopeBranch && ref == "":
			ref, _, _ = g.BranchBase(set.Repo.Root, override)
		}
		var worktrees []git.Worktree
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); result.Fingerprint, _ = g.Fingerprint(set.Repo.Root, req.Scope, ref) }()
		go func() { defer wg.Done(); worktrees, _ = g.Worktrees(set.Repo.Root) }()
		wg.Wait()
		result.Worktrees = make([]uireview.Worktree, len(worktrees))
		for i, wt := range worktrees {
			result.Worktrees[i] = uireview.Worktree{Root: wt.Root, Branch: wt.Branch}
		}
		return LoadMsg{Result: result, Normalize: normalize}
	}
}

// chooseReviewCommit lists the commit scope's commits into result and
// returns the one to show; "" follows HEAD. Review state keyed by a commit
// HEAD no longer reaches is mapped to its replacement in result.Rewrites.
func chooseReviewCommit(g Git, root, override string, load uireview.CommitLoad, result *uireview.LoadResult) (string, error) {
	commits, target, history, err := g.Commits(root, override)
	if err != nil {
		return "", err
	}
	result.Commits, result.CommitTarget = commits, target
	if len(commits) == 0 {
		return "", nil
	}
	var saved *uireview.SavedState
	if result.SavedLoaded {
		saved = &result.Saved
	}
	if pin, ok := load.Resume[result.TargetID+"\x00"+root]; ok {
		load.Pin = pin
	}
	// Recent history on the target holds no commit the branch made, so
	// nothing there waits on review.
	if history && load.Pin.Unreviewed {
		load.Pin = uireview.CommitPin{}
	}
	result.Head = commits[0].SHA
	if result.Head != load.Head {
		result.Rewrites, result.RewriteErr = g.Rewrites(root, load.RewriteCandidates(saved), commits)
	}
	sha, pinned, notice := uireview.ChooseCommit(commits, result.Rewrites, load, saved)
	result.Pinned, result.CommitNotice = pinned, notice
	return sha, nil
}

func FileCmd(g Git, req uireview.FileRequest) tea.Cmd {
	return func() tea.Msg {
		return fileResult(g, req)
	}
}

type FilesResult []uireview.FileResult

func FilesCmd(g Git, requests []uireview.FileRequest) tea.Cmd {
	if len(requests) == 0 {
		return nil
	}
	return func() tea.Msg {
		results := make(FilesResult, 0, len(requests))
		for _, req := range requests {
			results = append(results, fileResult(g, req))
		}
		return results
	}
}

func fileResult(g Git, req uireview.FileRequest) uireview.FileResult {
	return uireview.FileResult{
		TargetID: req.TargetID, Scope: req.Scope, Generation: req.Generation,
		RepoRoot: req.RepoRoot, Index: req.Index, Path: req.Path,
		File: g.LoadFile(req.Set, 0),
	}
}

// ProbeCmd fingerprints the review's current repo so a refresh knows whether
// the diff it shows is still the one on disk.
func ProbeCmd(g Git, st Store, req uireview.ProbeRequest, groupBase string) tea.Cmd {
	return func() tea.Msg {
		result := uireview.ProbeResult{TargetID: req.Target.ID, Scope: req.Scope, RepoSelected: req.RepoSelected}
		override, err := st.ReviewBase(req.Target.ID, Canonical(req.RepoSelected))
		if err != nil {
			return result
		}
		ref := ""
		switch req.Scope {
		case git.ScopeBranch:
			ref, _, _ = g.BranchBase(req.GitRoot, cmp.Or(override, groupBase))
		case git.ScopeCommit:
			ref = req.Commit
		}
		result.Fingerprint, _ = g.Fingerprint(req.GitRoot, req.Scope, ref)
		return result
	}
}

// HighlightCmd prepares the syntax text for one file without blocking Update.
func HighlightCmd(req uireview.HighlightRequest) tea.Cmd {
	return func() tea.Msg {
		return uireview.HighlightResult{Key: req.Key, Highlight: uireview.HighlightFile(&req.File)}
	}
}

// Canonical resolves a path's symlinks, keeping the input when the resolution
// fails so a store read still names the same repo.
func Canonical(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return resolved
}

// Target is the immutable part of a session a Review needs.
func Target(sess store.Session) uireview.Target {
	return uireview.Target{ID: sess.ID, Name: sess.Name, Tool: sess.Tool, Cwd: sess.Cwd}
}
