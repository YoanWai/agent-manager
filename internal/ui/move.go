package ui

import (
	"strings"

	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) openMove() {
	row, ok := m.selectedRow()
	if !ok {
		return
	}
	if row.isRoot() {
		m.reportErr("root is the top level, not a group to move")
		return
	}
	if row.isGroup {
		m.move.id = ""
		m.move.path = row.group
		m.rebuildGroupOptions(parentGroup(row.group))
		m.pruneMoveTargets(row.group)
	} else {
		m.move.id = row.sess.ID
		m.move.path = ""
		m.rebuildGroupOptions(row.sess.Group)
		if m.isShell(row.sess.Tool) {
			m.appendAgentMoveTargets()
		}
	}
	m.gens.dialog++
	m.mode = modeMove
	m.clearErr()
}

func (m *Model) appendAgentMoveTargets() {
	selected := m.form.groups[m.form.groupIndex].path
	var options []groupOption
	for _, opt := range m.form.groups {
		options = append(options, opt)
		for _, sess := range m.workspace.sessions {
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
		return m, m.moveGroupCursor(-1)
	case "down":
		return m, m.moveGroupCursor(1)
	case "enter":
		if m.move.path != "" {
			return m.moveGroupTo(m.selectedGroupPath())
		}
		opt := m.form.groups[m.form.groupIndex]
		return m, m.enqueueMove(uirail.Mutation{Kind: uirail.PlaceSession, SessionID: m.move.id, Group: opt.path, ParentID: opt.sessID}, moveDialogClose{sessID: m.move.id, optPath: opt.path, optSessID: opt.sessID})
	}
	return m, nil
}

func (m *Model) moveGroupTo(parent string) (tea.Model, tea.Cmd) {
	path := m.move.path
	newPath := baseName(path)
	if parent != "" {
		newPath = parent + "/" + newPath
	}
	if newPath == path {
		m.mode = modeList
		return m, nil
	}
	return m, m.enqueueMove(uirail.Mutation{Kind: uirail.MoveGroup, Path: path, Group: parent}, moveDialogClose{isGroup: true, group: path, parent: parent})
}
