package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/keybind"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// keyPickerModel opens the settings screen on its own config directory and
// steps into the key picker, so a test never writes the real config.
func keyPickerModel(t *testing.T) *Model {
	t.Helper()
	m := buildModel(t)
	m.configDir = t.TempDir()
	if err := os.WriteFile(filepath.Join(m.configDir, "config.toml"), []byte("poll_interval = \"2s\"\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	m.keys = keybind.DefaultSession()
	m.tmux.SetSessionKeys(m.keys)
	t.Cleanup(func() {
		m.tmux.SetSessionKeys(keybind.DefaultSession())
		if err := m.tmux.EnsureBindings(); err != nil {
			t.Errorf("restore default bindings: %v", err)
		}
	})
	m.openSettings()
	m.settings.field = settingsFieldSessionBindings
	updated, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	if !m.settings.keyPicker {
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

func savedConfig(t *testing.T, m *Model) string {
	t.Helper()
	text, err := os.ReadFile(filepath.Join(m.configDir, "config.toml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	return string(text)
}

// The picker binds the key that was pressed: leaving it writes the table to
// config.toml, puts it on the model, and rebinds the tmux server so a live
// session answers to the new key.
func TestKeyPickerBindsCapturedKeyAndSavesIt(t *testing.T) {
	m := keyPickerModel(t)
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.settings.keyCapture {
		t.Fatal("enter on an action should wait for a key")
	}
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyF9})
	if got := m.settings.keys.Detach.Label(); got != "f9" {
		t.Fatalf("detach = %q, want f9", got)
	}

	cmd := m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc})
	if m.settings.keyPicker {
		t.Fatal("esc should leave the picker")
	}
	if m.errBar.text != "" {
		t.Fatalf("saving reported %q", m.errBar.text)
	}
	if got := m.keys.Detach.Label(); got != "f9" {
		t.Fatalf("model detach = %q, want f9", got)
	}
	if saved := savedConfig(t, m); !strings.Contains(saved, `detach = "f9"`) {
		t.Fatalf("config.toml should carry the new key:\n%s", saved)
	}
	if cmd == nil {
		t.Fatal("saving should refresh the live sessions")
	}
	cmd()

	bound, err := tmuxCmd("list-keys", "-T", "root").CombinedOutput()
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

// A key the manager cannot bind is refused in the picker with the same
// reason the config file gives, and nothing changes.
func TestKeyPickerRefusesAKeyTheAgentNeeds(t *testing.T) {
	m := keyPickerModel(t)
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInPicker(t, runeKey("o"))
	if !strings.Contains(m.errBar.text, "a plain key reaches the agent") {
		t.Fatalf("err = %q, want the plain-key reason", m.errBar.text)
	}
	if got := m.settings.keys.Detach.Label(); got != `ctrl+q / ctrl+\` {
		t.Fatalf("detach should be untouched, got %q", got)
	}
}

// One key serves one action, so binding review to the editor's key is
// refused rather than leaving two actions on it.
func TestKeyPickerRefusesAKeyAnotherActionOwns(t *testing.T) {
	m := keyPickerModel(t)
	m.settings.keyCursor = 1
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyF3})
	if !strings.Contains(m.errBar.text, "bound to both") {
		t.Fatalf("err = %q, want the shared-key reason", m.errBar.text)
	}
	if got := m.settings.keys.Review.Label(); got != "ctrl+r" {
		t.Fatalf("review should be untouched, got %q", got)
	}
}

// d hands an action's key to the agent, and refuses to do it for detach,
// which is the way back from a focused session.
func TestKeyPickerTurnsAnActionOffButKeepsAWayBack(t *testing.T) {
	m := keyPickerModel(t)
	m.settings.keyCursor = 2
	m.pressInPicker(t, runeKey("d"))
	if got := m.settings.keys.Editor.Label(); got != "" {
		t.Fatalf("editor should be off, got %q", got)
	}

	m.settings.keyCursor = 0
	m.pressInPicker(t, runeKey("d"))
	if !strings.Contains(m.errBar.text, "detach needs at least one key") {
		t.Fatalf("err = %q, want the detach rule", m.errBar.text)
	}
	if got := m.settings.keys.Detach.Label(); got != `ctrl+q / ctrl+\` {
		t.Fatalf("detach should be untouched, got %q", got)
	}

	m.settings.keyCursor = 2
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc})
	if saved := savedConfig(t, m); !strings.Contains(saved, `editor = "none"`) {
		t.Fatalf("config.toml should record the disabled action:\n%s", saved)
	}
}

// a adds a second key to an action rather than replacing what it answers to.
func TestKeyPickerAddsASecondKey(t *testing.T) {
	m := keyPickerModel(t)
	m.settings.keyCursor = 1
	m.pressInPicker(t, runeKey("a"))
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyF9})
	if got := m.settings.keys.Review.Label(); got != "ctrl+r / f9" {
		t.Fatalf("review = %q, want both keys", got)
	}
	m.pressInPicker(t, runeKey("a"))
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyF9})
	if !strings.Contains(m.errBar.text, "already answers to") {
		t.Fatalf("err = %q, want the duplicate note", m.errBar.text)
	}
}

// esc during capture cancels it; the key that would have been bound is not.
// r asks before it moves anything, names what would change, and on yes
// puts the shipped keys back on every action at once, added keys included;
// leaving then saves it like any other change.
func TestKeyPickerResetsEveryActionToItsDefaultAfterAsking(t *testing.T) {
	m := keyPickerModel(t)
	custom := keybind.Session{
		Detach: bindingOf(t, "ctrl+q", "f9"),
		Review: bindingOf(t),
		Editor: bindingOf(t, "f5"),
	}
	m.keys = custom
	m.tmux.SetSessionKeys(custom)
	m.settings.keys = custom

	m.pressInPicker(t, runeKey("r"))
	if !m.settings.keyReset {
		t.Fatal("r should ask before resetting")
	}
	ask := ansi.Strip(m.viewKeyPicker())
	for _, want := range []string{"Reset the in-session keys", "detach: ctrl+q / f9 back to ctrl+q / ctrl+\\", "review: off back to ctrl+r", "editor: f5 back to f3"} {
		if !strings.Contains(ask, want) {
			t.Fatalf("the question should say %q:\n%s", want, ask)
		}
	}
	m.pressInPicker(t, runeKey("n"))
	if m.settings.keyReset || !m.settings.keys.Equal(custom) {
		t.Fatalf("n should keep the keys, got %s", sessionKeysSummary(m.settings.keys))
	}

	m.pressInPicker(t, runeKey("r"))
	m.pressInPicker(t, runeKey("y"))
	if m.settings.keyReset || !m.settings.keys.Equal(keybind.DefaultSession()) {
		t.Fatalf("y should restore the defaults, got %s", sessionKeysSummary(m.settings.keys))
	}

	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc})
	if !m.keys.Equal(keybind.DefaultSession()) {
		t.Fatalf("model keys after save = %s", sessionKeysSummary(m.keys))
	}
	saved := savedConfig(t, m)
	for _, want := range []string{"[keybindings.session]", `review = "ctrl+r"`, `editor = "f3"`} {
		if !strings.Contains(saved, want) {
			t.Fatalf("saved config is missing %q:\n%s", want, saved)
		}
	}
	if strings.Contains(saved, "f9") {
		t.Fatalf("the added key should be gone:\n%s", saved)
	}
}

// A table already on its defaults has nothing to reset, so r asks nothing.
func TestKeyPickerResetOnDefaultsAsksNothing(t *testing.T) {
	m := keyPickerModel(t)
	m.pressInPicker(t, runeKey("r"))
	if m.settings.keyReset {
		t.Fatal("r on the defaults should not open the question")
	}
}

func TestKeyPickerEscapeCancelsCapture(t *testing.T) {
	m := keyPickerModel(t)
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc})
	if m.settings.keyCapture {
		t.Fatal("esc should end the capture")
	}
	if !m.settings.keyPicker {
		t.Fatal("cancelling a capture should stay in the picker")
	}
	if got := m.settings.keys.Detach.Label(); got != `ctrl+q / ctrl+\` {
		t.Fatalf("detach should be untouched, got %q", got)
	}
}

// Leaving the picker without a change writes nothing, so a visit cannot
// rewrite a config file the user hand-wrote.
func TestKeyPickerLeavesTheFileAloneWithoutAChange(t *testing.T) {
	m := keyPickerModel(t)
	before := savedConfig(t, m)
	if cmd := m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc}); cmd != nil {
		t.Fatal("an unchanged table should not refresh the sessions")
	}
	if after := savedConfig(t, m); after != before {
		t.Fatalf("the file should be untouched:\n%s", after)
	}
}

func TestKeyPickerViewNamesTheKeysAndTheCapture(t *testing.T) {
	m := keyPickerModel(t)
	view := ansi.Strip(m.viewKeyPicker())
	for _, want := range []string{"detach", `ctrl+q / ctrl+\`, "back to the manager", "every other key reaches the agent"} {
		if !strings.Contains(view, want) {
			t.Fatalf("picker view is missing %q:\n%s", want, view)
		}
	}
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	if capture := ansi.Strip(m.viewKeyPicker()); !strings.Contains(capture, "press a key") {
		t.Fatalf("a waiting row should say so:\n%s", capture)
	}
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc})
	m.settings.keyCursor = 2
	m.pressInPicker(t, runeKey("d"))
	if off := ansi.Strip(m.viewKeyPicker()); !strings.Contains(off, "off, the agent gets it") {
		t.Fatalf("a disabled action should say where its key goes:\n%s", off)
	}
}

// The settings row names the keys in force, so the screen answers the
// question without opening the picker.
func TestSettingsRowSummarizesTheSessionKeys(t *testing.T) {
	m := buildModel(t)
	m.keys = keybind.DefaultSession()
	m.openSettings()
	m.settings.field = settingsFieldSessionBindings
	view := ansi.Strip(m.viewSettings())
	if !strings.Contains(view, "in-session keys") {
		t.Fatalf("settings should carry the row:\n%s", view)
	}
	if !strings.Contains(view, `ctrl+q / ctrl+\ · ctrl+r · f3`) {
		t.Fatalf("the row should name the keys in force:\n%s", view)
	}
}
