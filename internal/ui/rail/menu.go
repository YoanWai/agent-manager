package rail

import (
	"github.com/YoanWai/agent-manager/internal/keybind"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) openMenu(selection Selection, x, y int, held bool) Decision {
	index := m.rowIndex(selection.key())
	if index < 0 {
		return Decision{Consumed: true}
	}
	previous := m.cursor
	m.cursor = index
	row := m.rows[index]
	title := groupLabel(row.group)
	if row.kind == SessionRow {
		title = sessionName(row.sess)
	}
	m.menu = rowMenu{
		active: true, held: held, key: row.key(), title: title,
		items: m.rowMenuItems(row), anchorX: x, anchorY: y,
	}
	m.menu.index = m.menu.nextItem(-1, 1)
	return previewSelectionDecision(previous != index, row.selection())
}

func (m Model) rowMenuItems(row treeRow) []menuItem {
	create := []menuItem{{label: "New session", action: NewSession}, {label: "New terminal", action: NewTerminal}}
	editor := menuItem{label: "Open in editor", action: OpenEditor}
	manage := []menuItem{editor, {label: "Rename", action: RenameAction}, {label: "Move to group", action: MoveToGroup}}
	revive := menuItem{label: "Revive", action: Revive}
	kill := menuItem{label: "Kill", action: Kill, danger: true}
	remove := menuItem{label: "Delete", action: Delete, danger: true}
	if row.isRoot() {
		return append(create, editor)
	}
	if row.kind == GroupRow {
		live, dead := m.groupLiveAndDead(row.group)
		var end []menuItem
		if dead {
			end = append(end, revive)
		}
		end = append(end, m.archiveMenuItem())
		if live {
			end = append(end, kill)
		}
		return menuSections(create, manage, append(end, remove))
	}
	if row.sess.Archived {
		return []menuItem{{label: "Restore", action: Restore}, remove}
	}
	if row.sess.Status == "dead" {
		return menuSections(manage, []menuItem{revive, m.archiveMenuItem(), remove})
	}
	var agent []menuItem
	if !row.sess.IsShell {
		agent = []menuItem{
			{label: "Prompt", action: Prompt},
			{label: "Copy last reply", action: CopyReply},
			{label: "Review changes", action: OpenReview},
			{label: "Fork", action: Fork},
			{label: "New terminal", action: NewTerminal},
		}
	}
	return menuSections(
		[]menuItem{{label: "Attach", action: Attach}},
		agent, manage,
		[]menuItem{{label: "Restart", action: Restart}, m.archiveMenuItem(), kill, remove},
	)
}

func menuSections(sections ...[]menuItem) []menuItem {
	var items []menuItem
	for _, section := range sections {
		if len(section) == 0 {
			continue
		}
		if len(items) > 0 {
			items = append(items, menuItem{})
		}
		items = append(items, section...)
	}
	return items
}

func (m Model) groupLiveAndDead(group string) (live, dead bool) {
	for _, session := range m.snapshot.Sessions {
		if session.Archived != m.showArchived || !inGroupSubtree(session.Group, group) {
			continue
		}
		if session.Status == "dead" {
			dead = true
		} else {
			live = true
		}
	}
	return live, dead
}

func (m Model) archiveMenuItem() menuItem {
	if m.showArchived {
		return menuItem{label: "Restore", action: Restore}
	}
	return menuItem{label: "Archive", action: Archive}
}

func (menu rowMenu) nextItem(index, step int) int {
	for next := index + step; next >= 0 && next < len(menu.items); next += step {
		if menu.items[next].label != "" {
			return next
		}
	}
	if index < 0 {
		return 0
	}
	return index
}

func (m *Model) runMenuItem(index int) Decision {
	if index < 0 || index >= len(m.menu.items) || m.menu.items[index].label == "" {
		return Decision{Consumed: true}
	}
	item, key := m.menu.items[index], m.menu.key
	m.menu = rowMenu{}
	rowIndex := m.rowIndex(key)
	if rowIndex < 0 {
		return Decision{Consumed: true}
	}
	previous := m.cursor
	m.cursor = rowIndex
	selected := m.rows[rowIndex].selection()
	decision := previewSelectionDecision(previous != rowIndex, selected)
	decision.Intent = Intent{Kind: item.action, Target: selected}
	return decision
}

func (m *Model) menuKey(msg tea.KeyMsg, ctx KeyContext) Decision {
	key := keybind.Normalize(msg.String())
	action, _ := ctx.ListKeys.ActionFor(key)
	switch {
	case key == "ctrl+c":
		m.menu = rowMenu{}
		return m.intent(Quit)
	case key == "esc":
		m.menu = rowMenu{}
		return Decision{Consumed: true}
	case key == "enter" || key == "space":
		return m.runMenuItem(m.menu.index)
	case key == "up" || action == keybind.Up:
		m.menu.index = m.menu.nextItem(m.menu.index, -1)
		return Decision{Consumed: true}
	case key == "down" || action == keybind.Down:
		m.menu.index = m.menu.nextItem(m.menu.index, 1)
		return Decision{Consumed: true}
	}
	if kind := actionKind(action); kind != NoAction {
		for index, item := range m.menu.items {
			if item.action == kind {
				return m.runMenuItem(index)
			}
		}
	}
	return Decision{Consumed: true}
}

func actionKind(action string) ActionKind {
	switch action {
	case keybind.NewSession:
		return NewSession
	case keybind.NewGroup:
		return NewGroup
	case keybind.Fork:
		return Fork
	case keybind.Revive:
		return Revive
	case keybind.Restart:
		return Restart
	case keybind.Kill:
		return Kill
	case keybind.Archive:
		return Archive
	case keybind.Restore:
		return Restore
	case keybind.Delete:
		return Delete
	case keybind.Prompt:
		return Prompt
	case keybind.CopyReply:
		return CopyReply
	case keybind.Terminal:
		return NewTerminal
	case keybind.Editor:
		return OpenEditor
	case keybind.Rename:
		return RenameAction
	case keybind.Move:
		return MoveToGroup
	case keybind.Review:
		return OpenReview
	default:
		return NoAction
	}
}
