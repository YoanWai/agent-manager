package ui

import (
	"github.com/YoanWai/agent-manager/internal/keybind"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.gens.foreground++
	// A drag armed by the mouse alone leaves the keyboard live, so a press
	// whose release never lands cannot strand the list: the next key ends
	// that drag where it stands, esc cancelling it exactly as the footer
	// says, and anything else is then handled normally.
	if m.layout.split.dragging && !m.layout.split.resizeMode {
		key := keybind.Normalize(msg.String())
		if key == "esc" {
			return m.exitResizeMode(false)
		}
		if key == "ctrl+c" || m.services.listKeys.Binding(keybind.Quit).Has(key) {
			return m.requestQuit()
		}
		m.exitResizeMode(m.layout.split.moved)
	}
	// Resize mode owns the keyboard until the drag commits or the user
	// cancels: other bindings would fight the mouse-gated session.
	if m.layout.split.resizeMode {
		key := keybind.Normalize(msg.String())
		switch {
		case key == "enter" || m.services.listKeys.Binding(keybind.Resize).Has(key):
			return m.exitResizeMode(true)
		case key == "esc":
			return m.exitResizeMode(false)
		case key == "ctrl+c" || m.services.listKeys.Binding(keybind.Quit).Has(key):
			return m.requestQuit()
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
		return m, m.confirm.handleKey(m, msg)
	case modeLaunchHint:
		return m, m.launchHint.handleKey(m, msg)
	case modeRename:
		return m, m.rename.handleKey(m, msg)
	case modeFork:
		return m, m.fork.handleKey(m, msg)
	case modeSettings:
		return m.handleSettingsKey(msg)
	case modeMove:
		return m, m.move.handleKey(m, msg)
	case modeRepoPick:
		return m, m.reviewNav.picker.handleKey(m, msg)
	case modeGroupForm:
		return m.handleGroupFormKey(msg)
	case modeConnection:
		return m.handleConnectionKey(msg)
	case modeDiff:
		return m.handleDiffKey(msg)
	case modeFocus:
		return m.handleFocusKey(msg)
	case modeNotices:
		return m, m.notices.handleKey(m, msg)
	case modeHelp:
		return m.handleHelpKey(msg)
	}

	if m.rail.InputMode() != uirail.BrowseMode {
		return m.applyRailDecision(m.rail.Key(msg, m.layout.displayedRail, m.railKeyContext()))
	}
	if m.quick.active {
		return m.handleQuickKey(msg)
	}
	return m.applyRailDecision(m.rail.Key(msg, m.layout.displayedRail, m.railKeyContext()))
}

func (m *Model) railKeyContext() uirail.KeyContext {
	return uirail.KeyContext{ListKeys: m.services.listKeys, ArrowStep: m.prefs.arrowStep, EnterFocuses: m.enterFocuses()}
}
