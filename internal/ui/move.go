package ui

import (
	"strings"

	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	tea "github.com/charmbracelet/bubbletea"
)

type moveTarget struct {
	id   string
	path string
}

// moveDialog picks a new parent for the selected session or group. It owns
// the captured row, the picker keys and the Rail mutation it submits. Its
// target list is the spawn form's group picker, which the root rebuilds and
// shapes when the dialog opens; the root runs the mutation on the effect
// lane and closes the dialog from its fenced follow-up.
type moveDialog struct{ moveTarget }

// moveHost is what the move dialog reaches on the root: the borrowed group
// picker, mode changes, the effect lane and the card chrome.
type moveHost interface {
	setMode(next mode)
	moveGroupCursor(delta int) tea.Cmd
	pickedGroupOption() groupOption
	selectedGroupPath() string
	enqueueMove(mut uirail.Mutation, close moveDialogClose) tea.Cmd
	card(title, body string, hint [][2]string) string
	viewGroupPicker() string
}

var _ moveHost = (*Model)(nil)

// pickedGroupOption is the group picker's highlighted row.
func (m *Model) pickedGroupOption() groupOption {
	return m.form.groups[m.form.groupIndex]
}

// openMove captures the selected row and shapes the spawn form's group
// picker into the targets it may land in. It stays on the root because it
// rebuilds that borrowed picker from the workspace inventory.
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

func (d *moveDialog) handleKey(h moveHost, msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		h.setMode(modeList)
		return nil
	case "up":
		return h.moveGroupCursor(-1)
	case "down":
		return h.moveGroupCursor(1)
	case "enter":
		if d.path != "" {
			return d.moveGroupTo(h, h.selectedGroupPath())
		}
		opt := h.pickedGroupOption()
		return h.enqueueMove(uirail.Mutation{Kind: uirail.PlaceSession, SessionID: d.id, Group: opt.path, ParentID: opt.sessID}, moveDialogClose{sessID: d.id, optPath: opt.path, optSessID: opt.sessID})
	}
	return nil
}

func (d *moveDialog) moveGroupTo(h moveHost, parent string) tea.Cmd {
	path := d.path
	newPath := baseName(path)
	if parent != "" {
		newPath = parent + "/" + newPath
	}
	if newPath == path {
		h.setMode(modeList)
		return nil
	}
	return h.enqueueMove(uirail.Mutation{Kind: uirail.MoveGroup, Path: path, Group: parent}, moveDialogClose{isGroup: true, group: path, parent: parent})
}
