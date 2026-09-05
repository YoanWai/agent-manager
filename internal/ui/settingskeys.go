package ui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/keybind"
	tea "github.com/charmbracelet/bubbletea"
)

// keySection is how the picker introduces each table and words a key it
// has turned off: inside a session that key now belongs to the agent.
type keySection struct {
	title string
	off   string
}

func keySectionFor(keys keybind.Table) keySection {
	if keys.Scope() == keybind.ScopeSession {
		return keySection{"inside a session · every other key reaches the agent", "off, the agent gets it"}
	}
	return keySection{"in the manager · esc and ctrl+c stay as they are", "off"}
}

type keyRow struct {
	table  int
	action keybind.Action
}

func keyRowsOf(tables []keybind.Table) []keyRow {
	var rows []keyRow
	for i, keys := range tables {
		for _, action := range keys.Actions() {
			rows = append(rows, keyRow{table: i, action: action})
		}
	}
	return rows
}

func keybindingsSummary(tables ...keybind.Table) string {
	var moved []string
	for _, keys := range tables {
		defaults := keys.Defaults()
		for _, action := range keys.Actions() {
			label := keys.Binding(action.Name).Label()
			if label == defaults.Binding(action.Name).Label() {
				continue
			}
			if label == "" {
				label = "off"
			}
			moved = append(moved, action.Name+" "+label)
		}
	}
	switch len(moved) {
	case 0:
		return "defaults"
	case 1, 2:
		return strings.Join(moved, " · ")
	}
	return fmt.Sprintf("%d moved", len(moved))
}

func (m *Model) openKeyPicker() {
	m.settings.keyPicker = true
	m.settings.tables = []keybind.Table{m.keys, m.listKeys}
	m.settings.keyCursor = 0
	m.settings.keyCapture = false
	m.settings.keyAppend = false
	m.settings.keyReset = false
	m.errBar.text = ""
}

func (m *Model) pickedRow() keyRow {
	return keyRowsOf(m.settings.tables)[m.settings.keyCursor]
}

func (m *Model) pickedTable() keybind.Table {
	return m.settings.tables[m.pickedRow().table]
}

func (m *Model) handleKeyPickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.settings.keyCapture {
		return m.captureKey(msg)
	}
	if m.settings.keyReset {
		return m.answerKeyReset(msg)
	}
	count := len(keyRowsOf(m.settings.tables))
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
		return m, m.setBinding(keybind.Keys())
	case "r":
		m.settings.keyReset = len(keyResetChanges(m.settings.tables...)) > 0
		m.errBar.text = ""
	case "esc":
		m.settings.keyPicker = false
		return m, m.saveKeys()
	}
	return m, nil
}

// Every key reaches here, so esc leaves rather than binds; Parse would
// refuse it anyway.
func (m *Model) captureKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
		existing := m.pickedTable().Binding(m.pickedRow().action.Name)
		if existing.Has(key.Tea()) {
			m.errBar.text = fmt.Sprintf("%s already answers to %s", m.pickedRow().action.Name, key)
			return m, nil
		}
		binding = keybind.Keys(append(slices.Clone(existing.Keys()), key)...)
	}
	return m, m.setBinding(binding)
}

func (m *Model) answerKeyReset(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "enter":
		for i, keys := range m.settings.tables {
			m.settings.tables[i] = keys.Defaults()
		}
		m.settings.keyReset = false
	case "n", "esc":
		m.settings.keyReset = false
	}
	return m, nil
}

func keyResetChanges(tables ...keybind.Table) []string {
	var changes []string
	for _, keys := range tables {
		defaults := keys.Defaults()
		for _, action := range keys.Actions() {
			current, shipped := keys.Binding(action.Name).Label(), defaults.Binding(action.Name).Label()
			if current == shipped {
				continue
			}
			if current == "" {
				current = "off"
			}
			changes = append(changes, fmt.Sprintf("%s: %s back to %s", action.Name, current, shipped))
		}
	}
	return changes
}

// The picker refuses what config load would refuse, so the table it saves
// always loads back.
func (m *Model) setBinding(binding keybind.Binding) tea.Cmd {
	row := m.pickedRow()
	candidate := m.settings.tables[row.table].With(row.action.Name, binding)
	if err := candidate.Validate(); err != nil {
		m.errBar.text = err.Error()
		return nil
	}
	m.settings.tables[row.table] = candidate
	m.errBar.text = ""
	return nil
}

// The saved tables take effect without a restart: the list reads its
// table on the next key, and for the session table the driver rebinds the
// tmux keys and every live session's footer is redrawn.
func (m *Model) saveKeys() tea.Cmd {
	session, list := m.settings.tables[0], m.settings.tables[1]
	if session.Equal(m.keys) && list.Equal(m.listKeys) {
		return nil
	}
	if m.configDir == "" {
		m.errBar.text = "no config directory to save the keys to"
		return nil
	}
	if !list.Equal(m.listKeys) {
		if err := config.SaveKeys(m.configDir, list); err != nil {
			m.errBar.text = err.Error()
			return nil
		}
		m.listKeys = list
	}
	if session.Equal(m.keys) {
		return nil
	}
	if err := config.SaveKeys(m.configDir, session); err != nil {
		m.errBar.text = err.Error()
		return nil
	}
	m.keys = session
	m.tmux.SetSessionKeys(session)
	return m.refreshExistingSessionUX
}
