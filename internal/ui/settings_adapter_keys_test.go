package ui

import (
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/keybind"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// keyPickerModel opens the settings screen and steps into the key picker.
func keyPickerModel(t *testing.T) *Model {
	t.Helper()
	m := buildModel(t)
	m.services.keys = keybind.DefaultSession()
	m.services.tmux.SetSessionKeys(m.services.keys)
	t.Cleanup(func() {
		m.services.tmux.SetSessionKeys(keybind.DefaultSession())
		if err := m.services.tmux.EnsureBindings(); err != nil {
			t.Errorf("restore default bindings: %v", err)
		}
	})
	m.openSettings()
	m.settingsCard().State().Field = uisettings.FieldKeybindings
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	if !m.settingsCard().State().KeyPicker {
		t.Fatalf("enter on the keys row should open the picker, err = %q", m.errBar.text)
	}
	return m
}

func (m *Model) pressInPicker(t *testing.T, msg tea.KeyMsg) tea.Cmd {
	t.Helper()
	updated, cmd := m.handleKey(msg)
	*m = *updated.(*Model)
	return cmd
}

func storedSessionKeys(t *testing.T, m *Model) keybind.Table {
	t.Helper()
	keys, err := m.services.store.SessionKeys()
	if err != nil {
		t.Fatalf("SessionKeys: %v", err)
	}
	return keys
}

// storedKeyRow is the raw row a scope's table is kept in, empty until the
// picker saves that table.
func storedKeyRow(t *testing.T, m *Model, scope string) string {
	t.Helper()
	row, err := m.services.store.Setting("keybindings." + scope)
	if err != nil {
		t.Fatalf("Setting: %v", err)
	}
	return row
}

// The picker binds the key that was pressed: leaving it writes the table to
// the store, puts it on the model, and rebinds the tmux server so a live
// session answers to the new key.
func TestKeyPickerBindsCapturedKeyAndSavesIt(t *testing.T) {
	m := keyPickerModel(t)
	// A live session, bound with the old keys before the change: psmux
	// keeps bindings per session server, so the rebind is checked there.
	live := newID()
	if err := m.services.tmux.Create(live, t.TempDir(), "", nil, 80, 24); err != nil {
		t.Fatalf("create live session: %v", err)
	}
	t.Cleanup(func() { _ = m.services.tmux.Kill(live) })
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.settingsCard().State().KeyCapture {
		t.Fatal("enter on an action should wait for a key")
	}
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyF9})
	if got := m.settingsCard().State().Tables[0].Binding(keybind.Detach).Label(); got != "f9" {
		t.Fatalf("detach = %q, want f9", got)
	}

	cmd := m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc})
	if m.settingsCard().State().KeyPicker {
		t.Fatal("esc should leave the picker")
	}
	if m.errBar.text != "" {
		t.Fatalf("saving reported %q", m.errBar.text)
	}
	if cmd == nil {
		t.Fatal("saving should enqueue the save work")
	}
	if got := m.services.keys.Binding(keybind.Detach).Label(); got != `ctrl+q / ctrl+\` {
		t.Fatalf("runtime detach changed before the save committed: %q", got)
	}
	if row := storedKeyRow(t, m, keybind.ScopeSession); row != "" {
		t.Fatalf("the store was written on the update path: %s", row)
	}
	// The completion's refresh command carries the tmux rebind; run it the
	// way the event loop would.
	updated, next := m.Update(cmd())
	*m = *updated.(*Model)
	m.applyTestMsg(t, next())
	m.drainEffects(t)
	if got := m.services.keys.Binding(keybind.Detach).Label(); got != "f9" {
		t.Fatalf("model detach = %q, want f9", got)
	}
	if got := storedSessionKeys(t, m).Binding(keybind.Detach).Label(); got != "f9" {
		t.Fatalf("stored detach = %q, want f9", got)
	}

	bound, err := sessionTmuxCmd(t, live, "list-keys", "-T", "root").CombinedOutput()
	if err != nil {
		t.Fatalf("list root keys: %v: %s", err, bound)
	}
	if !strings.Contains(string(bound), "F9") {
		t.Fatalf("the server should carry the new detach key:\n%s", bound)
	}
	for _, line := range strings.Split(string(bound), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 4 && fields[3] == "C-q" && strings.Contains(line, "detach-client") {
			t.Fatalf("the old detach key should be gone: %q", line)
		}
	}
}

// A key the manager cannot bind is refused in the picker with the reason,
// and nothing changes.
func TestKeyPickerRefusesAKeyTheAgentNeeds(t *testing.T) {
	m := keyPickerModel(t)
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInPicker(t, runeKey("o"))
	if !strings.Contains(m.errBar.text, "plain key, which reaches the agent") {
		t.Fatalf("err = %q, want the plain-key reason", m.errBar.text)
	}
	if got := m.settingsCard().State().Tables[0].Binding(keybind.Detach).Label(); got != `ctrl+q / ctrl+\` {
		t.Fatalf("detach should be untouched, got %q", got)
	}
}

// One key serves one action, so binding review to the editor's key is
// refused rather than leaving two actions on it.
func TestKeyPickerRefusesAKeyAnotherActionOwns(t *testing.T) {
	m := keyPickerModel(t)
	m.settingsCard().State().KeyCursor = 1
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyF3})
	if !strings.Contains(m.errBar.text, "bound to both") {
		t.Fatalf("err = %q, want the shared-key reason", m.errBar.text)
	}
	if got := m.settingsCard().State().Tables[0].Binding(keybind.Review).Label(); got != "ctrl+r" {
		t.Fatalf("review should be untouched, got %q", got)
	}
}

// d hands an action's key to the agent, and refuses to do it for detach,
// which is the way back from a focused session.
func TestKeyPickerTurnsAnActionOffButKeepsAWayBack(t *testing.T) {
	m := keyPickerModel(t)
	m.settingsCard().State().KeyCursor = 2
	m.pressInPicker(t, runeKey("d"))
	if got := m.settingsCard().State().Tables[0].Binding(keybind.Editor).Label(); got != "" {
		t.Fatalf("editor should be off, got %q", got)
	}

	m.settingsCard().State().KeyCursor = 0
	m.pressInPicker(t, runeKey("d"))
	if !strings.Contains(m.errBar.text, "detach needs at least one key") {
		t.Fatalf("err = %q, want the detach rule", m.errBar.text)
	}
	if got := m.settingsCard().State().Tables[0].Binding(keybind.Detach).Label(); got != `ctrl+q / ctrl+\` {
		t.Fatalf("detach should be untouched, got %q", got)
	}

	m.settingsCard().State().KeyCursor = 2
	cmd := m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc})
	m.applyCmd(t, cmd)
	if got := storedSessionKeys(t, m).Binding(keybind.Editor).Label(); got != "" {
		t.Fatalf("the store should record the disabled action, got %q", got)
	}
}

// a adds a second key to an action rather than replacing what it answers to.
func TestKeyPickerAddsASecondKey(t *testing.T) {
	m := keyPickerModel(t)
	m.settingsCard().State().KeyCursor = 1
	m.pressInPicker(t, runeKey("a"))
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyF9})
	if got := m.settingsCard().State().Tables[0].Binding(keybind.Review).Label(); got != "ctrl+r / f9" {
		t.Fatalf("review = %q, want both keys", got)
	}
	m.pressInPicker(t, runeKey("a"))
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyF9})
	if !strings.Contains(m.errBar.text, "already answers to") {
		t.Fatalf("err = %q, want the duplicate note", m.errBar.text)
	}
}

// esc during capture cancels it; the key that would have been bound is not.
func TestKeyPickerResetsEveryActionToItsDefaultAfterAsking(t *testing.T) {
	m := keyPickerModel(t)
	custom := sessionOf(t, []string{"ctrl+q", "f9"}, nil, []string{"f5"})
	customList := keybind.DefaultList().With(keybind.NewSession, bindingOf(t, "N"))
	m.services.keys, m.services.listKeys = custom, customList
	m.services.tmux.SetSessionKeys(custom)
	m.settingsCard().State().Tables[0], m.settingsCard().State().Tables[1] = custom, customList

	m.pressInPicker(t, runeKey("r"))
	if !m.settingsCard().State().KeyReset {
		t.Fatal("r should ask before resetting")
	}
	ask := ansi.Strip(m.viewSettings())
	for _, want := range []string{"Reset every key", "detach: ctrl+q / f9 back to ctrl+q / ctrl+\\", "review: off back to ctrl+r", "editor: f5 back to f3", "new_session: N back to n"} {
		if !strings.Contains(ask, want) {
			t.Fatalf("the question should say %q:\n%s", want, ask)
		}
	}
	m.pressInPicker(t, runeKey("n"))
	if m.settingsCard().State().KeyReset || !m.settingsCard().State().Tables[0].Equal(custom) {
		t.Fatalf("n should keep the keys, got %s", m.settingsCard().State().Tables[0].Binding(keybind.Detach).Label())
	}

	m.pressInPicker(t, runeKey("r"))
	m.pressInPicker(t, runeKey("y"))
	if m.settingsCard().State().KeyReset || !m.settingsCard().State().Tables[0].Equal(keybind.DefaultSession()) || !m.settingsCard().State().Tables[1].Equal(keybind.DefaultList()) {
		t.Fatalf("y should restore both tables, got %s and new_session %s", m.settingsCard().State().Tables[0].Binding(keybind.Detach).Label(), m.settingsCard().State().Tables[1].Binding(keybind.NewSession).Label())
	}

	cmd := m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc})
	m.applyCmd(t, cmd)
	if !m.services.keys.Equal(keybind.DefaultSession()) || !m.services.listKeys.Equal(keybind.DefaultList()) {
		t.Fatalf("model keys after save = %s, new_session %s", m.services.keys.Binding(keybind.Detach).Label(), m.services.listKeys.Binding(keybind.NewSession).Label())
	}
	if !storedSessionKeys(t, m).Equal(keybind.DefaultSession()) {
		t.Fatalf("stored session keys = %s", storedKeyRow(t, m, keybind.ScopeSession))
	}
	storedList, err := m.services.store.ListKeys()
	if err != nil {
		t.Fatalf("ListKeys: %v", err)
	}
	if !storedList.Equal(keybind.DefaultList()) {
		t.Fatalf("stored list keys = %s", storedKeyRow(t, m, keybind.ScopeList))
	}
}

func TestKeyPickerResetOnDefaultsAsksNothing(t *testing.T) {
	m := keyPickerModel(t)
	m.pressInPicker(t, runeKey("r"))
	if m.settingsCard().State().KeyReset {
		t.Fatal("r on the defaults should not open the question")
	}
}

func TestKeyPickerEscapeCancelsCapture(t *testing.T) {
	m := keyPickerModel(t)
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc})
	if m.settingsCard().State().KeyCapture {
		t.Fatal("esc should end the capture")
	}
	if !m.settingsCard().State().KeyPicker {
		t.Fatal("cancelling a capture should stay in the picker")
	}
	if got := m.settingsCard().State().Tables[0].Binding(keybind.Detach).Label(); got != `ctrl+q / ctrl+\` {
		t.Fatalf("detach should be untouched, got %q", got)
	}
}

// Leaving the picker without a change writes nothing, so an action nobody
// moved keeps following the default a later release gives it.
func TestKeyPickerLeavesTheStoreAloneWithoutAChange(t *testing.T) {
	m := keyPickerModel(t)
	if cmd := m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc}); cmd != nil {
		t.Fatal("an unchanged table should not refresh the sessions")
	}
	m.drainEffects(t)
	for _, scope := range []string{keybind.ScopeSession, keybind.ScopeList} {
		if row := storedKeyRow(t, m, scope); row != "" {
			t.Fatalf("the %s table should not be stored: %s", scope, row)
		}
	}
}

func TestKeyPickerViewNamesTheKeysAndTheCapture(t *testing.T) {
	m := keyPickerModel(t)
	view := ansi.Strip(m.viewSettings())
	for _, want := range []string{"detach", `ctrl+q / ctrl+\`, "back to the manager", "every other key reaches the agent"} {
		if !strings.Contains(view, want) {
			t.Fatalf("picker view is missing %q:\n%s", want, view)
		}
	}
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	if capture := ansi.Strip(m.viewSettings()); !strings.Contains(capture, "press a key") {
		t.Fatalf("a waiting row should say so:\n%s", capture)
	}
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc})
	m.settingsCard().State().KeyCursor = 2
	m.pressInPicker(t, runeKey("d"))
	if off := ansi.Strip(m.viewSettings()); !strings.Contains(off, "off, the agent gets it") {
		t.Fatalf("a disabled action should say where its key goes:\n%s", off)
	}
}

// The settings row names the keys in force, so the screen answers the
// question without opening the picker.
func TestSettingsRowCountsTheMovedKeys(t *testing.T) {
	m := buildModel(t)
	m.services.keys = keybind.DefaultSession()
	m.openSettings()
	m.settingsCard().State().Field = uisettings.FieldKeybindings
	view := ansi.Strip(m.viewSettings())
	if !strings.Contains(view, "keybindings") || !strings.Contains(view, "defaults") {
		t.Fatalf("settings should carry the row on its defaults:\n%s", view)
	}
	m.services.keys = m.services.keys.With(keybind.Editor, bindingOf(t))
	if view := ansi.Strip(m.viewSettings()); !strings.Contains(view, "editor off") {
		t.Fatalf("one moved key should be named:\n%s", view)
	}
	m.services.listKeys = m.services.listKeys.With(keybind.NewSession, bindingOf(t, "N"))
	if view := ansi.Strip(m.viewSettings()); !strings.Contains(view, "editor off · new_session N") {
		t.Fatalf("two moved keys should both be named:\n%s", view)
	}
	m.services.listKeys = m.services.listKeys.With(keybind.Quit, bindingOf(t, "Q"))
	if view := ansi.Strip(m.viewSettings()); !strings.Contains(view, "3 moved") {
		t.Fatalf("past two the row counts:\n%s", view)
	}
}

func TestListPickerMovesAKeyAndTheListFollows(t *testing.T) {
	m := keyPickerModel(t)
	view := ansi.Strip(m.viewSettings())
	for _, want := range []string{"Keybindings", "inside a session", "detach", "in the manager", "new_session", "esc and ctrl+c stay as they are"} {
		if !strings.Contains(view, want) {
			t.Fatalf("list picker is missing %q:\n%s", want, view)
		}
	}
	m.settingsCard().State().KeyCursor = listRow(t, m, keybind.NewSession)
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInPicker(t, runeKey("N"))
	if got := m.settingsCard().State().Tables[1].Binding(keybind.NewSession).Label(); got != "N" {
		t.Fatalf("new_session = %q, want N", got)
	}
	cmd := m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("saving the list table should enqueue the save work")
	}
	if row := storedKeyRow(t, m, keybind.ScopeList); row != "" {
		t.Fatalf("the store was written on the update path: %s", row)
	}
	if m.errBar.text != "" {
		t.Fatalf("saving reported %q", m.errBar.text)
	}
	m.applyCmd(t, cmd)
	stored, err := m.services.store.ListKeys()
	if err != nil {
		t.Fatalf("ListKeys: %v", err)
	}
	if got := stored.Binding(keybind.NewSession).Label(); got != "N" {
		t.Fatalf("stored new_session = %q, want N", got)
	}
	if row := storedKeyRow(t, m, keybind.ScopeSession); row != "" {
		t.Fatalf("the session table was not touched and should not be stored: %s", row)
	}

	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc})
	if m.settingsOpen() {
		t.Fatalf("mode after leaving settings = %v", m.screenID())
	}
	m.pressInPicker(t, runeKey("n"))
	if formOpen(m) {
		t.Fatal("n should no longer open the new-session form")
	}
	m.pressInPicker(t, runeKey("N"))
	if !formOpen(m) {
		t.Fatalf("N should open the new-session form, screen = %v", m.screenID())
	}
}

func TestListPickerRefusesWhatWouldStrandTheUser(t *testing.T) {
	m := keyPickerModel(t)
	m.settingsCard().State().KeyCursor = listRow(t, m, keybind.Settings)
	m.pressInPicker(t, runeKey("d"))
	if !strings.Contains(m.errBar.text, "settings needs at least one key") {
		t.Fatalf("err = %q, want the settings rule", m.errBar.text)
	}
	m.settingsCard().State().KeyCursor = listRow(t, m, keybind.Kill)
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInPicker(t, runeKey("n"))
	if !strings.Contains(m.errBar.text, "n is bound to both new_session and kill") {
		t.Fatalf("err = %q, want the shared-key reason", m.errBar.text)
	}
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc})
	if !m.settingsCard().State().Tables[1].Equal(keybind.DefaultList()) {
		t.Fatal("a refused or cancelled capture should leave the table alone")
	}
}

// Thirty-odd rows do not fit a short terminal: the picker shows a window
// around the cursor and says how many rows lie beyond it.
func TestListPickerScrollsAroundTheCursor(t *testing.T) {
	m := keyPickerModel(t)
	m.layout.height = 20
	below, above := regexp.MustCompile(`↓ \d+ more`), regexp.MustCompile(`↑ \d+ more`)
	top := ansi.Strip(m.viewSettings())
	if !below.MatchString(top) || above.MatchString(top) {
		t.Fatalf("at the top only the rows below should be counted:\n%s", top)
	}
	m.settingsCard().State().KeyCursor = len(uisettings.KeyRows(m.settingsCard().State().Tables)) - 1
	bottom := ansi.Strip(m.viewSettings())
	if !strings.Contains(bottom, "quit") || !above.MatchString(bottom) || below.MatchString(bottom) {
		t.Fatalf("at the bottom the last row shows and only the rows above are counted:\n%s", bottom)
	}
}

func listRow(t *testing.T, m *Model, name string) int {
	t.Helper()
	for i, row := range uisettings.KeyRows(m.settingsCard().State().Tables) {
		if row.Table == 1 && row.Action.Name == name {
			return i
		}
	}
	t.Fatalf("no list action %q", name)
	return -1
}

// The picker sets tmux_prefix like a session key and refuses a third prefix key.
func TestKeyPickerSetsTheTmuxPrefix(t *testing.T) {
	m := keyPickerModel(t)
	m.settingsCard().State().KeyCursor = 3
	if row := m.settingsCard().State().PickedRow(); row.Action.Name != keybind.TmuxPrefix {
		t.Fatalf("row 3 = %q, want tmux_prefix", row.Action.Name)
	}
	if view := ansi.Strip(m.viewSettings()); !strings.Contains(view, "off, your prefix stays") {
		t.Fatalf("an unset tmux_prefix should say tmux keeps its prefix:\n%s", view)
	}

	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyCtrlB})
	m.pressInPicker(t, runeKey("a"))
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyF12})
	m.pressInPicker(t, runeKey("a"))
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyCtrlG})
	if !strings.Contains(m.errBar.text, "takes one key or two") {
		t.Fatalf("err = %q, want the two-key rule", m.errBar.text)
	}
	if got := m.settingsCard().State().Tables[0].Binding(keybind.TmuxPrefix).Label(); got != "ctrl+b / f12" {
		t.Fatalf("tmux_prefix = %q, want both keys", got)
	}

	// The refused third key's message belongs to the picker; the save
	// itself must report nothing.
	m.errBar.text = ""
	cmd := m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("saving should enqueue the save work")
	}
	// The completion's refresh command carries the tmux rebind; run it the
	// way the event loop would.
	updated, next := m.Update(cmd())
	*m = *updated.(*Model)
	m.applyTestMsg(t, next())
	m.drainEffects(t)
	if m.errBar.text != "" {
		t.Fatalf("saving reported %q", m.errBar.text)
	}
	if got := m.services.keys.Binding(keybind.TmuxPrefix).Label(); got != "ctrl+b / f12" {
		t.Fatalf("model tmux_prefix = %q", got)
	}
	if got := storedSessionKeys(t, m).Binding(keybind.TmuxPrefix).Label(); got != "ctrl+b / f12" {
		t.Fatalf("stored tmux_prefix = %q, want both keys", got)
	}
}

// Reset puts tmux_prefix back to off, and the question says so in words.
func TestKeyPickerResetNamesTheTmuxPrefixGoingOff(t *testing.T) {
	m := keyPickerModel(t)
	m.settingsCard().State().Tables[0] = m.settingsCard().State().Tables[0].With(keybind.TmuxPrefix, bindingOf(t, "ctrl+b"))
	m.pressInPicker(t, runeKey("r"))
	if ask := ansi.Strip(m.viewSettings()); !strings.Contains(ask, "tmux_prefix: ctrl+b back to off") {
		t.Fatalf("the question should name tmux_prefix going off:\n%s", ask)
	}
}

func TestKeySaveDefersDiskWritesToTheEffect(t *testing.T) {
	m := keyPickerModel(t)
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyF9})
	before := savedKeys(t, m)
	cmd := m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("a changed table should enqueue the save work")
	}
	if got := savedKeys(t, m); got != before {
		t.Fatalf("the key table was written on the update path:\n%s", got)
	}
	m.applyCmd(t, cmd)
	if saved := savedKeys(t, m); !strings.Contains(saved, `"detach":["f9"]`) {
		t.Fatalf("the store should carry the new key:\n%s", saved)
	}
	if got := m.services.keys.Binding(keybind.Detach).Label(); got != "f9" {
		t.Fatalf("runtime detach = %q, want f9", got)
	}
}

func TestKeySavePersistsCapturedTablesNotLaterEdits(t *testing.T) {
	m := keyPickerModel(t)
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyF9})
	cmd := m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc})
	if m.effects.Main.Active == nil {
		t.Fatal("the save should be accepted before a later edit")
	}
	m.settingsCard().State().KeyCursor = 1 // review
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInPicker(t, runeKey("t")) // ctrl+t, while the save is pending
	m.applyCmd(t, cmd)
	saved := savedKeys(t, m)
	if !strings.Contains(saved, `"detach":["f9"]`) {
		t.Fatalf("the captured detach save is missing:\n%s", saved)
	}
	if strings.Contains(saved, `"review":["ctrl+t"]`) {
		t.Fatalf("a later edit reached the captured save:\n%s", saved)
	}
	if got := m.services.keys.Binding(keybind.Detach).Label(); got != "f9" {
		t.Fatalf("runtime detach = %q, want f9", got)
	}
	if got := m.services.keys.Binding(keybind.Review).Label(); got != "ctrl+r" {
		t.Fatalf("runtime review = %q, want the captured default", got)
	}
}

func TestKeySavePartialFailureCommitsListOnly(t *testing.T) {
	m := keyPickerModel(t)
	m.settingsCard().State().KeyCursor = listRow(t, m, keybind.NewSession)
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInPicker(t, runeKey("N"))
	m.settingsCard().State().KeyCursor = 0
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyF9})
	original := m.services.store.SetKeys
	failOnce := true
	m.services.saveKeys = func(keys keybind.Table) error {
		if keys.Scope() == keybind.ScopeSession && failOnce {
			failOnce = false
			return errors.New("scripted session write failure")
		}
		return original(keys)
	}

	cmd := m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc})
	m.applyCmd(t, cmd)
	saved := savedKeys(t, m)
	if !strings.Contains(saved, `"new_session":["N"]`) {
		t.Fatalf("the committed list table is missing:\n%s", saved)
	}
	if strings.Contains(saved, `"detach":["f9"]`) {
		t.Fatalf("the failed session table reached the store:\n%s", saved)
	}
	if got := m.services.listKeys.Binding(keybind.NewSession).Label(); got != "N" {
		t.Fatalf("runtime list new_session = %q, want N", got)
	}
	if got := m.services.keys.Binding(keybind.Detach).Label(); got != `ctrl+q / ctrl+\` {
		t.Fatalf("runtime detach changed after a failed save: %q", got)
	}
	if m.errBar.text == "" {
		t.Fatal("the failed write should report its reason")
	}
	if got := m.settingsCard().State().Tables[0].Binding(keybind.Detach).Label(); got != "f9" {
		t.Fatalf("the picker should keep the uncommitted table: %q", got)
	}
	m.settingsCard().State().Field = uisettings.FieldKeybindings
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.settingsCard().State().KeyPicker {
		t.Fatalf("reopening the picker failed: %q", m.errBar.text)
	}
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyF9})
	cmd = m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc})
	m.applyCmd(t, cmd)
	if saved := savedKeys(t, m); !strings.Contains(saved, `"detach":["f9"]`) {
		t.Fatalf("the resubmission should finish the save:\n%s", saved)
	}
	if got := m.services.keys.Binding(keybind.Detach).Label(); got != "f9" {
		t.Fatalf("runtime detach = %q after the resubmission, want f9", got)
	}
}

func TestBlockedKeySaveLeavesNavigationAndResizeRunning(t *testing.T) {
	m := keyPickerModel(t)
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyF9})
	original := m.services.store.SetKeys
	started := make(chan struct{})
	release := make(chan struct{})
	m.services.saveKeys = func(keys keybind.Table) error {
		close(started)
		<-release
		return original(keys)
	}

	cmd := m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("the save should be accepted before the worker blocks")
	}
	completed := make(chan tea.Msg, 1)
	go func() { completed <- cmd() }()
	<-started
	done := make(chan struct{})
	go func() {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
		m.Update(tea.WindowSizeMsg{Width: 150, Height: 45})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		close(release)
		<-completed
		t.Fatal("navigation or resize waited on the running key save")
	}
	close(release)
	m.applyTestMsg(t, <-completed)
	m.drainEffects(t)
	if saved := savedKeys(t, m); !strings.Contains(saved, `"detach":["f9"]`) {
		t.Fatalf("the store should carry the new key once the writer unblocks:\n%s", saved)
	}
	if m.layout.width != 150 || m.layout.height != 45 {
		t.Fatalf("resize lost behind the blocked save: %dx%d", m.layout.width, m.layout.height)
	}
}

func TestKeySaveWithoutChangesQueuesNothing(t *testing.T) {
	m := keyPickerModel(t)
	if cmd := m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc}); cmd != nil {
		t.Fatal("an unchanged table should not enqueue save work")
	}
	if m.effects.Main.Active != nil || len(m.effects.Main.Pending) != 0 {
		t.Fatal("an unchanged table left a job in the lane")
	}
}

func TestKeySaveCapturesTheInstanceWriter(t *testing.T) {
	m := keyPickerModel(t)
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyF9})
	calls := 0
	m.services.saveKeys = func(keys keybind.Table) error { calls++; return m.services.store.SetKeys(keys) }
	cmd := m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc})
	m.services.saveKeys = func(keybind.Table) error { t.Fatal("accepted job borrowed a newer writer"); return nil }
	m.applyCmd(t, cmd)
	if calls != 1 {
		t.Fatalf("captured writer calls=%d", calls)
	}
}

func TestKeySaveCanRestoreOriginalBindingBehindPendingSave(t *testing.T) {
	m := keyPickerModel(t)
	original := m.services.keys
	m.settingsCard().State().Tables[0] = original.With(keybind.Detach, bindingOf(t, "f9"))
	m.saveSettingsKeys(m.settingsCard().State())
	m.settingsCard().State().Tables[0] = original
	m.saveSettingsKeys(m.settingsCard().State())
	m.drainEffects(t)
	if !m.services.keys.Equal(original) {
		t.Fatal("pending save overwrote the user's later return to the original binding")
	}
}

// savedKeys is both stored key rows, so a test reads what a save committed.
func savedKeys(t *testing.T, m *Model) string {
	t.Helper()
	return storedKeyRow(t, m, keybind.ScopeSession) + "\n" + storedKeyRow(t, m, keybind.ScopeList)
}

func TestReopenedKeyPickerKeepsPendingSave(t *testing.T) {
	m := keyPickerModel(t)
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyF9})
	first := m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc})
	m.settingsCard().State().Field = uisettings.FieldKeybindings
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc})
	m.applyCmd(t, first)
	m.drainEffects(t)
	if got := storedSessionKeys(t, m).Binding(keybind.Detach).Label(); got != "f9" {
		t.Fatalf("reopening and closing the picker reverted the accepted binding to %q", got)
	}
}
