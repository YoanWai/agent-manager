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
	if err := m.tmux.Create(sess.ID, sess.Cwd, command, env, m.previewPaneWidth(), m.previewPaneHeight()); err != nil {
		discardWorktree()
		return err
	}
	m.markFreshPane(sess.ID)
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

// goneMark records how a session left the loaded list: deleted outright,
// or archived out of the active view while the archived view keeps it.
type goneMark struct {
	at       time.Time
	archived bool
}

// markGone records that this run just removed a row from the list, so
// stale polls predating the removal are dropped on arrival.
func (m *Model) markGone(id string, archived bool) {
	if m.gone == nil {
		m.gone = map[string]goneMark{}
	}
	m.gone[id] = goneMark{at: time.Now(), archived: archived}
}

// dropRecentlyRemoved filters out the rows a poll listed before this run
// took them off the list itself: without it, a pass in flight across a
// delete or an archive delivers its pre-change listing afterwards and the
// row blinks back for one more frame. A poll whose listing postdates the
// removal is the authority on the new state and retires every record it
// postdates, including ones for rows the listing no longer carries at
// all; an archived row reported as archived belongs in the archived view
// and stays.
func (m *Model) dropRecentlyRemoved(polled []store.Session, listedAt time.Time) []store.Session {
	if len(m.gone) == 0 {
		return polled
	}
	kept := make([]store.Session, 0, len(polled))
	for _, sess := range polled {
		mark, gone := m.gone[sess.ID]
		if !gone || listedAt.After(mark.at) || (mark.archived && sess.Archived) {
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

// unmarkGone forgets a removal record, for a session brought back before
// any poll has had the chance to retire it.
func (m *Model) unmarkGone(id string) {
	delete(m.gone, id)
}

// stripDeletedGroups drops a stale listing's group rows and metadata for
// groups this run deleted after the listing was taken, so the deleted
// header cannot hang back onto the tree for a frame. A listing that
// postdates a deletion retires its marker instead.
func stripDeletedGroups(msg *refreshMsg, gone map[string]time.Time) {
	removed := make(map[string]bool, len(gone))
	for path, at := range gone {
		if msg.listedAt.After(at) {
			delete(gone, path)
			continue
		}
		removed[path] = true
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
