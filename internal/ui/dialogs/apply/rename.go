package apply

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
)

// Kind is what a rename targets: a group path or a session row.
type Kind int

const (
	Group Kind = iota
	Session
)

// Request carries everything the rename worker needs so it never reads the
// running model: the target, the requested values and a copy of the session
// row that pins which creation, launch and socket the rename was meant for.
type Request struct {
	Kind              Kind
	OldGroup          string
	NewGroup          string
	Dir               string
	RawDir            string
	DirFallbacks      []string
	DraftDir          string
	Worktree          string
	Base              string
	Env               string
	SessID            string
	Sess              store.Session
	Name              string
	Tool              string
	CheckNoChildren   bool
	CheckBrowserReady bool
	Gen               uint64
}

func (r Request) Copy() Request {
	r.Sess.PendingInputs = slices.Clone(r.Sess.PendingInputs)
	r.Sess.RelaunchSnapshot = maps.Clone(r.Sess.RelaunchSnapshot)
	return r
}

// Outcome reports which durable stages committed, in order, so the apply
// side can reconcile partial failures before surfacing the error.
type Outcome struct {
	GroupRenamed bool
	PathSet      bool
	WorktreeSet  bool
	BaseSet      bool
	EnvSet       bool
	NameRenamed  bool
	ToolChanged  bool
	Branch       string
	Sessions     []store.Session
	Warning      error
	Dir          string
}

func Rename(s Sources, preflight Preflight, request Request) (Outcome, error) {
	result := Outcome{}
	if request.Kind == Group {
		if request.RawDir != "" || len(request.DirFallbacks) > 0 {
			resolved, ok := preflight.Resolve(request.RawDir, request.DirFallbacks)
			result.Dir = resolved
			if !ok {
				return result, fmt.Errorf("default path does not exist: %s", resolved)
			}
			request.Dir = resolved
		} else {
			result.Dir = request.Dir
		}
		var err error
		if err = s.Store.RenameGroup(request.OldGroup, request.NewGroup); err != nil {
			return result, err
		}
		result.GroupRenamed = true
		// CreateGroup upserts, so it doubles as the default-path setter.
		if err = s.Store.CreateGroup(request.NewGroup, request.Dir); err != nil {
			return result, err
		}
		result.PathSet = true
		if err = s.Store.SetGroupWorktree(request.NewGroup, request.Worktree); err != nil {
			return result, err
		}
		result.WorktreeSet = true
		if err = s.Store.SetGroupBase(request.NewGroup, request.Base); err != nil {
			return result, err
		}
		result.BaseSet = true
		if err = s.Store.SetGroupEnvironment(request.NewGroup, request.Env); err != nil {
			return result, err
		}
		result.EnvSet = true
		sessions, err := s.Store.SessionsInSubtree(request.NewGroup)
		if err != nil {
			return result, err
		}
		result.Sessions = sessions
		result.Warning = s.relabel(sessions)
		return result, nil
	}
	if request.CheckNoChildren {
		children, err := s.Store.Children(request.SessID)
		if err != nil {
			return result, err
		}
		if len(children) > 0 {
			return result, errors.New("move its terminals first")
		}
	}
	if request.CheckBrowserReady {
		if err := s.BrowserReady(); err != nil {
			return result, err
		}
	}
	// A rename queued behind another on the same row must start from the
	// branch that one left, so the worker reads the row instead of trusting
	// the enqueue-time copy, which only pins which launch it was meant for.
	sess, err := s.Store.Get(request.SessID)
	if err != nil {
		return result, err
	}
	// A rename names the session, not one run of its agent, so a restart or
	// revive while it waits still renames; a replaced row does not.
	if !sess.CreatedAt.Equal(request.Sess.CreatedAt) || sess.TmuxSocket != request.Sess.TmuxSocket {
		return result, errors.New("rename target was replaced (its creation or socket changed) before the accepted rename ran")
	}
	// The branch changes before the name is stored, so a name git cannot
	// give it leaves the rename card open instead of splitting them apart.
	if sess.WorktreeRepo != "" && sess.WorktreeBranch != "" {
		if err := sessioncmd.RenameWorktreeBranch(s.Git, s.Store, &sess, request.Name); err != nil {
			return result, err
		}
		if sess.WorktreeBranch != request.Sess.WorktreeBranch {
			result.Branch = sess.WorktreeBranch
		}
	}
	if err = s.Store.RenameSession(request.SessID, request.Name); err != nil {
		return result, err
	}
	result.NameRenamed = true
	if request.Tool != "" && request.Tool != request.Sess.Tool {
		if err = s.Store.UpdateTool(request.SessID, request.Tool); err != nil {
			return result, err
		}
		result.ToolChanged = true
	}
	updated, err := s.Store.Get(request.SessID)
	if err != nil {
		return result, err
	}
	result.Sessions = []store.Session{updated}
	result.Warning = s.relabel(result.Sessions)
	return result, nil
}

// relabel refreshes tmux status-bar labels from durable rows. A label
// failure is a warning: the rename already committed.
func (s Sources) relabel(sessions []store.Session) error {
	var first error
	for _, sess := range sessions {
		if !s.Panes.Exists(sess.ID) {
			continue
		}
		if err := s.Panes.SetLabel(sess.ID, sessioncmd.SessionLabel(sess.Group, sess.Name)); err != nil && first == nil {
			first = err
		}
	}
	return first
}
