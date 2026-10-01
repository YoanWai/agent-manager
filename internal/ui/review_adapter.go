package ui

import (
	"os"
	"path/filepath"
	"sync"

	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/store"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	tea "github.com/charmbracelet/bubbletea"
)

// reviewLoadMsg carries one load result and, when the stored state needed
// normalization, the captured normalized state to persist through the
// effect lane so the write orders with every other Review mutation. The
// load itself stays concurrent; only its persistence joins the lane.
type reviewLoadMsg struct {
	result    uireview.LoadResult
	normalize *reviewEffectRequest
}

func reviewTarget(sess store.Session) uireview.Target {
	return uireview.Target{ID: sess.ID, Name: sess.Name, Tool: sess.Tool, Cwd: sess.Cwd}
}

func (m *Model) reviewLoadCmd(req uireview.LoadRequest) tea.Cmd {
	driver, stor := m.services.gitDrv, m.services.store
	return func() tea.Msg {
		result := uireview.LoadResult{TargetID: req.Target.ID, Scope: req.Scope, Generation: req.Generation, Refresh: req.Refresh}
		roots := append([]string(nil), req.RepoRoots...)
		root := req.RepoRoot
		if req.Resolve {
			var err error
			roots, err = driver.ResolveRepos(req.Target.Cwd)
			if err != nil {
				result.Err = err
				return reviewLoadMsg{result: result}
			}
			idx, found := 0, false
			for i := range roots {
				if roots[i] == req.RepoWanted {
					idx, found = i, true
					break
				}
			}
			if req.RepoWanted != "" && !found {
				if driver.IsRepoRoot(req.RepoWanted) {
					roots = append(roots, req.RepoWanted)
					idx = len(roots) - 1
				} else {
					result.MissingRepo = req.RepoWanted
				}
			}
			root = roots[idx]
		}
		result.RepoRoots, result.RepoRoot = roots, root
		var normalize *reviewEffectRequest
		key := req.Target.ID + "\x00" + root
		if !req.Restored[key] {
			stored, err := stor.ReviewState(req.Target.ID, root)
			if err != nil {
				result.SavedErr = err
			} else {
				state := reviewStateFromStore(stored)
				normalized, changed := uireview.NormalizeSavedState(state)
				if changed {
					// Persistence joins the effect lane so the merge
					// orders with every other Review mutation; the in-memory
					// value uses the normalized shape immediately. A failed
					// persist is retried by the next load: normalization is a
					// pure idempotent function of the stored shape.
					state = normalized
					normalize = &reviewEffectRequest{
						op:       reviewOpNormalize,
						targetID: req.Target.ID,
						repoRoot: root,
						state:    normalized,
					}
				}
				result.Saved, result.SavedLoaded = state, true
			}
		}
		override := ""
		if req.BaseOverride != nil {
			override = *req.BaseOverride
		} else {
			var err error
			override, err = stor.ReviewBase(req.Target.ID, resolveSymlinksOrSelf(root))
			if err != nil {
				result.Err = err
				return reviewLoadMsg{result: result}
			}
		}
		set, err := diff.BuildSet(driver, root, req.Scope, override)
		result.Set, result.Err = set, err
		if err != nil {
			return reviewLoadMsg{result: result}
		}
		baseRef := set.BaseRef
		if req.Scope == git.ScopeBranch && baseRef == "" {
			baseRef, _, _ = driver.BranchBase(set.Repo.Root, override)
		}
		var worktrees []git.Worktree
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); result.Fingerprint, _ = driver.Fingerprint(set.Repo.Root, req.Scope, baseRef) }()
		go func() { defer wg.Done(); worktrees, _ = driver.Worktrees(set.Repo.Root) }()
		wg.Wait()
		result.Worktrees = make([]uireview.Worktree, len(worktrees))
		for i, wt := range worktrees {
			result.Worktrees[i] = uireview.Worktree{Root: wt.Root, Branch: wt.Branch}
		}
		if normalize != nil {
			return reviewLoadMsg{result: result, normalize: normalize}
		}
		return reviewLoadMsg{result: result}
	}
}

