package ui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/store"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	tea "github.com/charmbracelet/bubbletea"
)

// reviewAdapter is the concrete root edge for SQLite, git, tmux, and the
// filesystem. Review policy and result acceptance live in uireview.Model.
type reviewAdapter struct {
	writeDone <-chan struct{}
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
				return result
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
		key := req.Target.ID + "\x00" + root
		if !req.Restored[key] {
			stored, err := stor.ReviewState(req.Target.ID, root)
			if err != nil {
				result.SavedErr = err
			} else {
				state := reviewStateFromStore(stored)
				normalized, changed := uireview.NormalizeSavedState(state)
				if changed {
					if err := stor.MergeReviewState(req.Target.ID, root, reviewStateToStore(normalized)); err != nil {
						result.SavedErr = err
					} else {
						state = normalized
					}
				}
				if result.SavedErr == nil {
					result.Saved, result.SavedLoaded = state, true
				}
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
				return result
			}
		}
		set, err := diff.BuildSet(driver, root, req.Scope, override)
		result.Set, result.Err = set, err
		if err != nil {
			return result
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
		return result
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

func (a *reviewAdapter) chain(run func() tea.Msg) tea.Cmd {
	previous := a.writeDone
	done := make(chan struct{})
	a.writeDone = done
	return func() tea.Msg {
		if previous != nil {
			<-previous
		}
		defer close(done)
		return run()
	}
}

func (m *Model) reviewSaveCmd(req uireview.SaveRequest) tea.Cmd {
	stor := m.services.store
	return m.reviewFX.chain(func() tea.Msg {
		return uireview.SaveResult{TargetID: req.TargetID, RepoRoot: req.RepoRoot, Err: stor.MergeReviewState(req.TargetID, req.RepoRoot, reviewStateToStore(req.State))}
	})
}

func (m *Model) reviewStatusCmd(req uireview.StatusRequest) tea.Cmd {
	stor, writesDone := m.services.store, m.reviewFX.writeDone
	return func() tea.Msg {
		if writesDone != nil {
			<-writesDone
		}
		state, err := stor.ReviewState(req.TargetID, req.RepoRoot)
		result := uireview.StatusResult{TargetID: req.TargetID, RepoRoot: req.RepoRoot, Generation: req.Generation, Err: err}
		if err == nil {
			result.Handled = make(map[string]bool, len(state.Comments))
			for _, comment := range state.Comments {
				if comment.ID != "" && comment.Round > 0 {
					result.Handled[comment.ID] = comment.Resolved
				}
			}
		}
		return result
	}
}

func (m *Model) reviewHandleCmd(req uireview.HandleCommentRequest) tea.Cmd {
	stor := m.services.store
	return m.reviewFX.chain(func() tea.Msg {
		found, err := stor.SetReviewCommentHandled(req.TargetID, req.CommentID, req.Handled)
		return uireview.HandleCommentResult{
			TargetID: req.TargetID, RepoRoot: req.RepoRoot, CommentID: req.CommentID,
			Handled: req.Handled, Previous: req.Previous, Found: found, Err: err,
		}
	})
}

func (m *Model) reviewSendCmd(req uireview.SendRequest) tea.Cmd {
	stor, driver := m.services.store, m.services.tmux
	return m.reviewFX.chain(func() tea.Msg {
		result := uireview.SendResult{
			TargetID: req.Target.ID, RepoRoot: req.RepoRoot, CommentIDs: append([]string(nil), req.CommentIDs...),
			PreviousRound: req.PreviousRound, Round: req.Round, Count: req.Count, TargetName: req.Target.Name,
		}
		if !driver.Exists(req.Target.ID) {
			result.Err = errors.New(deadSessionHint)
			return result
		}
		if err := stor.MergeReviewState(req.Target.ID, req.RepoRoot, reviewStateToStore(req.State)); err != nil {
			result.Err = fmt.Errorf("saving review round: %w", err)
			return result
		}
		if err := driver.SendText(req.Target.ID, req.Prompt); err != nil {
			result.Err = err
			if rollbackErr := stor.MergeReviewState(req.Target.ID, req.RepoRoot, reviewStateToStore(req.PreviousState)); rollbackErr != nil {
				result.Err = fmt.Errorf("%w; restoring review drafts: %w", err, rollbackErr)
			}
			return result
		}
		result.Delivered = true
		result.AckErr = stor.SetAcked(req.Target.ID, false)
		return result
	})
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
