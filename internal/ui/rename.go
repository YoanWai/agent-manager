package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) openRename() {
	entry, ok := m.selectedRow()
	if !ok {
		return
	}
	if entry.isRoot() {
		m.err = "root is the top level, not a group to rename"
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
		m.rename = renameTarget{isGroup: true, path: entry.group, input: input, dir: dir}
	} else {
		input.SetValue(entry.sess.Name)
		tools := sortedToolNames(m.cfg)
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
	m.err = ""
}

func (m *Model) renameFocus(delta int) {
	m.pathSugg.reset()
	m.rename.focus = (m.rename.focus + delta + 2) % 2
	m.rename.input.Blur()
	m.rename.dir.Blur()
	if m.rename.focus == 0 {
		m.rename.input.Focus()
	} else {
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
	case "enter":
		if pathSuggesting && m.pathSugg.chosen {
			m.applyPathSuggestion()
			return m, nil
		}
		return m.applyRename()
	}
	var cmd tea.Cmd
	if m.rename.focus == 0 {
		m.rename.input, cmd = m.rename.input.Update(msg)
	} else {
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
		m.err = "name cannot be empty"
		return m, nil
	}
	if m.rename.isGroup {
		parent := parentGroup(m.rename.path)
		dir, ok := resolveExistingDir(m.rename.dir.Value(), m.groupDefaultDir(parent))
		if !ok {
			m.err = "default path does not exist: " + dir
			return m, nil
		}
		newPath := name
		if parent != "" {
			newPath = parent + "/" + name
		}
		if err := m.store.RenameGroup(m.rename.path, newPath); err != nil {
			m.err = err.Error()
			return m, nil
		}
		// CreateGroup upserts, so it doubles as the default-path setter.
		if err := m.store.CreateGroup(newPath, dir); err != nil {
			m.err = err.Error()
			return m, nil
		}
		m.renameGroupLocally(m.rename.path, newPath, dir)
		m.relabelSubtree(newPath)
	} else {
		if err := m.store.RenameSession(m.rename.sessID, name); err != nil {
			m.err = err.Error()
			return m, nil
		}
		tool := m.renameTool()
		var prevTool string
		for i := range m.sessions {
			if m.sessions[i].ID == m.rename.sessID {
				prevTool = m.sessions[i].Tool
				break
			}
		}
		toolChanged := tool != "" && tool != prevTool
		if toolChanged {
			if err := m.store.UpdateTool(m.rename.sessID, tool); err != nil {
				m.err = err.Error()
				return m, nil
			}
		}
		for i := range m.sessions {
			if m.sessions[i].ID == m.rename.sessID {
				m.sessions[i].Name = name
				if toolChanged {
					m.sessions[i].Tool = tool
					m.sessions[i].AgentSessionID = ""
				}
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
func (m *Model) renameGroupLocally(old, newPath, dir string) {
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
		m.err = err.Error()
		return
	}
	if !m.tmux.Exists(id) {
		return
	}
	if err := m.tmux.SetLabel(id, sessionLabel(sess.Group, sess.Name)); err != nil {
		m.err = err.Error()
	}
}

// relabelSubtree refreshes labels for every session under a group path.
func (m *Model) relabelSubtree(path string) {
	sessions, err := m.store.SessionsInSubtree(path)
	if err != nil {
		m.err = err.Error()
		return
	}
	for _, sess := range sessions {
		m.relabelSession(sess.ID)
	}
}
