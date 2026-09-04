package ui

import (
	"fmt"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/keybind"
	tea "github.com/charmbracelet/bubbletea"
)

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

// Every key reaches here, so esc leaves rather than binds; Parse would
// refuse it as a plain key anyway.
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

// The picker refuses what config load would refuse, so the table it saves
// always loads back.
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

// The saved table takes effect without a restart: the driver rebinds the
// tmux keys and every live session's footer is redrawn.
func (m *Model) saveSessionKeys() tea.Cmd {
	keys := m.settings.keys
	if keys.Equal(m.keys) {
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
	m.tmux.SetSessionKeys(keys)
	return m.refreshExistingSessionUX
}
