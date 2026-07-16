package ui

import (
	"fmt"
	"strings"

	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeForm:
		return m.handleFormKey(msg)
	case modeConfirmDelete:
		return m.handleConfirmKey(msg)
	case modeRename:
		return m.handleRenameKey(msg)
	case modeMove:
		return m.handleMoveKey(msg)
	case modeHelp:
		m.mode = modeList
		return m, nil
	}

	if m.searching {
		return m.handleSearchKey(msg)
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		m.moveCursor(-1)
	case "down", "j":
		m.moveCursor(1)
	case "enter":
		if r, ok := m.selectedRow(); ok && r.isGroup {
			m.toggleCollapse()
			return m, nil
		}
		return m.attachSelected()
	case "n":
		m.openForm()
	case "a":
		return m.archiveSelected()
	case "u":
		return m.restoreSelected()
	case "d":
		m.prepareDelete()
	case "space":
		m.toggleCollapse()
	case "t":
		m.showArchived = !m.showArchived
		return m, m.refreshCmd()
	case "/":
		m.searching = true
		m.err = ""
	case "r":
		m.openRename()
	case "m":
		m.openMove()
	case "ctrl+r":
		return m, m.refreshCmd()
	case "?":
		m.mode = modeHelp
	}
	return m, nil
}

func (m *Model) moveCursor(delta int) {
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
}

func (m *Model) toggleCollapse() {
	r, ok := m.selectedRow()
	if !ok {
		return
	}
	path := r.group
	if !r.isGroup {
		path = r.sess.Group
	}
	if path == "" {
		return
	}
	m.collapsed[path] = !m.collapsed[path]
	m.rebuildRows()
}

func (m *Model) attachSelected() (tea.Model, tea.Cmd) {
	sess, ok := m.selected()
	if !ok {
		return m, nil
	}
	if !m.tmux.Exists(sess.ID) {
		m.err = "session is not running (dead); delete or recreate it"
		return m, nil
	}
	m.err = ""
	cmd := m.tmux.AttachCommand(sess.ID)
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		return attachDoneMsg{err}
	})
}

func (m *Model) archiveSelected() (tea.Model, tea.Cmd) {
	sess, ok := m.selected()
	if !ok {
		return m, nil
	}
	if err := m.store.SetArchived(sess.ID, true); err != nil {
		m.err = err.Error()
		return m, nil
	}
	return m, m.refreshCmd()
}

func (m *Model) restoreSelected() (tea.Model, tea.Cmd) {
	sess, ok := m.selected()
	if !ok {
		return m, nil
	}
	if err := m.store.SetArchived(sess.ID, false); err != nil {
		m.err = err.Error()
		return m, nil
	}
	return m, m.refreshCmd()
}

func (m *Model) prepareDelete() {
	r, ok := m.selectedRow()
	if !ok {
		return
	}
	if !r.isGroup {
		m.confirm = confirmTarget{
			label:    "delete " + r.sess.Name + "? kills its tmux session. (y/n)",
			sessions: []store.Session{r.sess},
		}
		m.mode = modeConfirmDelete
		return
	}
	subtree, err := m.store.SessionsInSubtree(r.group)
	if err != nil {
		m.err = err.Error()
		return
	}
	subgroups := 0
	for _, g := range m.groups {
		if strings.HasPrefix(g, r.group+"/") {
			subgroups++
		}
	}
	label := fmt.Sprintf("delete group %s (%d subgroups, %d sessions incl. archived)? kills their tmux sessions. (y/n)",
		r.group, subgroups, len(subtree))
	m.confirm = confirmTarget{isGroup: true, path: r.group, label: label, sessions: subtree}
	m.mode = modeConfirmDelete
}

func (m *Model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	defer func() { m.mode = modeList }()
	switch msg.String() {
	case "y", "enter":
		for _, sess := range m.confirm.sessions {
			if err := m.tmux.Kill(sess.ID); err != nil {
				m.err = err.Error()
				return m, nil
			}
			if err := m.store.Delete(sess.ID); err != nil {
				m.err = err.Error()
				return m, nil
			}
		}
		if m.confirm.isGroup {
			if err := m.store.DeleteGroup(m.confirm.path); err != nil {
				m.err = err.Error()
				return m, nil
			}
			for path := range m.collapsed {
				if path == m.confirm.path || strings.HasPrefix(path, m.confirm.path+"/") {
					delete(m.collapsed, path)
				}
			}
		}
		m.confirm = confirmTarget{}
		return m, m.refreshCmd()
	}
	m.confirm = confirmTarget{}
	return m, nil
}

func (m *Model) openRename() {
	r, ok := m.selectedRow()
	if !ok {
		return
	}
	input := textinput.New()
	input.CharLimit = 60
	input.Focus()
	if r.isGroup {
		input.SetValue(baseName(r.group))
		m.rename = renameTarget{isGroup: true, path: r.group, input: input}
	} else {
		input.SetValue(r.sess.Name)
		m.rename = renameTarget{sessID: r.sess.ID, input: input}
	}
	m.mode = modeRename
	m.err = ""
}

func (m *Model) handleRenameKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeList
		return m, nil
	case "enter":
		name := strings.TrimSpace(m.rename.input.Value())
		name = strings.ReplaceAll(name, "/", "-")
		if name == "" {
			m.err = "name cannot be empty"
			return m, nil
		}
		if m.rename.isGroup {
			newPath := name
			if idx := strings.LastIndex(m.rename.path, "/"); idx >= 0 {
				newPath = m.rename.path[:idx] + "/" + name
			}
			if err := m.store.RenameGroup(m.rename.path, newPath); err != nil {
				m.err = err.Error()
				return m, nil
			}
			if m.collapsed[m.rename.path] {
				delete(m.collapsed, m.rename.path)
				m.collapsed[newPath] = true
			}
			m.relabelSubtree(newPath)
		} else {
			if err := m.store.RenameSession(m.rename.sessID, name); err != nil {
				m.err = err.Error()
				return m, nil
			}
			m.relabelSession(m.rename.sessID)
		}
		m.mode = modeList
		return m, m.refreshCmd()
	}
	var cmd tea.Cmd
	m.rename.input, cmd = m.rename.input.Update(msg)
	return m, cmd
}

func (m *Model) openMove() {
	sess, ok := m.selected()
	if !ok {
		return
	}
	m.moveID = sess.ID
	m.form.newGroup = newGroupInput()
	m.rebuildGroupOptions(sess.Group)
	m.mode = modeMove
	m.err = ""
}

func (m *Model) handleMoveKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.form.creatingGroup {
		return m.handleNewGroupKey(msg)
	}
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
	case "n":
		m.form.creatingGroup = true
		m.form.newGroup.SetValue("")
		m.form.newGroup.Focus()
		return m, nil
	case "enter":
		group := m.form.groups[m.form.groupIndex].path
		if err := m.store.MoveSession(m.moveID, group); err != nil {
			m.err = err.Error()
			return m, nil
		}
		m.relabelSession(m.moveID)
		m.mode = modeList
		return m, m.refreshCmd()
	}
	return m, nil
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

func (m *Model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter", "esc":
		m.searching = false
	case "backspace":
		if len(m.search) > 0 {
			m.search = m.search[:len(m.search)-1]
		}
		m.rebuildRows()
	default:
		if len(msg.String()) == 1 {
			m.search += msg.String()
			m.rebuildRows()
		}
	}
	return m, nil
}
