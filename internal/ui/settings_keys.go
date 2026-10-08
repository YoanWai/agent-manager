package ui

import (
	"fmt"
	"github.com/YoanWai/agent-manager/internal/keybind"
	tea "github.com/charmbracelet/bubbletea"
	"slices"
	"strings"
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

// tmux_prefix off hands the agent no key, since tmux keeps the prefix it had.
func (s keySection) offLabel(action string) string {
	if action == keybind.TmuxPrefix {
		return "off, your prefix stays"
	}
	return s.off
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
			label := labelOrOff(keys.Binding(action.Name))
			if label == labelOrOff(defaults.Binding(action.Name)) {
				continue
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

func labelOrOff(binding keybind.Binding) string {
	if label := binding.Label(); label != "" {
		return label
	}
	return "off"
}

func (s *settingsFeature) openKeyPicker(h settingsHost) {
	s.dialog.keyPicker = true
	session, list := h.keyTables()
	s.dialog.tables = []keybind.Table{session, list}
	s.dialog.keyCursor = 0
	s.dialog.keyCapture = false
	s.dialog.keyAppend = false
	s.dialog.keyReset = false
	h.clearErr()
}

func (s *settingsFeature) pickedRow() keyRow {
	return keyRowsOf(s.dialog.tables)[s.dialog.keyCursor]
}

func (s *settingsFeature) pickedTable() keybind.Table {
	return s.dialog.tables[s.pickedRow().table]
}

func (s *settingsFeature) handleKeyPickerKey(h keyPickerHost, msg tea.KeyMsg) tea.Cmd {
	if s.dialog.keyCapture {
		return s.captureKey(h, msg)
	}
	if s.dialog.keyReset {
		return s.answerKeyReset(msg)
	}
	count := len(keyRowsOf(s.dialog.tables))
	switch msg.String() {
	case "up", "k":
		s.dialog.keyCursor = (s.dialog.keyCursor + count - 1) % count
	case "down", "j":
		s.dialog.keyCursor = (s.dialog.keyCursor + 1) % count
	case "enter", "a":
		s.dialog.keyCapture = true
		s.dialog.keyAppend = msg.String() == "a"
		h.clearErr()
	case "d":
		return s.setBinding(h, keybind.Keys())
	case "r":
		s.dialog.keyReset = len(keyResetChanges(s.dialog.tables...)) > 0
		h.clearErr()
	case "esc":
		s.dialog.keyPicker = false
		return s.saveKeys(h)
	}
	return nil
}

// Every key reaches here, so esc leaves rather than binds; Parse would
// refuse it anyway.
func (s *settingsFeature) captureKey(h keyPickerHost, msg tea.KeyMsg) tea.Cmd {
	s.dialog.keyCapture = false
	if msg.String() == "esc" {
		h.clearErr()
		return nil
	}
	key, err := keybind.Parse(msg.String())
	if err != nil {
		h.reportErr(err.Error())
		return nil
	}
	binding := keybind.Keys(key)
	if s.dialog.keyAppend {
		existing := s.pickedTable().Binding(s.pickedRow().action.Name)
		if existing.Has(key.Tea()) {
			h.reportErr(fmt.Sprintf("%s already answers to %s", s.pickedRow().action.Name, key))
			return nil
		}
		binding = keybind.Keys(append(slices.Clone(existing.Keys()), key)...)
	}
	return s.setBinding(h, binding)
}

func (s *settingsFeature) answerKeyReset(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "y", "enter":
		for i, keys := range s.dialog.tables {
			s.dialog.tables[i] = keys.Defaults()
		}
		s.dialog.keyReset = false
	case "n", "esc":
		s.dialog.keyReset = false
	}
	return nil
}

func keyResetChanges(tables ...keybind.Table) []string {
	var changes []string
	for _, keys := range tables {
		defaults := keys.Defaults()
		for _, action := range keys.Actions() {
			current, shipped := labelOrOff(keys.Binding(action.Name)), labelOrOff(defaults.Binding(action.Name))
			if current == shipped {
				continue
			}
			changes = append(changes, fmt.Sprintf("%s: %s back to %s", action.Name, current, shipped))
		}
	}
	return changes
}

// The picker refuses what the store would refuse, so the table it saves
// always loads back.
func (s *settingsFeature) setBinding(h keyPickerHost, binding keybind.Binding) tea.Cmd {
	row := s.pickedRow()
	candidate := s.dialog.tables[row.table].With(row.action.Name, binding)
	if err := candidate.Validate(); err != nil {
		h.reportErr(err.Error())
		return nil
	}
	s.dialog.tables[row.table] = candidate
	h.clearErr()
	return nil
}

// The saved tables take effect without a restart: the list reads its
// table on the next key, and for the session table the driver rebinds the
// tmux keys and every live session's footer is redrawn. The picker submits
// captured tables to the effect lane; the store writes happen off the
// update path and a partial commit reconciles the runtime to the store.
func (s *settingsFeature) saveKeys(h keyPickerHost) tea.Cmd {
	session, list := s.dialog.tables[0], s.dialog.tables[1]
	expectedSession, expectedList := h.keyTables()
	for _, pending := range h.queuedKeys() {
		if pending.listChanged {
			expectedList = pending.list
		}
		if pending.sessionChanged {
			expectedSession = pending.session
		}
	}
	listChanged := !list.Equal(expectedList)
	sessionChanged := !session.Equal(expectedSession)
	if !listChanged && !sessionChanged {
		return nil
	}
	return h.submitKeys(keysRequest{list: list, session: session, listChanged: listChanged, sessionChanged: sessionChanged})
}
