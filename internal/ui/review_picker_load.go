package ui

import (
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

type reviewPickerLoadKind uint8

const (
	reviewPickerLoadBranches reviewPickerLoadKind = iota
	reviewPickerLoadBases
)

// reviewPickerSource is the review identity whose projection a picker shows.
// It follows the message through the worker and stays on the open picker so a
// late read or Enter cannot retarget a newer review.
type reviewPickerSource struct {
	generation int
	targetID   string
	repoRoot   string
}

type reviewPickerLoadRequest struct {
	kind          reviewPickerLoadKind
	source        reviewPickerSource
	foregroundGen uint64
}

type reviewPickerLoadedMsg struct {
	request   reviewPickerLoadRequest
	rows      []pickRow
	current   string
	storeRoot string
	err       error
}

// reviewPickerReader keeps every filesystem, git, and SQLite read behind the
// tea.Cmd boundary. Canonical deliberately returns the input on resolution
// failure, matching the existing raw-path fallback.
type reviewPickerReader interface {
	Worktrees(string) ([]git.Worktree, error)
	BranchRefs(string) ([]string, error)
	ReviewBase(string, string) (string, error)
	Canonical(string) string
}

type systemReviewPickerReader struct {
	git   *git.Driver
	store *store.Store
}

func (r systemReviewPickerReader) Worktrees(root string) ([]git.Worktree, error) {
	return r.git.Worktrees(root)
}

func (r systemReviewPickerReader) BranchRefs(root string) ([]string, error) {
	return r.git.BranchRefs(root)
}

func (r systemReviewPickerReader) ReviewBase(targetID, root string) (string, error) {
	return r.store.ReviewBase(targetID, root)
}

func (systemReviewPickerReader) Canonical(root string) string {
	return resolveSymlinksOrSelf(root)
}

func reviewPickerReadCmd(reader reviewPickerReader, request reviewPickerLoadRequest) tea.Cmd {
	return func() tea.Msg {
		msg := reviewPickerLoadedMsg{request: request}
		switch request.kind {
		case reviewPickerLoadBranches:
			worktrees, err := reader.Worktrees(request.source.repoRoot)
			if err != nil {
				msg.err = err
				return msg
			}
			current := reader.Canonical(request.source.repoRoot)
			msg.rows = make([]pickRow, len(worktrees))
			for i, worktree := range worktrees {
				msg.rows[i] = pickRow{label: worktree.Branch, root: worktree.Root}
				if reader.Canonical(worktree.Root) == current {
					msg.current = worktree.Root
				}
			}
		case reviewPickerLoadBases:
			refs, err := reader.BranchRefs(request.source.repoRoot)
			if err != nil {
				msg.err = err
				return msg
			}
			msg.storeRoot = reader.Canonical(request.source.repoRoot)
			msg.current, msg.err = reader.ReviewBase(request.source.targetID, msg.storeRoot)
			if msg.err != nil {
				return msg
			}
			msg.rows = make([]pickRow, 1, len(refs)+1)
			msg.rows[0] = pickRow{label: "auto"}
			for _, ref := range refs {
				msg.rows = append(msg.rows, pickRow{label: ref, root: ref})
			}
		}
		return msg
	}
}
