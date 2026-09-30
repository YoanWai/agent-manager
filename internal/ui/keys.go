package ui

import (
	"github.com/YoanWai/agent-manager/internal/keybind"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// A drag armed by the mouse alone leaves the keyboard live, so a press
	// whose release never lands cannot strand the list: the next key ends
	// that drag where it stands, esc cancelling it exactly as the footer
	// says, and anything else is then handled normally.
	if m.split.dragging && !m.split.resizeMode {
		if keybind.Normalize(msg.String()) == "esc" {
			return m.exitResizeMode(false)
		}
		m.exitResizeMode(m.split.moved)
	}
	// Resize mode owns the keyboard until the drag commits or the user
	// cancels: other bindings would fight the mouse-gated session.
	if m.split.resizeMode {
		key := keybind.Normalize(msg.String())
		switch {
		case key == "enter" || m.services.listKeys.Binding(keybind.Resize).Has(key):
			return m.exitResizeMode(true)
		case key == "esc":
			return m.exitResizeMode(false)
		case key == "ctrl+c" || m.services.listKeys.Binding(keybind.Quit).Has(key):
			m.persistSplitRatio()
			m.split.resizeMode = false
			m.split.dragging = false
			m.split.moved = false
			return m, tea.Quit
		case key == "left" || key == "h":
			m.nudgeSplit(-1)
			return m, nil
		case key == "right" || key == "l":
			m.nudgeSplit(1)
			return m, nil
		default:
			return m, nil
		}
	}

	switch m.mode {
	case modeForm:
		return m.handleFormKey(msg)
	case modeConfirmDelete:
		return m.handleConfirmKey(msg)
	case modeLaunchHint:
		return m.handleLaunchHintKey(msg)
	case modeRename:
		return m.handleRenameKey(msg)
	case modeFork:
		return m.handleForkKey(msg)
	case modeSettings:
		return m.handleSettingsKey(msg)
	case modeMove:
		return m.handleMoveKey(msg)
	case modeRepoPick:
		return m.handleRepoPickKey(msg)
	case modeGroupForm:
		return m.handleGroupFormKey(msg)
	case modeDiff:
		return m.handleDiffKey(msg)
	case modeFocus:
		return m.handleFocusKey(msg)
	case modeNotices:
		return m.handleNoticesKey(msg)
	case modeHelp:
		return m.handleHelpKey(msg)
	}

	// A lifted row and an open menu sit on top of the quick bar, so they
	// take the keys first.
	if m.rail.reorder.active {
		return m.handleReorderKey(msg)
	}
	if m.rail.menu.active {
		model, cmd := m.handleMenuKey(msg)
		m.closeQuickOffTheList()
		return model, cmd
	}
	if m.rail.searching {
		return m.handleSearchKey(msg)
	}
	if m.quick.active {
		return m.handleQuickKey(msg)
	}

	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		return m, m.clearSearch()
	}
	action, _ := m.services.listKeys.ActionFor(keybind.Normalize(msg.String()))
	return m.runListAction(action)
}

// runListAction does what a list key bound to action does, so the row
// menu runs exactly what the key would.
func (m *Model) runListAction(action string) (tea.Model, tea.Cmd) {
	switch action {
	case keybind.Quit:
		return m, tea.Quit
	case keybind.Up:
		return m, m.moveCursor(-1)
	case keybind.Down:
		return m, m.moveCursor(1)
	case keybind.ReorderUp:
		return m.reorderSelected(-1)
	case keybind.ReorderDown:
		return m.reorderSelected(1)
	case keybind.Open:
		if entry, ok := m.selectedRow(); ok && entry.isGroup {
			m.toggleCollapse()
			return m, nil
		}
		if m.enterFocuses() {
			return m.focusSelected()
		}
		return m.attachSelected()
	case keybind.StepIn:
		if !m.prefs.arrowStep {
			return m, nil
		}
		if entry, ok := m.selectedRow(); ok && entry.isGroup {
			if m.rail.collapsed[entry.group] {
				m.toggleCollapse()
			}
			return m, nil
		}
		return m.focusSelected()
	case keybind.StepOut:
		if !m.prefs.arrowStep {
			return m, nil
		}
		if entry, ok := m.selectedRow(); ok && entry.isGroup && !m.rail.collapsed[entry.group] {
			m.toggleCollapse()
		}
		return m, nil
	case keybind.Attach:
		if m.enterFocuses() {
			return m.attachSelected()
		}
		return m.focusSelected()
	case keybind.NewSession:
		m.openForm()
	case keybind.NewGroup:
		m.openGroupForm()
	case keybind.Fork:
		m.openFork()
	case keybind.Revive:
		return m.reviveSelected()
	case keybind.MarkIdle:
		return m.acknowledgeSelected()
	case keybind.ReviveAll:
		return m.reviveAllDead()
	case keybind.Restart:
		return m.restartSelected()
	case keybind.Kill:
		return m.killSelected()
	case keybind.KillAll:
		return m.killAllLive()
	case keybind.Archive:
		return m.archiveSelected()
	case keybind.Restore:
		return m.restoreSelected()
	case keybind.Delete:
		m.prepareDelete()
	case keybind.Prompt:
		m.openQuickMode()
	case keybind.CopyReply:
		return m.copyReplySelected()
	case keybind.FoldAll:
		m.toggleCollapseAll()
	case keybind.Filter:
		return m, m.cycleStatusFilter()
	case keybind.Settings:
		m.openSettings()
	case keybind.Resize:
		return m.enterResizeMode()
	case keybind.Archived:
		m.rail.showArchived = !m.rail.showArchived
		m.requestRefresh()
	case keybind.Terminal:
		return m.terminalKey()
	case keybind.Editor:
		return m.openEditor()
	case keybind.EmptyGroups:
		return m, m.toggleEmptyGroups()
	case keybind.Search:
		m.rail.searching = true
		m.errBar.text = ""
	case keybind.Rename:
		m.openRename()
	case keybind.Move:
		m.openMove()
	case keybind.Messages:
		m.openNotices("")
	case keybind.Help:
		m.openHelp()
	case keybind.Review:
		return m, m.openDiff()
	}
	return m, nil
}
