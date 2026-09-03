package ui

import (
	"fmt"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/keybind"
	tea "github.com/charmbracelet/bubbletea"
)

// sessionKeyActions is the order the picker lists the actions in, with what
// each one does written beside its keys.
var sessionKeyActions = []struct{ name, does string }{
	{"detach", "back to the manager"},
	{"review", "open the session's diff"},
	{"editor", "open its directory"},
}

func sessionBinding(keys keybind.Session, index int) keybind.Binding {
	switch index {
	case 0:
		return keys.Detach
	case 1:
		return keys.Review
	}
	return keys.Editor
}

func withSessionBinding(keys keybind.Session, index int, binding keybind.Binding) keybind.Session {
	switch index {
	case 0:
		keys.Detach = binding
	case 1:
		keys.Review = binding
	default:
		keys.Editor = binding
	}
	return keys
}

// sessionKeysSummary is the settings row's value: the keys each action
// answers to, or a note that it is off.
func sessionKeysSummary(keys keybind.Session) string {
	summary := ""
	for i := range sessionKeyActions {
		label := sessionBinding(keys, i).Label()
		if label == "" {
			label = "off"
		}
		if summary != "" {
			summary += " · "
		}
		summary += label
	}
	return summary
}

func (m *Model) openKeyPicker() {
	m.settings.keyPicker = true
	m.settings.keys = m.keys
	m.settings.keyCursor = 0
	m.settings.keyCapture = false
	m.settings.keyAppend = false
	m.errBar.text = ""
}

func (m *Model) handleKeyPickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.settings.keyCapture {
		return m.captureSessionKey(msg)
	}
	count := len(sessionKeyActions)
	switch msg.String() {
	case "up", "k":
		m.settings.keyCursor = (m.settings.keyCursor + count - 1) % count
	case "down", "j":
		m.settings.keyCursor = (m.settings.keyCursor + 1) % count
	case "enter", "a":
		m.settings.keyCapture = true
		m.settings.keyAppend = msg.String() == "a"
		m.errBar.text = ""
	case "d":
		return m, m.setSessionBinding(keybind.Keys())
	case "esc":
		m.settings.keyPicker = false
		return m, m.saveSessionKeys()
	}
	return m, nil
}

// captureSessionKey binds the key just pressed. Every key reaches here, so
// esc is what leaves rather than a binding: it is refused as a key anyway,
// since a plain key belongs to the agent.
func (m *Model) captureSessionKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.settings.keyCapture = false
	if msg.String() == "esc" {
		m.errBar.text = ""
		return m, nil
	}
	key, err := keybind.Parse(msg.String())
	if err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
	binding := keybind.Keys(key)
	if m.settings.keyAppend {
		existing := sessionBinding(m.settings.keys, m.settings.keyCursor)
		if existing.Has(key.Tea()) {
			m.errBar.text = fmt.Sprintf("%s already answers to %s", sessionKeyActions[m.settings.keyCursor].name, key)
			return m, nil
		}
		binding = keybind.Keys(append(existing.Keys(), key)...)
	}
	return m, m.setSessionBinding(binding)
}

// setSessionBinding puts a binding on the selected action, keeping the
// table the file would accept: a key already spoken for, or an action left
// with no way back, is refused here the way config load refuses it.
func (m *Model) setSessionBinding(binding keybind.Binding) tea.Cmd {
	candidate := withSessionBinding(m.settings.keys, m.settings.keyCursor, binding)
	if err := candidate.Validate(); err != nil {
		m.errBar.text = err.Error()
		return nil
	}
	m.settings.keys = candidate
	m.errBar.text = ""
	return nil
}

// saveSessionKeys writes the table to config.toml and puts it to work
// without a restart: the driver rebinds the tmux keys a full-screen attach
// uses, and every live session's footer is redrawn to name them.
func (m *Model) saveSessionKeys() tea.Cmd {
	keys := m.settings.keys
	if sameSessionKeys(keys, m.keys) {
		return nil
	}
	if m.configDir == "" {
		m.errBar.text = "no config directory to save the keys to"
		return nil
	}
	if err := config.SaveSessionKeys(m.configDir, keys); err != nil {
		m.errBar.text = err.Error()
		return nil
	}
	m.keys = keys
	m.cfg.Keybindings.Session = keys
	m.tmux.SetSessionKeys(keys)
	return m.refreshExistingSessionUX
}

func sameSessionKeys(a, b keybind.Session) bool {
	for i := range sessionKeyActions {
		if sessionBinding(a, i).Label() != sessionBinding(b, i).Label() {
			return false
		}
	}
	return true
}
