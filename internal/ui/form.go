package ui

import (
	"os"
	"sort"
	"strings"

	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	fieldName = iota
	fieldTool
	fieldDir
	fieldGroup
	fieldCount
)

type groupOption struct {
	path  string
	depth int
}

func newGroupInput() textinput.Model {
	input := textinput.New()
	input.Placeholder = "group-name"
	input.CharLimit = 60
	return input
}

type form struct {
	name          textinput.Model
	dir           textinput.Model
	newGroup      textinput.Model
	toolNames     []string
	toolIndex     int
	groups        []groupOption
	groupIndex    int
	creatingGroup bool
	focus         int
}

func (m *Model) openForm() {
	tools := m.cfg.ToolNames()
	sort.Strings(tools)

	name := textinput.New()
	name.Placeholder = "my-session"
	name.CharLimit = 60
	name.Focus()

	dir := textinput.New()
	cwd, err := os.Getwd()
	if err != nil {
		cwd = ""
	}
	dir.SetValue(cwd)
	dir.CharLimit = 400

	newGroup := newGroupInput()

	m.form = form{
		name:      name,
		dir:       dir,
		newGroup:  newGroup,
		toolNames: tools,
		focus:     fieldName,
	}
	m.rebuildGroupOptions("")
	m.mode = modeForm
	m.err = ""
}

// rebuildGroupOptions flattens the group tree into picker rows.
// Index 0 is always the root; selectPath moves the highlight when given.
func (m *Model) rebuildGroupOptions(selectPath string) {
	paths := groupClosure(m.groups, m.sessions)
	children := childIndex(paths)

	options := []groupOption{{path: "", depth: 0}}
	var walk func(path string, depth int)
	walk = func(path string, depth int) {
		options = append(options, groupOption{path: path, depth: depth})
		for _, child := range children[path] {
			walk(child, depth+1)
		}
	}
	for _, root := range children[""] {
		walk(root, 1)
	}

	m.form.groups = options
	for i, opt := range options {
		if selectPath != "" && opt.path == selectPath {
			m.form.groupIndex = i
			return
		}
	}
	if m.form.groupIndex >= len(options) {
		m.form.groupIndex = 0
	}
}

func (m *Model) handleFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.form.creatingGroup {
		return m.handleNewGroupKey(msg)
	}

	switch msg.String() {
	case "esc":
		m.mode = modeList
		return m, nil
	case "tab":
		m.formFocus(1)
		return m, nil
	case "shift+tab":
		m.formFocus(-1)
		return m, nil
	case "up":
		if m.form.focus == fieldGroup {
			m.moveGroupCursor(-1)
		} else {
			m.formFocus(-1)
		}
		return m, nil
	case "down":
		if m.form.focus == fieldGroup {
			m.moveGroupCursor(1)
		} else {
			m.formFocus(1)
		}
		return m, nil
	case "left":
		if m.form.focus == fieldTool {
			m.cycleTool(-1)
			return m, nil
		}
	case "right":
		if m.form.focus == fieldTool {
			m.cycleTool(1)
			return m, nil
		}
	case "n":
		if m.form.focus == fieldGroup {
			m.form.creatingGroup = true
			m.form.newGroup.SetValue("")
			m.form.newGroup.Focus()
			return m, nil
		}
	case "enter":
		return m.submitForm()
	}

	var cmd tea.Cmd
	switch m.form.focus {
	case fieldName:
		m.form.name, cmd = m.form.name.Update(msg)
	case fieldDir:
		m.form.dir, cmd = m.form.dir.Update(msg)
	}
	return m, cmd
}

func (m *Model) handleNewGroupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.form.creatingGroup = false
		return m, nil
	case "enter":
		name := strings.TrimSpace(m.form.newGroup.Value())
		name = strings.ReplaceAll(name, "/", "-")
		if name == "" {
			m.form.creatingGroup = false
			return m, nil
		}
		parent := m.form.groups[m.form.groupIndex].path
		path := name
		if parent != "" {
			path = parent + "/" + name
		}
		if err := m.store.CreateGroup(path); err != nil {
			m.err = err.Error()
			m.form.creatingGroup = false
			return m, nil
		}
		groups, err := m.store.Groups()
		if err != nil {
			m.err = err.Error()
			m.form.creatingGroup = false
			return m, nil
		}
		m.groups = groups
		m.form.creatingGroup = false
		m.rebuildGroupOptions(path)
		return m, nil
	}
	var cmd tea.Cmd
	m.form.newGroup, cmd = m.form.newGroup.Update(msg)
	return m, cmd
}

func (m *Model) moveGroupCursor(delta int) {
	m.form.groupIndex += delta
	if m.form.groupIndex < 0 {
		m.form.groupIndex = 0
	}
	if m.form.groupIndex >= len(m.form.groups) {
		m.form.groupIndex = len(m.form.groups) - 1
	}
}

func (m *Model) formFocus(delta int) {
	m.form.focus = (m.form.focus + delta + fieldCount) % fieldCount
	m.form.name.Blur()
	m.form.dir.Blur()
	switch m.form.focus {
	case fieldName:
		m.form.name.Focus()
	case fieldDir:
		m.form.dir.Focus()
	}
}

func (m *Model) cycleTool(delta int) {
	if len(m.form.toolNames) == 0 {
		return
	}
	m.form.toolIndex = (m.form.toolIndex + delta + len(m.form.toolNames)) % len(m.form.toolNames)
}

func (m *Model) submitForm() (tea.Model, tea.Cmd) {
	if len(m.form.toolNames) == 0 {
		m.err = "no tools configured"
		m.mode = modeList
		return m, nil
	}
	toolName := m.form.toolNames[m.form.toolIndex]
	tool := m.cfg.Tools[toolName]

	name := strings.TrimSpace(m.form.name.Value())
	if name == "" {
		name = toolName + "-" + newID()[:4]
	}
	dir := strings.TrimSpace(m.form.dir.Value())
	if dir == "" {
		dir, _ = os.Getwd()
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		m.err = "working directory does not exist: " + dir
		return m, nil
	}
	group := m.form.groups[m.form.groupIndex].path

	id := newID()
	if err := m.tmux.Create(id, dir, tool.Command); err != nil {
		m.err = err.Error()
		return m, nil
	}
	sess := store.Session{
		ID:     id,
		Name:   name,
		Tool:   toolName,
		Cwd:    dir,
		Group:  group,
		Status: tool.DefaultStatus,
	}
	if err := m.store.CreateSession(sess); err != nil {
		_ = m.tmux.Kill(id)
		m.err = err.Error()
		return m, nil
	}
	m.mode = modeList
	return m, m.refreshCmd()
}
