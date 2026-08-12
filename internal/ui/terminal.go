package ui

import (
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

// terminalKeyWindow is how long after a T keystroke another one reads as
// autorepeat rather than a second request: comfortably longer than the
// interval a held key repeats at, shorter than a deliberate second press.
const terminalKeyWindow = 250 * time.Millisecond

// shellTool finds the config block T spawns: the first one declaring
// shell = true, by name so the pick is the same on every launch. Nothing
// keys off the block being called "terminal", so a user who already has a
// [tools.terminal] block of their own keeps it as the agent CLI they wrote.
func (m *Model) shellTool() (string, config.Tool, bool) {
	return m.cfg.ShellTool()
}

// isShell reports whether a session's tool opens a shell rather than an
// agent. A tool no longer in the config answers false: an unknown block is
// not something we can claim runs a shell.
func (m *Model) isShell(toolName string) bool {
	return m.cfg.Tools[toolName].Shell
}

// terminalKeyRepeat reports whether T arrived inside the burst a held key
// sends. Each keystroke pushes the window out, so holding T spawns one
// shell however long it is held. The other keys that create something open
// a form first, which absorbs a burst on its own.
func (m *Model) terminalKeyRepeat() bool {
	now := time.Now()
	repeated := now.Sub(m.terminalKeyAt) < terminalKeyWindow
	m.terminalKeyAt = now
	return repeated
}

// openTerminal spawns a shell tab in the group under the cursor. The block
// carries no command, and tmux.Create leaves such a pane on the user's
// shell, so no prompt, rename directive or MCP registration applies to a
// session there is no agent to send them to.
func (m *Model) openTerminal() (tea.Model, tea.Cmd) {
	toolName, tool, ok := m.shellTool()
	if !ok {
		m.errBar.text = `no shell configured: add a tool block with shell = true to config.toml`
		return m, nil
	}
	dir, ok := m.rowDir()
	if !ok {
		m.errBar.text = "no directory to open a terminal in: " + dir
		return m, nil
	}
	sess := store.Session{
		ID:     newID(),
		Name:   toolName + "-" + newID()[:4],
		Tool:   toolName,
		Cwd:    dir,
		Group:  m.contextGroup(),
		Status: status.Starting,
	}
	if err := m.launchNewSession(sess, tool, tool.Command, launchOptions{}); err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
	// Starting sits outside the attention set, so the row the key just made
	// would be filtered off screen.
	m.statusFilter = statusFilterAll
	m.errBar.text = ""
	m.focusSession(sess.ID)
	return m, m.refreshCmd()
}

// rowDir is the directory the cursor points at: a live session's pane
// directory, which follows wherever its shell or agent moved, falling back
// to the directory it was created in; for a group, its default path. Both
// paths are checked, so a directory removed under a running session cannot
// hand back somewhere that is no longer there.
func (m *Model) rowDir() (string, bool) {
	entry, ok := m.selectedRow()
	if !ok {
		return "", false
	}
	if entry.isGroup {
		return resolveExistingDir(m.groupPaths[entry.group], m.groupDefaultDir(entry.group))
	}
	if path, err := m.tmux.PaneCurrentPath(entry.sess.ID); err == nil && isDir(path) {
		return path, true
	}
	return entry.sess.Cwd, isDir(entry.sess.Cwd)
}