func (m *Model) reviewFileCmd(req uireview.FileRequest) tea.Cmd {
	driver := m.services.gitDrv
	return func() tea.Msg {
		return uireview.FileResult{
			TargetID: req.TargetID, Scope: req.Scope, Generation: req.Generation,
			RepoRoot: req.RepoRoot, Index: req.Index, Path: req.Path,
			File: diff.LoadFile(driver, req.Set, 0),
		}
	}
}

func (m *Model) reviewFilesCmd(requests []uireview.FileRequest) tea.Cmd {
	if len(requests) == 0 {
		return nil
	}
	driver := m.services.gitDrv
	return func() tea.Msg {
		results := make(reviewFilesResult, 0, len(requests))
		for _, req := range requests {
			results = append(results, uireview.FileResult{
				TargetID: req.TargetID, Scope: req.Scope, Generation: req.Generation,
				RepoRoot: req.RepoRoot, Index: req.Index, Path: req.Path,
				File: diff.LoadFile(driver, req.Set, 0),
			})
		}
		return results
	}
}

type reviewFilesResult []uireview.FileResult

func (m *Model) reviewProbeCmd(req uireview.ProbeRequest) tea.Cmd {
	driver, stor := m.services.gitDrv, m.services.store
	return func() tea.Msg {
		result := uireview.ProbeResult{TargetID: req.Target.ID, Scope: req.Scope, RepoSelected: req.RepoSelected}
		override, err := stor.ReviewBase(req.Target.ID, resolveSymlinksOrSelf(req.RepoSelected))
		if err != nil {
			return result
		}
		baseRef := ""
		if req.Scope == git.ScopeBranch {
			baseRef, _, _ = driver.BranchBase(req.GitRoot, override)
		}
		result.Fingerprint, _ = driver.Fingerprint(req.GitRoot, req.Scope, baseRef)
		return result
	}
}

func (m *Model) reviewHighlightCmd(req uireview.HighlightRequest) tea.Cmd {
	return func() tea.Msg {
		return uireview.HighlightResult{Key: req.Key, Highlight: highlightFile(&req.File)}
	}
}

// reviewSaveCmd persists one captured review state through the effect
// lane; the worker owns the merge, so no Update path touches SQLite here.
// Dispatch is enqueue-only: the lane's trailing nextEffectCmd starts it.
func (m *Model) reviewSaveCmd(req uireview.SaveRequest) tea.Cmd {
	m.enqueueEffect(reviewEffectRequest{
		op:       reviewOpSave,
		targetID: req.TargetID,
		repoRoot: req.RepoRoot,
		state:    req.State,
	}, 0, false)
	return nil
}

// reviewStatusCmd reads comment statuses through the effect lane, so it
// observes every Review mutation accepted before it without a private
// write fence.
func (m *Model) reviewStatusCmd(req uireview.StatusRequest) tea.Cmd {
	if m.services.store == nil {
		return nil
	}
	m.enqueueEffect(reviewEffectRequest{
		op:         reviewOpStatus,
		targetID:   req.TargetID,
		repoRoot:   req.RepoRoot,
		generation: req.Generation,
	}, 0, false)
	return nil
}

// reviewHandleCmd marks one comment handled through the effect lane; the
// worker owns the store write and the found/ambiguous outcome.
func (m *Model) reviewHandleCmd(req uireview.HandleCommentRequest) tea.Cmd {
	m.enqueueEffect(reviewEffectRequest{
		op:        reviewOpHandle,
		targetID:  req.TargetID,
		repoRoot:  req.RepoRoot,
		commentID: req.CommentID,
		handled:   req.Handled,
		previous:  req.Previous,
	}, 0, false)
	return nil
}

