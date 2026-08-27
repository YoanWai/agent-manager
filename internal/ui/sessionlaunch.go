package ui

import (
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/store"
)

type launchOptions struct {
	rollbackWorktree bool
}

func (m *Model) launchNewSession(sess store.Session, tool config.Tool, baseCommand string, opts launchOptions) error {
	if sess.CreatedAt.IsZero() {
		sess.CreatedAt = time.Now()
	}
	if sess.LastStatusAt.IsZero() {
		sess.LastStatusAt = sess.CreatedAt
	}
	discardWorktree := func() {
		if opts.rollbackWorktree {
			m.discardWorktree(sess.WorktreeRepo, sess.Cwd, sess.WorktreeBranch)
		}
	}
	command, env, err := m.buildLaunch(sess.Tool, tool, baseCommand, sess.ID)
	if err != nil {
		discardWorktree()
		return err
	}
	paneWidth, paneHeight := m.paneTargetSize()
	if err := m.tmux.Create(sess.ID, sess.Cwd, command, env, paneWidth, paneHeight); err != nil {
		discardWorktree()
		return err
	}
	m.markFreshPane(sess.ID)
	sess.TmuxSocket = m.tmux.SocketPath()
	if err := m.store.CreateSession(sess); err != nil {
		_ = m.tmux.Kill(sess.ID)
		_ = m.hooks.Remove(sess.ID)
		discardWorktree()
		return err
	}
	labelErr := m.tmux.SetLabel(sess.ID, sessionLabel(sess.Group, sess.Name))
	if m.launched == nil {
		m.launched = map[string]time.Time{}
	}
	m.launched[sess.ID] = time.Now()
	m.sessions = append(m.sessions, sess)
	m.rebuildRows()
	return labelErr
}

// forgetLaunch drops a row from the pending set, so a session the user has
// since sent away is not carried back onto the tree by a poll that listed
// the store before it was launched.
func (m *Model) forgetLaunch(id string) {
	delete(m.launched, id)
}

// keepPendingLaunches carries over the rows this run spawned that the poll
// has not looked for yet. A poll lists its sessions and then spends the pass
// in tmux and ps calls, so the list the UI finally receives can predate a
// launch and would otherwise blink the new agent off screen. The first poll
// to list the store after a launch is the authority on it, whether it
// reports the row or its absence.
func (m *Model) keepPendingLaunches(polled []store.Session, listedAt time.Time) []store.Session {
	for id, at := range m.launched {
		if listedAt.After(at) {
			delete(m.launched, id)
			continue
		}
		if !sessionGone(polled, id) {
			continue
		}
		for _, sess := range m.sessions {
			if sess.ID == id {
				polled = append(polled, sess)
				break
			}
		}
	}
	return polled
}

// goneMark is the list state this run most recently gave a row, and when
// it gave it. deleted means the row must not appear at all; otherwise
// archived is the value the row's Archived flag should carry.
type goneMark struct {
	at       time.Time
	archived bool
	deleted  bool
}

// markSession records the loaded-list state this run just gave a session,
// so stale polls predating the change are reconciled on arrival instead
// of undoing it for a frame.
func (m *Model) markSession(id string, mark goneMark) {
	if m.gone == nil {
		m.gone = map[string]goneMark{}
	}
	mark.at = time.Now()
	m.gone[id] = mark
}

// dropRecentlyRemoved reconciles the rows a poll lists against what this
// run has just done to them: without it, a pass in flight across a delete,
// an archive, or a restore delivers its pre-change listing afterwards and
// blinks the old state back for one more frame. A stale copy of a deleted
// row is dropped; a stale copy of an archive or restore has its flag
// corrected to what the store was just written to say. A listing that
// postdates every recorded change retires those records instead.
func (m *Model) dropRecentlyRemoved(polled []store.Session, listedAt time.Time) []store.Session {
	if len(m.gone) == 0 {
		return polled
	}
	kept := make([]store.Session, 0, len(polled))
	for _, sess := range polled {
		mark, known := m.gone[sess.ID]
		switch {
		case !known || listedAt.After(mark.at):
			if known {
				delete(m.gone, sess.ID)
			}
			kept = append(kept, sess)
		case mark.deleted:
		default:
			sess.Archived = mark.archived
			kept = append(kept, sess)
		}
	}
	for id, mark := range m.gone {
		if listedAt.After(mark.at) {
			delete(m.gone, id)
		}
	}
	return kept
}

// stripDeletedGroups reconciles a stale listing's group rows, metadata,
// and archive flags against what this run has just done: a deleted group's
// header cannot hang back onto the tree, and an archive or restore of a
// group keeps its flag until a newer listing confirms it. A listing that
// postdates a change retires its marker instead.
func stripDeletedGroups(msg *refreshMsg, gone map[string]goneMark) {
	removed := make(map[string]bool, len(gone))
	for path, mark := range gone {
		switch {
		case msg.listedAt.After(mark.at):
			delete(gone, path)
		case mark.deleted:
			removed[path] = true
		default:
			if msg.archivedGroups == nil {
				msg.archivedGroups = map[string]bool{}
			}
			msg.archivedGroups[path] = mark.archived
		}
	}
	if len(removed) == 0 {
		return
	}
	groups := make([]string, 0, len(msg.groups))
	for _, group := range msg.groups {
		if !removed[group] {
			groups = append(groups, group)
		}
	}
	msg.groups = groups
	for path := range removed {
		delete(msg.groupPaths, path)
		delete(msg.groupWorktrees, path)
		delete(msg.archivedGroups, path)
	}
}
