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

const (
	gfName = iota
	gfParent
	gfPath
	gfCount
)

type groupOption struct {
	path  string
	depth int
}

type form struct {
	name       textinput.Model
	dir        textinput.Model
	dirAuto    bool
	toolNames  []string
	toolIndex  int
	groups     []groupOption
	groupIndex int
	focus      int
}

type groupForm struct {
	name     textinput.Model
	path     textinput.Model
	pathAuto bool
	focus    int
}

// sessionLabel renders a session's identity for the tmux status bar.
func sessionLabel(group, name string) string {
	if group == "" {
		return name
	}
	return group + " · " + name
}

func textField(placeholder string, limit int) textinput.Model {
	in := textinput.New()
	in.Placeholder = placeholder
	in.CharLimit = limit
	return in
}

// contextGroup is the group the cursor currently sits in: a highlighted
// group row itself, or the group holding a highlighted session.
func (m *Model) contextGroup() string {
	if r, ok := m.selectedRow(); ok {
		if r.isGroup {
			return r.group
		}
		return r.sess.Group
	}
	return ""
}

// ancestorGroupPath finds the closest configured default path walking up
// from the group to the root; empty when no ancestor has one.
func (m *Model) ancestorGroupPath(group string) string {
	for g := group; g != ""; {
		if p := m.groupPaths[g]; p != "" {
			if info, err := os.Stat(p); err == nil && info.IsDir() {
				return p
			}
		}
		idx := strings.LastIndex(g, "/")
		if idx < 0 {
			break
		}
		g = g[:idx]
	}
	return ""
}

// groupDefaultDir resolves the working directory for a session in a group:
// the nearest inherited default path, else the current directory.
func (m *Model) groupDefaultDir(group string) string {
	if p := m.ancestorGroupPath(group); p != "" {
		return p
	}
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return cwd
}

func (m *Model) openForm() {
	tools := m.cfg.ToolNames()
	sort.Strings(tools)

	name := textField("my-session", 60)
	name.Focus()

	dir := textField("", 400)

	m.form = form{
		name:      name,
		dir:       dir,
		dirAuto:   true,
		toolNames: tools,
		focus:     fieldName,
	}
	m.rebuildGroupOptions(m.contextGroup())
	m.form.dir.SetValue(m.groupDefaultDir(m.selectedGroupPath()))
	m.pathSugg.reset()
	m.mode = modeForm
	m.err = ""
}

func (m *Model) selectedGroupPath() string {
	if m.form.groupIndex >= 0 && m.form.groupIndex < len(m.form.groups) {
		return m.form.groups[m.form.groupIndex].path
	}
	return ""
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
	m.form.groupIndex = 0
	for i, opt := range options {
		if selectPath != "" && opt.path == selectPath {
			m.form.groupIndex = i
			return
		}
	}
}

func (m *Model) handleFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	dirSuggesting := m.form.focus == fieldDir && m.pathSugg.active()
	switch msg.String() {
	case "esc":
		if dirSuggesting {
			m.pathSugg.reset()
			return m, nil
		}
		m.mode = modeList
		return m, nil
	case "tab":
		if dirSuggesting {
			m.applyPathSuggestion()
			return m, nil
		}
		m.formFocus(1)
		return m, nil
	case "shift+tab":
		m.formFocus(-1)
		return m, nil
	case "up":
		if m.form.focus == fieldGroup {
			m.moveGroupCursor(-1)
		} else if dirSuggesting {
			m.pathSugg.move(-1)
		} else {
			m.formFocus(-1)
		}
		return m, nil
	case "down":
		if m.form.focus == fieldGroup {
			m.moveGroupCursor(1)
		} else if dirSuggesting {
			m.pathSugg.move(1)
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
	case "enter":
		if dirSuggesting {
			m.applyPathSuggestion()
			return m, nil
		}
		return m.submitForm()
	}

	var cmd tea.Cmd
	switch m.form.focus {
	case fieldName:
		m.form.name, cmd = m.form.name.Update(msg)
	case fieldDir:
		m.form.dir, cmd = m.form.dir.Update(msg)
		m.form.dirAuto = false
		m.pathSugg.recompute(m.form.dir.Value())
	}
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
	if m.mode == modeForm && m.form.dirAuto {
		m.form.dir.SetValue(m.groupDefaultDir(m.selectedGroupPath()))
	}
	if m.mode == modeGroupForm && m.groupForm.pathAuto {
		m.groupForm.path.SetValue(m.ancestorGroupPath(m.selectedGroupPath()))
	}
}

