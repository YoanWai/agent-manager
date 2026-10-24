package source

import (
	"github.com/YoanWai/agent-manager/internal/git"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	tea "github.com/charmbracelet/bubbletea"
)

// PickerLoadKind selects the rows a picker read asks for.
type PickerLoadKind uint8

const (
	PickerLoadBranches PickerLoadKind = iota
	PickerLoadBases
)

// PickerLoadRequest is one picker read, with the review identity whose
// projection it shows and the generation that fences its completion.
type PickerLoadRequest struct {
	Kind          PickerLoadKind
	Source        uireview.PickerSource
	ForegroundGen uint64
}

// PickerLoadedMsg carries the rows a picker read found, still keyed by the
// request that asked for them.
type PickerLoadedMsg struct {
	Request   PickerLoadRequest
	Rows      []uireview.PickerRow
	Current   string
	StoreRoot string
	Err       error
}

// PickerReader is the filesystem, git and SQLite side of a picker read.
type PickerReader interface {
	Worktrees(string) ([]git.Worktree, error)
	BranchRefs(string) ([]string, error)
	ReviewBase(string, string) (string, error)
	Canonical(string) string
}

// PickerReadCmd reads the rows a picker lists, off the update path.
func PickerReadCmd(reader PickerReader, request PickerLoadRequest) tea.Cmd {
	return func() tea.Msg {
		msg := PickerLoadedMsg{Request: request}
		switch request.Kind {
		case PickerLoadBranches:
			worktrees, err := reader.Worktrees(request.Source.RepoRoot)
			if err != nil {
				msg.Err = err
				return msg
			}
			current := reader.Canonical(request.Source.RepoRoot)
			msg.Rows = make([]uireview.PickerRow, len(worktrees))
			for i, worktree := range worktrees {
				msg.Rows[i] = uireview.PickerRow{Label: worktree.Branch, Root: worktree.Root}
				if reader.Canonical(worktree.Root) == current {
					msg.Current = worktree.Root
				}
			}
		case PickerLoadBases:
			refs, err := reader.BranchRefs(request.Source.RepoRoot)
			if err != nil {
				msg.Err = err
				return msg
			}
			msg.StoreRoot = reader.Canonical(request.Source.RepoRoot)
			msg.Current, msg.Err = reader.ReviewBase(request.Source.TargetID, msg.StoreRoot)
			if msg.Err != nil {
				return msg
			}
			msg.Rows = make([]uireview.PickerRow, 1, len(refs)+1)
			msg.Rows[0] = uireview.PickerRow{Label: "auto"}
			for _, ref := range refs {
				msg.Rows = append(msg.Rows, uireview.PickerRow{Label: ref, Root: ref})
			}
		}
		return msg
	}
}
