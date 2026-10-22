package settings

import (
	"fmt"
	"slices"
	"strings"

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

// tmux_prefix off hands the agent no key, since tmux keeps the prefix it had.
func (s keySection) offLabel(action string) string {
	if action == keybind.TmuxPrefix {
		return "off, your prefix stays"
	}
	return s.off
}

// KeyRow is one action in the picker, in the table at Table.
type KeyRow struct {
	Table  int
	Action keybind.Action
}

// KeyRows lists every action of the tables in picker order.
func KeyRows(tables []keybind.Table) []KeyRow {
	var rows []KeyRow
	for i, keys := range tables {
		for _, action := range keys.Actions() {
			rows = append(rows, KeyRow{Table: i, Action: action})
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

func (s *State) openKeyPicker(ctx Context) {
	s.KeyPicker = true
	s.Tables = []keybind.Table{ctx.Session, ctx.List}
	s.KeyCursor = 0
	s.KeyCapture = false
	s.KeyAppend = false
	s.KeyReset = false
}

// PickedRow is the action under the picker's cursor.
func (s *State) PickedRow() KeyRow {
	return KeyRows(s.Tables)[s.KeyCursor]
}

func (s *State) pickedTable() keybind.Table {
	return s.Tables[s.PickedRow().Table]
}

func (s *State) keyPickerKey(msg tea.KeyMsg) Result {
	if s.KeyCapture {
		return Result{Notice: s.captureKey(msg)}
	}
	if s.KeyReset {
		s.answerKeyReset(msg)
		return Result{}
	}
	count := len(KeyRows(s.Tables))
	switch msg.String() {
	case "up", "k":
		s.KeyCursor = (s.KeyCursor + count - 1) % count
	case "down", "j":
		s.KeyCursor = (s.KeyCursor + 1) % count
	case "enter", "a":
		s.KeyCapture = true
		s.KeyAppend = msg.String() == "a"
		return Result{Notice: clearNotice()}
	case "d":
		return Result{Notice: s.setBinding(keybind.Keys())}
	case "r":
		s.KeyReset = len(keyResetChanges(s.Tables...)) > 0
		return Result{Notice: clearNotice()}
	case "esc":
		s.KeyPicker = false
		return Result{SaveKeys: true}
	}
	return Result{}
}

// Every key reaches here, so esc leaves rather than binds; Parse would
// refuse it anyway.
func (s *State) captureKey(msg tea.KeyMsg) Notice {
	s.KeyCapture = false
	if msg.String() == "esc" {
		return clearNotice()
	}
	key, err := keybind.Parse(msg.String())
	if err != nil {
		return errorNotice(err.Error())
	}
	binding := keybind.Keys(key)
	if s.KeyAppend {
		existing := s.pickedTable().Binding(s.PickedRow().Action.Name)
		if existing.Has(key.Tea()) {
			return errorNotice(fmt.Sprintf("%s already answers to %s", s.PickedRow().Action.Name, key))
		}
		binding = keybind.Keys(append(slices.Clone(existing.Keys()), key)...)
	}
	return s.setBinding(binding)
}

func (s *State) answerKeyReset(msg tea.KeyMsg) {
	switch msg.String() {
	case "y", "enter":
		for i, keys := range s.Tables {
			s.Tables[i] = keys.Defaults()
		}
		s.KeyReset = false
	case "n", "esc":
		s.KeyReset = false
	}
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
func (s *State) setBinding(binding keybind.Binding) Notice {
	row := s.PickedRow()
	candidate := s.Tables[row.Table].With(row.Action.Name, binding)
	if err := candidate.Validate(); err != nil {
		return errorNotice(err.Error())
	}
	s.Tables[row.Table] = candidate
	return clearNotice()
}

// KeysChange is the picker's tables against what the store will hold once
// the saves already queued land.
type KeysChange struct {
	Session, List               keybind.Table
	SessionChanged, ListChanged bool
}

// KeysToSave compares the picker's tables with the expected ones. The saved
// tables take effect without a restart: the list reads its table on the next
// key, and for the session table the root rebinds the tmux keys.
func (s *State) KeysToSave(expectedSession, expectedList keybind.Table) (KeysChange, bool) {
	session, list := s.Tables[0], s.Tables[1]
	change := KeysChange{
		Session:        session,
		List:           list,
		SessionChanged: !session.Equal(expectedSession),
		ListChanged:    !list.Equal(expectedList),
	}
	return change, change.SessionChanged || change.ListChanged
}
