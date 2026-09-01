package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type worktreeRenameStore interface {
	ListSessions(bool) ([]store.Session, error)
	RenameSessionWorktreeBranch(string, string) error
}

// renameSessionWorktreeBranch keeps a managed branch aligned with its
// session's display name without moving the directory of a live process.
func renameSessionWorktreeBranch(gitDrv *git.Driver, st worktreeRenameStore, sess *store.Session, name string) error {
	if gitDrv == nil || sess.WorktreeRepo == "" || sess.WorktreeBranch == "" {
		return nil
	}
	sessions, err := st.ListSessions(true)
	if err != nil {
		return err
	}
	for _, other := range sessions {
		if other.ID != sess.ID && other.Cwd == sess.Cwd {
			return fmt.Errorf("worktree is shared with session %q", other.Name)
		}
	}
	branch, err := gitDrv.RenameWorktreeBranch(sess.WorktreeRepo, sess.Cwd, sess.WorktreeBranch, name)
	if err != nil {
		return err
	}
	if branch == sess.WorktreeBranch {
		return nil
	}
	if err := st.RenameSessionWorktreeBranch(sess.ID, branch); err != nil {
		rollbackBranch, rollbackErr := gitDrv.RenameWorktreeBranch(sess.WorktreeRepo, sess.Cwd, branch, sess.Name)
		if rollbackErr != nil {
			return fmt.Errorf("%w; could not restore git branch %s: %w", err, sess.WorktreeBranch, rollbackErr)
		}
		if rollbackBranch != sess.WorktreeBranch {
			return fmt.Errorf("%w; git branch rollback returned %s instead of %s", err, rollbackBranch, sess.WorktreeBranch)
		}
		return err
	}
	sess.WorktreeBranch = branch
	return nil
}

func (m *Model) openRename() {
	entry, ok := m.selectedRow()
	if !ok {
		return
	}
	if entry.isRoot() {
		m.errBar.text = "root is the top level, not a group to rename"
		return
	}
	input := textinput.New()
	input.CharLimit = 60
	input.Prompt = ""
	input.Focus()
	if entry.isGroup {
		input.SetValue(baseName(entry.group))
		dir := textField("default working directory", 400)
		dir.Prompt = ""
		dirValue := m.groupPaths[entry.group]
		if dirValue == "" {
			dirValue = m.groupDefaultDir(entry.group)
		}
		dir.SetValue(dirValue)
		m.pathSugg.reset()
		m.rename = renameTarget{
			isGroup:       true,
			path:          entry.group,
			input:         input,
			dir:           dir,
			worktreeIndex: groupWorktreeIndex(m.groupWorktrees[entry.group]),
		}
	} else {
		input.SetValue(entry.sess.Name)
		tools := sortedToolNames(m.cfg)
		shells := []string{}
		for _, name := range m.cfg.ToolNames() {
			if m.cfg.Tools[name].Shell {
				shells = append(shells, name)
			}
		}
		sort.Strings(shells)
		tools = append(tools, shells...)
		toolIndex := 0
		for i, name := range tools {
			if name == entry.sess.Tool {
				toolIndex = i
				break
			}
		}
		// Current tool missing from config (removed block): keep it selectable
		// so save does not silently reassign to the first configured tool.
		if len(tools) == 0 || tools[toolIndex] != entry.sess.Tool {
			tools = append([]string{entry.sess.Tool}, tools...)
			toolIndex = 0
		}
		m.rename = renameTarget{
			sessID:    entry.sess.ID,
			input:     input,
			toolNames: tools,
			toolIndex: toolIndex,
		}
	}
	m.mode = modeRename
	m.errBar.text = ""
}

func (m *Model) renameFocus(delta int) {
	m.pathSugg.reset()
	fields := 2
	if m.rename.isGroup {
		fields = 3
	}
	m.rename.focus = (m.rename.focus + delta + fields) % fields
	m.rename.input.Blur()
	m.rename.dir.Blur()
	switch m.rename.focus {
	case 0:
		m.rename.input.Focus()
	case 1:
		m.rename.dir.Focus()
	}
}

