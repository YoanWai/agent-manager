package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) openMove() {
	row, ok := m.selectedRow()
	if !ok {
		return
	}
	if row.isRoot() {
		m.errBar.text = "root is the top level, not a group to move"
		return
	}
	if row.isGroup {
		m.moveID = ""
		m.movePath = row.group
		m.rebuildGroupOptions(parentGroup(row.group))
		m.pruneMoveTargets(row.group)
	} else {
		m.moveID = row.sess.ID
		m.movePath = ""
		m.rebuildGroupOptions(row.sess.Group)
	}
	m.mode = modeMove
	m.errBar.text = ""
}

// pruneMoveTargets drops the moved group and its descendants from the
// picker: a group cannot land inside its own subtree.
func (m *Model) pruneMoveTargets(subtree string) {
	selected := m.form.groups[m.form.groupIndex].path
	options := make([]groupOption, 0, len(m.form.groups))
	for _, opt := range m.form.groups {
		if opt.path == subtree || strings.HasPrefix(opt.path, subtree+"/") {
			continue
		}
		options = append(options, opt)
	}
	m.form.groups = options
	m.form.groupIndex = 0
	for i, opt := range options {
		if opt.path == selected {
			m.form.groupIndex = i
			return
		}
	}
}

func (m *Model) handleMoveKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeList
		return m, nil
	case "up":
		m.moveGroupCursor(-1)
		return m, nil
	case "down":
		m.moveGroupCursor(1)
		return m, nil
	case "enter":
		group := m.selectedGroupPath()
		if m.movePath != "" {
			return m.moveGroupTo(group)
		}
		if err := m.store.MoveSession(m.moveID, group); err != nil {
			m.errBar.text = err.Error()
			return m, nil
		}
		m.relabelSession(m.moveID)
		m.mode = modeList
		m.requestRefresh()
		return m, nil
	}
	return m, nil
}

func (m *Model) moveGroupTo(parent string) (tea.Model, tea.Cmd) {
	newPath := baseName(m.movePath)
	if parent != "" {
		newPath = parent + "/" + newPath
	}
	if newPath == m.movePath {
		m.mode = modeList
		return m, nil
	}
	if err := m.store.MoveGroup(m.movePath, parent); err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
	m.renameGroupLocally(m.movePath, newPath, m.groupPaths[m.movePath], m.groupWorktrees[m.movePath])
	m.relabelSubtree(newPath)
	m.rebuildRows()
	m.mode = modeList
	m.requestRefresh()
	return m, nil
}
