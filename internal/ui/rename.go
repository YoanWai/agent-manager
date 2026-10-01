package ui

import (
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"sort"
	"strings"
)

type renameTarget struct {
	isGroup       bool
	path          string
	sessID        string
	input         textinput.Model
	dir           textinput.Model
	worktreeIndex int
	focus         int
	toolNames     []string
	toolIndex     int
}

func (m *Model) openRename() {
	m.dialogGen++
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
		dirValue := m.workspace.groupPaths[entry.group]
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
			worktreeIndex: groupWorktreeIndex(m.workspace.groupWorktrees[entry.group]),
		}
	} else {
		input.SetValue(entry.sess.Name)
		tools := sortedToolNames(m.services.cfg)
		shells := []string{}
		for _, name := range m.services.cfg.ToolNames() {
			if m.services.cfg.Tools[name].Shell {
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
		// Pre-confirmation read: the default dir must exist before the job
		// is accepted, so the dialog stays open with a specific message.
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
		m.dialogGen++
		m.enqueueEffect(renameRequest{
			kind:     renameGroup,
			oldGroup: m.rename.path,
			newGroup: newPath,
			dir:      dir,
			worktree: groupWorktreeValue(m.rename.worktreeIndex),
			name:     name,
			gen:      m.dialogGen,
		}, 0, false)
		return m, m.nextEffectCmd()
	}
	index := -1
	var sess store.Session
	for i := range m.workspace.sessions {
		if m.workspace.sessions[i].ID == m.rename.sessID {
			index = i
			break
		}
	}
	tool := m.renameTool()
	prevTool := ""
	if index >= 0 {
		prevTool = m.workspace.sessions[index].Tool
		sess = m.workspace.sessions[index]
	}
	toolChanged := tool != "" && tool != prevTool
	// Pre-confirmation read: the shell child restriction is checked before
	// dispatch, so a refused rename never enters the queue.
	if toolChanged && m.isShell(tool) {
		kids, err := m.services.store.Children(m.rename.sessID)
		if err != nil {
			m.errBar.text = err.Error()
			return m, nil
		}
		if len(kids) > 0 {
			m.errBar.text = "move its terminals first"
			return m, nil
		}
	}
	m.dialogGen++
	m.enqueueEffect(renameRequest{
		kind:   renameSession,
		sessID: m.rename.sessID,
		sess:   sess,
		name:   name,
		tool:   tool,
		gen:    m.dialogGen,
	}, 0, false)
	return m, m.nextEffectCmd()
}

// renameGroupLocally rewrites the in-memory tree right away, so the
// frames between saving and the poller's next refresh already show the
// new name and path instead of flashing the stale ones.
func (m *Model) renameGroupLocally(old, newPath, dir, worktree string) {
	m.renameGroupInventory(old, newPath)
	m.workspace.groupPaths[newPath] = dir
	if worktree == "" {
		delete(m.workspace.groupWorktrees, newPath)
	} else {
		m.workspace.groupWorktrees[newPath] = worktree
	}
	m.applyRailStateDecision(m.rail.RenameGroup(old, newPath))
}

// renameGroupInventory moves the in-memory groups and sessions under a
// renamed path; metadata maps are mirrored by the caller, which decides
// which stages committed.
func (m *Model) renameGroupInventory(old, newPath string) {
	moved := func(group string) (string, bool) {
		if group == old || strings.HasPrefix(group, old+"/") {
			return newPath + group[len(old):], true
		}
		return group, false
	}
	for i := range m.workspace.groups {
		m.workspace.groups[i], _ = moved(m.workspace.groups[i])
	}
	for i := range m.workspace.sessions {
		m.workspace.sessions[i].Group, _ = moved(m.workspace.sessions[i].Group)
	}
	groupPaths := make(map[string]string, len(m.workspace.groupPaths))
	for group, path := range m.workspace.groupPaths {
		group, _ = moved(group)
		groupPaths[group] = path
	}
	m.workspace.groupPaths = groupPaths
	groupWorktrees := make(map[string]string, len(m.workspace.groupWorktrees))
	for group, choice := range m.workspace.groupWorktrees {
		group, _ = moved(group)
		groupWorktrees[group] = choice
	}
	m.workspace.groupWorktrees = groupWorktrees
}
