package execution

import (
	"errors"
	"fmt"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"

	"github.com/YoanWai/agent-manager/internal/store"
)

// applyPendingRename picks up a name the session's agent left via the
// rename subcommand: the store row and tmux label update together here,
// keeping the manager the sole database writer. The file is consumed
// even when the name is unchanged so it never lingers, and the outcome
// is left in the result mailbox for the subcommand still waiting on it.
// A dead tmux session cannot take a label, which is fine; the label is
// rewritten on revive. A worktree session's branch follows the new name
// while its live directory stays fixed, and a name git cannot give the
// branch keeps the session on the one it has: the file is consumed either
// way, so the reason is reported once rather than on every poll from
// here on.
func (p *Runner) applyPendingRename(sess *store.Session) error {
	request, name, found, err := p.hooks.ClaimName(sess.ID)
	if err != nil || !found {
		return err
	}
	renameErr := p.renamePending(sess, name)
	// The claim outlives a verdict this poll could not write: the next one
	// answers the same rename rather than leaving the agent that asked to
	// time out on a rename that has in fact been applied or refused. That
	// pass costs nothing, since a name the session already carries is
	// applied again as a no-op and a refusal is refused the same way.
	// A rename that carries no request of ours has nobody waiting on an
	// answer, so it is applied and left at that.
	if request != "" {
		if err := p.hooks.WriteNameResult(sess.ID, request, name, sess.Name, renameErr); err != nil {
			return err
		}
	}
	if err := p.hooks.ReleaseName(sess.ID); err != nil {
		return err
	}
	// The row going out from under this rename is the agent's answer, not
	// a failure of the pass that carried it.
	if errors.Is(renameErr, store.ErrSessionGone) {
		return nil
	}
	return renameErr
}

func (p *Runner) renamePending(sess *store.Session, name string) error {
	if name == "" || name == sess.Name {
		return nil
	}
	if err := sessioncmd.RenameWorktreeBranch(p.gitDrv, p.store, sess, name); err != nil {
		return fmt.Errorf("worktree rename: %w", err)
	}
	if err := p.store.RenameSession(sess.ID, name); err != nil {
		return err
	}
	sess.Name = name
	_ = p.tmux.SetLabel(sess.ID, sessionLabel(sess.Group, name))
	return nil
}

func (p *Runner) applyPendingReviewRepo(sess *store.Session) error {
	root, found := p.hooks.ReadReviewRepo(sess.ID)
	if !found {
		return nil
	}
	if root != "" {
		if err := p.store.SetReviewRepo(sess.ID, root); err != nil {
			return err
		}
	}
	return p.hooks.RemoveReviewRepo(sess.ID)
}

func (p *Runner) applyPendingReviewBase(sess *store.Session) error {
	root, ref, found := p.hooks.ReadReviewBase(sess.ID)
	if !found {
		return nil
	}
	if root != "" {
		if err := p.store.SetReviewBase(sess.ID, root, ref); err != nil {
			return err
		}
	}
	return p.hooks.RemoveReviewBase(sess.ID)
}

func (p *Runner) applyPendingReviewScope(sess *store.Session) error {
	scope, found := p.hooks.ReadReviewScope(sess.ID)
	if !found {
		return nil
	}
	if err := p.store.SetReviewScope(sess.ID, scope); err != nil {
		return err
	}
	return p.hooks.RemoveReviewScope(sess.ID)
}

// renameSessionWorktreeBranch keeps a managed branch aligned with its
// session's display name without moving the directory of a live process.
