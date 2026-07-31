package ui

import (
	"fmt"
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
)

// launchNewSession creates one new tmux session and its store record. Session
// forms and forks prepare different commands but share the same launch path.
func (m *Model) launchNewSession(sess store.Session, baseCommand string, deferDirective bool) error {
	tool, ok := m.cfg.Tools[sess.Tool]
	if !ok {
		return fmt.Errorf("tool %s is no longer configured", sess.Tool)
	}
	if sess.CreatedAt.IsZero() {
		sess.CreatedAt = time.Now()
	}
	if sess.LastStatusAt.IsZero() {
		sess.LastStatusAt = sess.CreatedAt
	}
	command, env, err := m.buildLaunch(sess.Tool, tool, baseCommand, sess.ID)
	if err != nil {
		m.discardWorktree(sess.WorktreeRepo, sess.Cwd, sess.WorktreeBranch)
		return err
	}
	if err := m.tmux.Create(sess.ID, sess.Cwd, command, env, m.previewPaneWidth(), m.previewPaneHeight()); err != nil {
		m.discardWorktree(sess.WorktreeRepo, sess.Cwd, sess.WorktreeBranch)
		return err
	}
	if err := m.store.CreateSession(sess); err != nil {
		_ = m.tmux.Kill(sess.ID)
		_ = m.hooks.Remove(sess.ID)
		m.discardWorktree(sess.WorktreeRepo, sess.Cwd, sess.WorktreeBranch)
		return err
	}
	if deferDirective && m.poller != nil {
		m.poller.markDirectivePending(sess.ID)
	}
	if err := m.tmux.SetLabel(sess.ID, sessionLabel(sess.Group, sess.Name)); err != nil {
		return err
	}
	m.sessions = append(m.sessions, sess)
	m.rebuildRows()
	return nil
}