func (m *Model) handleRenameKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	pathSuggesting := m.rename.isGroup && m.rename.focus == 1 && m.pathSugg.active()
	switch msg.String() {
	case "esc":
		if pathSuggesting {
			m.pathSugg.reset()
			return m, nil
		}
		m.mode = modeList
		return m, nil
	case "tab":
		if !m.rename.isGroup {
			m.cycleRenameTool(1)
			return m, nil
		}
		if pathSuggesting {
			m.applyPathSuggestion()
			return m, nil
		}
		m.renameFocus(1)
		return m, nil
	case "shift+tab":
		if !m.rename.isGroup {
			m.cycleRenameTool(-1)
			return m, nil
		}
		if pathSuggesting {
			return m, nil
		}
		m.renameFocus(-1)
		return m, nil
	case "up", "down":
		if !m.rename.isGroup {
			break
		}
		if pathSuggesting {
			if msg.String() == "up" {
				if !m.pathSugg.move(-1) {
					m.renameFocus(-1)
				}
			} else {
				if !m.pathSugg.move(1) {
					m.renameFocus(1)
				}
			}
			return m, nil
		}
		if msg.String() == "up" {
			m.renameFocus(-1)
		} else {
			m.renameFocus(1)
		}
		return m, nil
	case "left", "right":
		if m.rename.isGroup && m.rename.focus == 2 {
			delta := 1
			if msg.String() == "left" {
				delta = -1
			}
			count := len(groupWorktreeOptions)
			m.rename.worktreeIndex = (m.rename.worktreeIndex + delta + count) % count
			return m, nil
		}
	case "enter":
		if pathSuggesting && m.pathSugg.chosen {
			m.applyPathSuggestion()
			return m, nil
		}
		return m.applyRename()
	}
	var cmd tea.Cmd
	switch m.rename.focus {
	case 0:
		m.rename.input, cmd = m.rename.input.Update(msg)
	case 1:
		m.rename.dir, cmd = m.rename.dir.Update(msg)
		m.pathSugg.recompute(m.rename.dir.Value())
	}
	return m, cmd
}

func (m *Model) cycleRenameTool(delta int) {
	if len(m.rename.toolNames) == 0 {
		return
	}
	n := len(m.rename.toolNames)
	m.rename.toolIndex = (m.rename.toolIndex + delta + n) % n
}

func (m *Model) renameTool() string {
	if len(m.rename.toolNames) == 0 {
		return ""
	}
	return m.rename.toolNames[m.rename.toolIndex]
}

