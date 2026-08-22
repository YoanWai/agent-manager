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
