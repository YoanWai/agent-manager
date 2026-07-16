package ui

import (
	"fmt"
	"strings"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/sysstat"
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
	case modeGroupForm:
		return m.handleGroupFormKey(msg)
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
		return m, m.moveCursor(-1)
	case "down", "j":
		return m, m.moveCursor(1)
	case "shift+up":
		return m.reorderSelected(-1)
	case "shift+down":
		return m.reorderSelected(1)
	case "enter":
		if entry, ok := m.selectedRow(); ok && entry.isGroup {
			m.toggleCollapse()
			return m, nil
		}
		return m.attachSelected()
	case "n":
		m.openForm()
	case "g":
		m.openGroupForm()
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
		m.requestRefresh()
	case "/":
		m.searching = true
		m.err = ""
	case "r":
		m.openRename()
	case "m":
		m.openMove()
	case "ctrl+r":
		m.requestRefresh()
	case "?":
		m.mode = modeHelp
	}
	return m, nil
}

// moveCursor shifts the selection and kicks off an immediate preview
// fetch for the newly selected session, so the sidebar follows the
// cursor without waiting for the next poll tick.
func (m *Model) moveCursor(delta int) tea.Cmd {
	previous := m.cursor
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor == previous {
		return nil
	}
	sess, ok := m.selected()
	if !ok {
		m.preview = ""
		m.proc = sysstat.ProcStat{}
		m.procFor = ""
		return nil
	}
	m.preview = ""
	return m.previewCmd(sess.ID)
}

// reorderSelected moves the selected session among its group siblings,
// or the selected group among the groups sharing its parent.
func (m *Model) reorderSelected(delta int) (tea.Model, tea.Cmd) {
	entry, ok := m.selectedRow()
	if !ok {
		return m, nil
	}
	var moved bool
	var err error
	if entry.isGroup {
		moved, err = m.store.ReorderGroup(entry.group, delta)
	} else {
		moved, err = m.store.ReorderSession(entry.sess.ID, delta, m.showArchived)
	}
	if err != nil {
		m.err = err.Error()
		return m, nil
	}
	if !moved {
		edge := "top"
		if delta > 0 {
			edge = "bottom"
		}
		what := "group"
		if !entry.isGroup {
			what = "session"
		}
		m.err = fmt.Sprintf("%s already at the %s of its level", what, edge)
		return m, nil
	}
	// Mirror the swap in memory so the list redraws instantly; the next
	// poll re-reads the authoritative order from the store.
	if entry.isGroup {
		m.swapGroupLocal(entry.group, delta)
	} else {
		m.swapSessionLocal(entry.sess.ID, delta)
	}
	m.rebuildRows()
	m.requestRefresh()
	return m, nil
}

func (m *Model) swapSessionLocal(id string, delta int) {
	step := 1
	if delta < 0 {
		step = -1
	}
	for i, sess := range m.sessions {
		if sess.ID != id {
			continue
		}
		neighbor := i + step
		if neighbor >= 0 && neighbor < len(m.sessions) && m.sessions[neighbor].Group == sess.Group {
			m.sessions[i], m.sessions[neighbor] = m.sessions[neighbor], m.sessions[i]
		}
		return
	}
}

func (m *Model) swapGroupLocal(path string, delta int) {
	parent := ""
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		parent = path[:idx]
	}
	isSibling := func(name string) bool {
		if parent == "" {
			return !strings.Contains(name, "/")
		}
		rest, ok := strings.CutPrefix(name, parent+"/")
		return ok && !strings.Contains(rest, "/")
	}
	step := 1
	if delta < 0 {
		step = -1
	}
	current := -1
	for i, name := range m.groups {
		if name == path {
			current = i
			break
		}
	}
	if current < 0 {
		return
	}
	for i := current + step; i >= 0 && i < len(m.groups); i += step {
		if isSibling(m.groups[i]) {
			m.groups[current], m.groups[i] = m.groups[i], m.groups[current]
			return
		}
	}
}

func (m *Model) toggleCollapse() {
	entry, ok := m.selectedRow()
	if !ok {
		return
	}
	path := entry.group
	if !entry.isGroup {
		path = entry.sess.Group
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
	// Entering a finished session acknowledges the alert; the acked mark
	// keeps it idle while the pane still shows the acknowledged turn.
	if sess.Status == status.Finished {
		if err := m.store.UpdateStatus(sess.ID, status.Idle); err != nil {
			m.err = err.Error()
			return m, nil
		}
		if err := m.store.SetAcked(sess.ID, true); err != nil {
			m.err = err.Error()
			return m, nil
		}
	}
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
	m.requestRefresh()
	return m, nil
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
	m.requestRefresh()
	return m, nil
}

func (m *Model) prepareDelete() {
	entry, ok := m.selectedRow()
	if !ok {
		return
	}
	if !entry.isGroup {
		m.confirm = confirmTarget{
			label:    "delete " + entry.sess.Name + "? kills its tmux session. (y/n)",
			sessions: []store.Session{entry.sess},
		}
		m.mode = modeConfirmDelete
		return
	}
	subtree, err := m.store.SessionsInSubtree(entry.group)
	if err != nil {
		m.err = err.Error()
		return
	}
	subgroups := 0
	for _, g := range m.groups {
		if strings.HasPrefix(g, entry.group+"/") {
			subgroups++
		}
	}
	label := fmt.Sprintf("delete group %s (%d subgroups, %d sessions incl. archived)? kills their tmux sessions. (y/n)",
		entry.group, subgroups, len(subtree))
	m.confirm = confirmTarget{isGroup: true, path: entry.group, label: label, sessions: subtree}
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
			if err := m.hooks.Remove(sess.ID); err != nil {
				m.err = err.Error()
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
		m.requestRefresh()
		return m, nil
	}
	m.confirm = confirmTarget{}
	return m, nil
}

func (m *Model) openRename() {
	entry, ok := m.selectedRow()
	if !ok {
		return
	}
	input := textinput.New()
	input.CharLimit = 60
	input.Focus()
	if entry.isGroup {
		input.SetValue(baseName(entry.group))
		m.rename = renameTarget{isGroup: true, path: entry.group, input: input}
	} else {
		input.SetValue(entry.sess.Name)
		m.rename = renameTarget{sessID: entry.sess.ID, input: input}
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
		m.requestRefresh()
		return m, nil
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
	m.rebuildGroupOptions(sess.Group)
	m.mode = modeMove
	m.err = ""
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
		if err := m.store.MoveSession(m.moveID, group); err != nil {
			m.err = err.Error()
			return m, nil
		}
		m.relabelSession(m.moveID)
		m.mode = modeList
		m.requestRefresh()
		return m, nil
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
