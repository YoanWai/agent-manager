package ui

import (
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/project"
	"github.com/YoanWai/agent-manager/internal/store"
)

type launchOptions struct {
	rollbackWorktree bool
	// env is exported into the pane on top of what buildLaunch sets, for the
	// project variables a run or setup script is given. Keys already set by
	// buildLaunch are left alone, so a project cannot shadow the hook and MCP
	// wiring a session needs to report its status.
	env map[string]string
	// setup is the project's setup script, run in the pane before the agent.
	// It is applied here rather than by the caller because buildLaunch appends
	// to the command it is given — MCP config, a --settings path — and those
	// flags belong to the agent, inside the wrapper's success branch, not
	// dangling after its fi.
	setup string
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
	for key, value := range opts.env {
		if _, taken := env[key]; !taken {
			env[key] = value
		}
	}
	command = project.SetupCommand(opts.setup, command)
	if err := m.tmux.Create(sess.ID, sess.Cwd, command, env, m.previewPaneWidth(), m.previewPaneHeight()); err != nil {
		discardWorktree()
		return err
	}
	if err := m.store.CreateSession(sess); err != nil {
		_ = m.tmux.Kill(sess.ID)
		_ = m.hooks.Remove(sess.ID)
		discardWorktree()
		return err
	}
	labelErr := m.tmux.SetLabel(sess.ID, sessionLabel(sess.Group, sess.Name))
	m.sessions = append(m.sessions, sess)
	m.rebuildRows()
	return labelErr
}
