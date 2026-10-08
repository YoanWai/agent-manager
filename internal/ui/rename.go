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
	base          string
	focus         int
	toolNames     []string
	toolIndex     int
}

func (m *Model) openRename() {
	m.gens.dialog++
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
			dirValue = m.capturedGroupDefaultDir(entry.group)
		}
		dir.SetValue(dirValue)
		m.pathSugg.reset()
		m.rename = renameTarget{
			isGroup:       true,
			path:          entry.group,
			input:         input,
			dir:           dir,
			worktreeIndex: groupWorktreeIndex(m.workspace.groupWorktrees[entry.group]),
			base:          m.workspace.groupBases[entry.group],
		}
	} else {
		input.SetValue(entry.sess.Name)
		tools := m.services.cfg.AgentToolNames()
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
		fields = 4
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
			return m, m.applyPathSuggestion()
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
		delta := 1
		if msg.String() == "left" {
			delta = -1
		}
		if m.rename.isGroup && m.rename.focus == 2 {
			count := len(groupWorktreeOptions)
			m.rename.worktreeIndex = (m.rename.worktreeIndex + delta + count) % count
			return m, nil
		}
		if m.rename.isGroup && m.rename.focus == 3 {
			var cmd tea.Cmd
			m.rename.base, cmd = m.stepGroupBase(groupBaseRename, m.gens.dialog, m.renameGroupDir(), m.rename.base, delta)
			return m, cmd
		}
	case "enter":
		if pathSuggesting && m.pathSugg.chosen {
			return m, m.applyPathSuggestion()
		}
		return m.applyRename()
	}
	var cmd tea.Cmd
	switch m.rename.focus {
	case 0:
		m.rename.input, cmd = m.rename.input.Update(msg)
	case 1:
		m.rename.dir, cmd = m.rename.dir.Update(msg)
		cmd = tea.Batch(cmd, m.requestPathSuggestions(pathSuggestionRename, m.rename.dir.Value()))
	}
	return m, cmd
}

// renameGroupDir is the default path the group edit would save, resolved
// the way the rename worker resolves it.
func (m *Model) renameGroupDir() string {
	return m.capturedAbsolutePath(m.rename.dir.Value(), m.capturedGroupDefaultDir(parentGroup(m.rename.path)))
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
		newPath := name
		if parent != "" {
			newPath = parent + "/" + name
		}
		m.gens.dialog++
		m.enqueueEffect(renameRequest{
			kind:         renameGroup,
			oldGroup:     m.rename.path,
			newGroup:     newPath,
			rawDir:       m.rename.dir.Value(),
			dirFallbacks: m.groupDirCandidates(parent),
			draftDir:     m.rename.dir.Value(),
			worktree:     groupWorktreeValue(m.rename.worktreeIndex),
			base:         m.rename.base,
			name:         name,
			gen:          m.gens.dialog,
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
	m.gens.dialog++
	m.enqueueEffect(renameRequest{
		kind:            renameSession,
		sessID:          m.rename.sessID,
		sess:            sess,
		name:            name,
		tool:            tool,
		checkNoChildren: toolChanged && m.isShell(tool),
		gen:             m.gens.dialog,
	}, 0, false)
	return m, m.nextEffectCmd()
}

// renameGroupLocally rewrites the in-memory tree right away, so the
// frames between saving and the poller's next refresh already show the
// new name and path instead of flashing the stale ones.
func (m *Model) renameGroupLocally(old, newPath, dir, worktree, base string) {
	m.renameGroupInventory(old, newPath)
	m.workspace.groupPaths[newPath] = dir
	m.workspace.groupWorktrees = setGroupChoice(m.workspace.groupWorktrees, newPath, worktree)
	m.workspace.groupBases = setGroupChoice(m.workspace.groupBases, newPath, base)
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
	m.workspace.groupWorktrees = movedChoices(m.workspace.groupWorktrees, moved)
	m.workspace.groupBases = movedChoices(m.workspace.groupBases, moved)
}

// movedChoices rekeys a per-group choice map for a renamed subtree.
func movedChoices(choices map[string]string, moved func(string) (string, bool)) map[string]string {
	out := make(map[string]string, len(choices))
	for group, choice := range choices {
		group, _ = moved(group)
		out[group] = choice
	}
	return out
}

// setGroupChoice records a group's own choice, "" dropping it to inherit.
func setGroupChoice(choices map[string]string, group, value string) map[string]string {
	if choices == nil {
		choices = map[string]string{}
	}
	if value == "" {
		delete(choices, group)
	} else {
		choices[group] = value
	}
	return choices
}
