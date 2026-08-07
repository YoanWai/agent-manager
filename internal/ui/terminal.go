package ui

import (
	"os"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

// terminalTool names the config block behind the terminal tab. It is a
// session like any other — same list, same group, same kill and revive —
// with a shell in the pane instead of an agent.
const terminalTool = "terminal"

// openTerminal spawns a shell tab in the group under the cursor. The tool
// block carries no command, and tmux.Create leaves such a pane on the
// user's shell, so no prompt, rename directive or MCP registration applies
// to a session there is no agent to send them to.
func (m *Model) openTerminal() (tea.Model, tea.Cmd) {
	dir, ok := m.rowDir()
	if !ok {
		m.errBar.text = "no directory to open a terminal in: " + dir
		return m, nil
	}
	tool := m.cfg.Tools[terminalTool]
	sess := store.Session{
		ID:     newID(),
		Name:   terminalTool + "-" + newID()[:4],
		Tool:   terminalTool,
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
	for i, row := range m.rows {
		if !row.isGroup && row.sess.ID == sess.ID {
			m.cursor = i
			break
		}
	}
	return m, m.refreshCmd()
}

// rowDir is the directory the cursor points at: a live session's pane
// directory, which follows wherever its shell or agent moved, falling back
// to the directory it was created in; for a group, its default path.
func (m *Model) rowDir() (string, bool) {
	entry, ok := m.selectedRow()
	if !ok {
		return "", false
	}
	if entry.isGroup {
		return resolveExistingDir(m.groupPaths[entry.group], m.groupDefaultDir(entry.group))
	}
	if path, err := m.tmux.PaneCurrentPath(entry.sess.ID); err == nil && path != "" {
		return path, true
	}
	info, err := os.Stat(entry.sess.Cwd)
	return entry.sess.Cwd, err == nil && info.IsDir()
}