func (m *Model) applyRename() (tea.Model, tea.Cmd) {
	name := strings.TrimSpace(m.rename.input.Value())
	name = strings.ReplaceAll(name, "/", "-")
	if name == "" {
		m.errBar.text = "name cannot be empty"
		return m, nil
	}
	if m.rename.isGroup {
		parent := parentGroup(m.rename.path)
		dir, ok := resolveExistingDir(m.rename.dir.Value(), m.groupDefaultDir(parent))
		if !ok {
			m.errBar.text = "default path does not exist: " + dir
			return m, nil
		}
		newPath := name
		if parent != "" {
			newPath = parent + "/" + name
		}
		if err := m.store.RenameGroup(m.rename.path, newPath); err != nil {
			m.errBar.text = err.Error()
			return m, nil
		}
		// CreateGroup upserts, so it doubles as the default-path setter.
		if err := m.store.CreateGroup(newPath, dir); err != nil {
			m.errBar.text = err.Error()
			return m, nil
		}
		worktree := groupWorktreeValue(m.rename.worktreeIndex)
		if err := m.store.SetGroupWorktree(newPath, worktree); err != nil {
			m.errBar.text = err.Error()
			return m, nil
		}
		m.renameGroupLocally(m.rename.path, newPath, dir, worktree)
		m.relabelSubtree(newPath)
	} else {
		index := -1
		for i := range m.sessions {
			if m.sessions[i].ID == m.rename.sessID {
				index = i
				break
			}
		}
		tool := m.renameTool()
		prevTool := ""
		if index >= 0 {
			prevTool = m.sessions[index].Tool
		}
		toolChanged := tool != "" && tool != prevTool
		if toolChanged && m.isShell(tool) {
			kids, err := m.store.Children(m.rename.sessID)
			if err != nil {
				m.errBar.text = err.Error()
				return m, nil
			}
			if len(kids) > 0 {
				m.errBar.text = "move its terminals first"
				return m, nil
			}
		}
		// The branch changes before the name is stored, so a name git cannot
		// give it leaves the rename card open instead of splitting them apart.
		if index >= 0 {
			if err := renameSessionWorktreeBranch(m.gitDrv, m.store, &m.sessions[index], name); err != nil {
				m.errBar.text = "worktree rename: " + err.Error()
				return m, nil
			}
		}
		if err := m.store.RenameSession(m.rename.sessID, name); err != nil {
			m.errBar.text = err.Error()
			return m, nil
		}
		if toolChanged {
			if err := m.store.UpdateTool(m.rename.sessID, tool); err != nil {
				m.errBar.text = err.Error()
				return m, nil
			}
		}
		if index >= 0 {
			m.sessions[index].Name = name
			if toolChanged {
				m.sessions[index].Tool = tool
				m.sessions[index].AgentSessionID = ""
			}
		}
		m.relabelSession(m.rename.sessID)
	}
	m.rebuildRows()
	m.mode = modeList
	m.requestRefresh()
	return m, nil
}

// renameGroupLocally rewrites the in-memory tree right away, so the
// frames between saving and the poller's next refresh already show the
// new name and path instead of flashing the stale ones.
func (m *Model) renameGroupLocally(old, newPath, dir, worktree string) {
	moved := func(group string) (string, bool) {
		if group == old || strings.HasPrefix(group, old+"/") {
			return newPath + group[len(old):], true
		}
		return group, false
	}
	for i := range m.groups {
		m.groups[i], _ = moved(m.groups[i])
	}
	for i := range m.sessions {
		m.sessions[i].Group, _ = moved(m.sessions[i].Group)
	}
	groupPaths := make(map[string]string, len(m.groupPaths))
	for group, path := range m.groupPaths {
		group, _ = moved(group)
		groupPaths[group] = path
	}
	groupPaths[newPath] = dir
	m.groupPaths = groupPaths
	groupWorktrees := make(map[string]string, len(m.groupWorktrees))
	for group, choice := range m.groupWorktrees {
		group, _ = moved(group)
		groupWorktrees[group] = choice
	}
	if worktree == "" {
		delete(groupWorktrees, newPath)
	} else {
		groupWorktrees[newPath] = worktree
	}
	m.groupWorktrees = groupWorktrees
	for group, folded := range m.collapsed {
		if renamed, ok := moved(group); ok {
			delete(m.collapsed, group)
			m.collapsed[renamed] = folded
		}
	}
	m.persistCollapsed()
}

// relabelSession refreshes one session's tmux status-bar label from the db.
func (m *Model) relabelSession(id string) {
	sess, err := m.store.Get(id)
	if err != nil {
		m.errBar.text = err.Error()
		return
	}
	if !m.tmux.Exists(id) {
		return
	}
	if err := m.tmux.SetLabel(id, sessionLabel(sess.Group, sess.Name)); err != nil {
		m.errBar.text = err.Error()
	}
}

// relabelSubtree refreshes labels for every session under a group path.
func (m *Model) relabelSubtree(path string) {
	sessions, err := m.store.SessionsInSubtree(path)
	if err != nil {
		m.errBar.text = err.Error()
		return
	}
	for _, sess := range sessions {
		m.relabelSession(sess.ID)
	}
}