func (m *Model) formFocus(delta int) {
	m.pathSugg.reset()
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
	dir := expandHome(strings.TrimSpace(m.form.dir.Value()))
	if dir == "" {
		dir, _ = os.Getwd()
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		m.err = "working directory does not exist: " + dir
		return m, nil
	}
	group := m.selectedGroupPath()

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
	if err := m.tmux.SetLabel(id, sessionLabel(group, name)); err != nil {
		m.err = err.Error()
	}
	m.mode = modeList
	return m, m.refreshCmd()
}

func (m *Model) openGroupForm() {
	name := textField("group-name", 60)
	name.Focus()
	m.groupForm = groupForm{
		name:     name,
		path:     textField("default working directory (optional)", 400),
		pathAuto: true,
		focus:    gfName,
	}
	m.rebuildGroupOptions(m.contextGroup())
	m.groupForm.path.SetValue(m.ancestorGroupPath(m.selectedGroupPath()))
	m.pathSugg.reset()
	m.mode = modeGroupForm
	m.err = ""
}

func (m *Model) handleGroupFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	pathSuggesting := m.groupForm.focus == gfPath && m.pathSugg.active()
	switch msg.String() {
	case "esc":
		if pathSuggesting {
			m.pathSugg.reset()
			return m, nil
		}
		m.mode = modeList
		return m, nil
	case "tab":
		if pathSuggesting {
			m.applyPathSuggestion()
			return m, nil
		}
		m.groupFormFocus(1)
		return m, nil
	case "shift+tab":
		m.groupFormFocus(-1)
		return m, nil
	case "up":
		if m.groupForm.focus == gfParent {
			m.moveGroupCursor(-1)
		} else if pathSuggesting {
			m.pathSugg.move(-1)
		} else {
			m.groupFormFocus(-1)
		}
		return m, nil
	case "down":
		if m.groupForm.focus == gfParent {
			m.moveGroupCursor(1)
		} else if pathSuggesting {
			m.pathSugg.move(1)
		} else {
			m.groupFormFocus(1)
		}
		return m, nil
	case "enter":
		if pathSuggesting {
			m.applyPathSuggestion()
			return m, nil
		}
		return m.submitGroupForm()
	}

	var cmd tea.Cmd
	switch m.groupForm.focus {
	case gfName:
		m.groupForm.name, cmd = m.groupForm.name.Update(msg)
	case gfPath:
		m.groupForm.path, cmd = m.groupForm.path.Update(msg)
		m.groupForm.pathAuto = false
		m.pathSugg.recompute(m.groupForm.path.Value())
	}
	return m, cmd
}

func (m *Model) groupFormFocus(delta int) {
	m.pathSugg.reset()
	m.groupForm.focus = (m.groupForm.focus + delta + gfCount) % gfCount
	m.groupForm.name.Blur()
	m.groupForm.path.Blur()
	switch m.groupForm.focus {
	case gfName:
		m.groupForm.name.Focus()
	case gfPath:
		m.groupForm.path.Focus()
	}
}

func (m *Model) submitGroupForm() (tea.Model, tea.Cmd) {
	name := strings.TrimSpace(m.groupForm.name.Value())
	name = strings.ReplaceAll(name, "/", "-")
	if name == "" {
		m.err = "group name cannot be empty"
		return m, nil
	}
	parent := m.selectedGroupPath()
	full := name
	if parent != "" {
		full = parent + "/" + name
	}
	path := expandHome(strings.TrimSpace(m.groupForm.path.Value()))
	if path != "" {
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			m.err = "default path does not exist: " + path
			return m, nil
		}
	}
	if err := m.store.CreateGroup(full, path); err != nil {
		m.err = err.Error()
		return m, nil
	}
	m.mode = modeList
	return m, m.refreshCmd()
}
