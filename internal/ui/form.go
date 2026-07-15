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

type form struct {
	name      textinput.Model
	dir       textinput.Model
	group     textinput.Model
	toolNames []string
	toolIndex int
	focus     int
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

	group := textinput.New()
	group.SetValue(m.cfg.DefaultGroup)
	group.CharLimit = 60

	m.form = form{
		name:      name,
		dir:       dir,
		group:     group,
		toolNames: tools,
		toolIndex: 0,
		focus:     fieldName,
	}
	m.mode = modeForm
	m.err = ""
}

func (m *Model) handleFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeList
		return m, nil
	case "tab", "down":
		m.formFocus(1)
		return m, nil
	case "shift+tab", "up":
		m.formFocus(-1)
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
	case "enter":
		return m.submitForm()
	}

	var cmd tea.Cmd
	switch m.form.focus {
	case fieldName:
		m.form.name, cmd = m.form.name.Update(msg)
	case fieldDir:
		m.form.dir, cmd = m.form.dir.Update(msg)
	case fieldGroup:
		m.form.group, cmd = m.form.group.Update(msg)
	}
	return m, cmd
}

func (m *Model) formFocus(delta int) {
	m.form.focus = (m.form.focus + delta + fieldCount) % fieldCount
	m.form.name.Blur()
	m.form.dir.Blur()
	m.form.group.Blur()
	switch m.form.focus {
	case fieldName:
		m.form.name.Focus()
	case fieldDir:
		m.form.dir.Focus()
	case fieldGroup:
		m.form.group.Focus()
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
	group := strings.TrimSpace(m.form.group.Value())
	if group == "" {
		group = m.cfg.DefaultGroup
	}

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
