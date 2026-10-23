package ui

import (
	"github.com/YoanWai/agent-manager/internal/keybind"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	tea "github.com/charmbracelet/bubbletea"
)

// quitRoute is what a key does about quitting before any surface sees it.
type quitRoute uint8

const (
	quitPass       quitRoute = iota // the layer under the key takes it
	quitNow                         // the manager quits, or abandons its drain
	quitPopSurface                  // the surface on top closes
)

// routeQuit is the one place that decides ctrl+c. Update asks it before a
// drain swallows keys, and handleKey asks it again for a key a click
// presses, so no surface keeps its own idea of quit.
//
// This is the priority layer of model_surface.go's stack: it runs before
// any surface sees the key. ctrl+c closes the surface on top, quits where
// the manager owns the keyboard, and passes to the agent where it does not.
// A drain already under way leaves on a second ctrl+c or quit key from
// anywhere, since by then the agent has nothing left to cancel.
func (m *Model) routeQuit(msg tea.KeyMsg) quitRoute {
	name := keybind.Normalize(msg.String())
	ctrlC := name == "ctrl+c"
	top, onSurface := m.top()
	switch {
	case m.effects.quitting && (ctrlC || m.services.listKeys.Binding(keybind.Quit).Has(name)):
		return quitNow
	case !ctrlC || m.agentOwnsKeyboard():
		return quitPass
	case onSurface && top.Dismissible():
		return quitPopSurface
	case onSurface:
		return quitPass
	}
	return quitNow
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.routeQuit(msg) {
	case quitNow:
		return m.requestQuit()
	case quitPopSurface:
		m.pop()
		return m, nil
	}
	if cmd, ok := m.surfaceKey(msg); ok {
		return m, cmd
	}
	if m.mode == modeList && m.rail.InputMode() == uirail.BrowseMode && !m.quick.active && !m.layout.split.dragging && !m.layout.split.resizeMode {
		decision := m.rail.Key(msg, m.layout.displayedRail, m.railKeyContext())
		if decision.Consumed {
			m.gens.foreground++
		}
		return m.applyRailDecision(decision)
	}
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
		if m.services.listKeys.Binding(keybind.Quit).Has(key) {
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
		case m.services.listKeys.Binding(keybind.Quit).Has(key):
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
	case modeSettings:
		return m.handleSettingsKey(msg)
	case modeRepoPick:
		return m, m.handleRepoPickKey(msg)
	case modeGroupForm:
		return m.handleGroupFormKey(msg)
	case modeConnection:
		return m.handleConnectionKey(msg)
	case modeDiff:
		return m.handleDiffKey(msg)
	case modeFocus:
		return m.handleFocusKey(msg)
	case modeNotices:
		return m.handleNoticesKey(msg)
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