// reviewSendCmd persists the round and types the prompt through the effect
// lane, so the existence preflight, state merge, text delivery, rollback,
// and ack clear run in one ordered job behind every earlier Review
// mutation.
func (m *Model) reviewSendCmd(req uireview.SendRequest) tea.Cmd {
	m.enqueueEffect(reviewEffectRequest{
		op:            reviewOpSend,
		targetID:      req.Target.ID,
		targetName:    req.Target.Name,
		repoRoot:      req.RepoRoot,
		state:         req.State,
		previousState: req.PreviousState,
		prompt:        req.Prompt,
		commentIDs:    req.CommentIDs,
		previousRound: req.PreviousRound,
		round:         req.Round,
		count:         req.Count,
	}, 0, false)
	return nil
}

func reviewFileCheckCmd(req uireview.FileCheckRequest) tea.Cmd {
	return func() tea.Msg { _, err := os.Stat(req.Path); return uireview.FileCheckResult{Request: req, Err: err} }
}

func (m *Model) reviewCommands(requests uireview.Requests) tea.Cmd {
	var cmds []tea.Cmd
	if requests.Load != nil {
		cmds = append(cmds, m.reviewLoadCmd(*requests.Load))
	}
	if len(requests.Files) > 0 {
		cmds = append(cmds, m.reviewFileCmd(requests.Files[0]))
		if len(requests.Files) > 1 {
			cmds = append(cmds, m.reviewFilesCmd(requests.Files[1:]))
		}
	}
	if requests.Highlight != nil {
		cmds = append(cmds, m.reviewHighlightCmd(*requests.Highlight))
	}
	if requests.Save != nil && m.services.store != nil {
		cmds = append(cmds, m.reviewSaveCmd(*requests.Save))
	}
	if requests.Status != nil && m.services.store != nil {
		cmds = append(cmds, m.reviewStatusCmd(*requests.Status))
	}
	if requests.Handle != nil && m.services.store != nil {
		cmds = append(cmds, m.reviewHandleCmd(*requests.Handle))
	}
	if requests.Send != nil && m.services.store != nil && m.services.tmux != nil {
		cmds = append(cmds, m.reviewSendCmd(*requests.Send))
	}
	if requests.FileCheck != nil {
		cmds = append(cmds, reviewFileCheckCmd(*requests.FileCheck))
	}
	if requests.WidgetCmd != nil {
		cmds = append(cmds, requests.WidgetCmd)
	}
	if requests.StartupTick {
		cmds = append(cmds, m.startStartupTick())
	}
	return tea.Batch(cmds...)
}

func reviewStateFromStore(state store.ReviewState) uireview.SavedState {
	out := uireview.SavedState{Reviewed: state.Reviewed, Round: uireview.Round{Number: state.Round.Number, Scope: state.Round.Scope, Fingerprint: state.Round.Fingerprint}}
	out.Comments = make([]uireview.Comment, len(state.Comments))
	for i, note := range state.Comments {
		out.Comments[i] = uireview.Comment{ID: note.ID, File: note.File, Line: note.Line, Deleted: note.Deleted, Excerpt: note.Excerpt, Text: note.Text, ContentHash: note.ContentHash, Round: note.Round, Scope: note.Scope, Point: note.Point, Resolved: note.Resolved, Outdated: note.Outdated}
	}
	return out
}

func reviewStateToStore(state uireview.SavedState) store.ReviewState {
	out := store.ReviewState{Reviewed: state.Reviewed, Round: store.ReviewRound{Number: state.Round.Number, Scope: state.Round.Scope, Fingerprint: state.Round.Fingerprint}}
	out.Comments = make([]store.ReviewComment, len(state.Comments))
	for i, note := range state.Comments {
		out.Comments[i] = store.ReviewComment{ID: note.ID, File: note.File, Line: note.Line, Deleted: note.Deleted, Excerpt: note.Excerpt, Text: note.Text, ContentHash: note.ContentHash, Round: note.Round, Scope: note.Scope, Point: note.Point, Resolved: note.Resolved, Outdated: note.Outdated}
	}
	return out
}

func reviewOpenPathError(path string, err error) string {
	if os.IsNotExist(err) {
		return "file no longer exists: " + path
	}
	return "checking file " + filepath.Clean(path) + ": " + err.Error()
}
