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
		if m.isShell(row.sess.Tool) {
			m.appendAgentMoveTargets()
		}
	}
	m.mode = modeMove
	m.errBar.text = ""
}

func (m *Model) appendAgentMoveTargets() {
	selected := m.form.groups[m.form.groupIndex].path
	var options []groupOption
	for _, opt := range m.form.groups {
		options = append(options, opt)
		for _, sess := range m.sessions {
			if sess.Archived || m.isShell(sess.Tool) || sess.Group != opt.path {
				continue
			}
			options = append(options, groupOption{
				path:   sess.Group,
				depth:  opt.depth + 1,
				sessID: sess.ID,
				name:   sess.Name,
			})
		}
	}
	m.form.groups = options
	m.form.groupIndex = 0
	for i, opt := range options {
		if opt.path == selected && opt.sessID == "" {
			m.form.groupIndex = i
			return
		}
	}
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
		if m.movePath != "" {
			return m.moveGroupTo(m.selectedGroupPath())
		}
		opt := m.form.groups[m.form.groupIndex]
		sess, err := m.store.Get(m.moveID)
		if err != nil {
			m.errBar.text = err.Error()
			return m, nil
		}
		if sess.ParentID == opt.sessID && sess.Group == opt.path {
			m.mode = modeList
			return m, nil
		}
		if err := m.store.PlaceSession(m.moveID, opt.path, opt.sessID); err != nil {
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
