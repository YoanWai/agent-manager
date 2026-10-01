package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/keybind"
	tea "github.com/charmbracelet/bubbletea"
)

func TestKeySaveDefersDiskWritesToTheEffect(t *testing.T) {
	m := keyPickerModel(t)
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyF9})
	before := savedConfig(t, m)
	cmd := m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("a changed table should enqueue the save work")
	}
	if got := savedConfig(t, m); got != before {
		t.Fatalf("config.toml was written on the update path:\n%s", got)
	}
	m.applyCmd(t, cmd)
	if saved := savedConfig(t, m); !strings.Contains(saved, `detach = "f9"`) {
		t.Fatalf("config.toml should carry the new key:\n%s", saved)
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
	if m.effects.active == nil {
		t.Fatal("the save should be accepted before a later edit")
	}
	m.settings.keyCursor = 1 // review
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInPicker(t, runeKey("t")) // ctrl+t, while the save is pending
	m.applyCmd(t, cmd)
	saved := savedConfig(t, m)
	if !strings.Contains(saved, `detach = "f9"`) {
		t.Fatalf("the captured detach save is missing:\n%s", saved)
	}
	if strings.Contains(saved, `review = "ctrl+t"`) {
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
	m.settings.keyCursor = listRow(t, m, keybind.NewSession)
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInPicker(t, runeKey("N"))
	m.settings.keyCursor = 0
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyF9})
	original := config.SaveKeys
	failOnce := true
	m.services.saveKeys = func(dir string, keys keybind.Table) error {
		if keys.Scope() == keybind.ScopeSession && failOnce {
			failOnce = false
			return errors.New("scripted session write failure")
		}
		return original(dir, keys)
	}

	cmd := m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc})
	m.applyCmd(t, cmd)
	saved := savedConfig(t, m)
	if !strings.Contains(saved, `new_session = "N"`) {
		t.Fatalf("the committed list table is missing:\n%s", saved)
	}
	if strings.Contains(saved, `detach = "f9"`) {
		t.Fatalf("the failed session table reached the file:\n%s", saved)
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
	if got := m.settings.tables[0].Binding(keybind.Detach).Label(); got != "f9" {
		t.Fatalf("the picker should keep the uncommitted table: %q", got)
	}
	m.settings.field = settingsFieldKeybindings
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.settings.keyPicker {
		t.Fatalf("reopening the picker failed: %q", m.errBar.text)
	}
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyF9})
	cmd = m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc})
	m.applyCmd(t, cmd)
	if saved := savedConfig(t, m); !strings.Contains(saved, `detach = "f9"`) {
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
	original := config.SaveKeys
	started := make(chan struct{})
	release := make(chan struct{})
	m.services.saveKeys = func(dir string, keys keybind.Table) error {
		close(started)
		<-release
		return original(dir, keys)
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
	if saved := savedConfig(t, m); !strings.Contains(saved, `detach = "f9"`) {
		t.Fatalf("config.toml should carry the new key once the writer unblocks:\n%s", saved)
	}
	if m.width != 150 || m.height != 45 {
		t.Fatalf("resize lost behind the blocked save: %dx%d", m.width, m.height)
	}
}

func TestKeySaveWithoutChangesQueuesNothing(t *testing.T) {
	m := keyPickerModel(t)
	if cmd := m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc}); cmd != nil {
		t.Fatal("an unchanged table should not enqueue save work")
	}
	if m.effects.active != nil || len(m.effects.pending) != 0 {
		t.Fatal("an unchanged table left a job in the lane")
	}
}

func TestKeySaveCapturesTheInstanceWriter(t *testing.T) {
	m := keyPickerModel(t)
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEnter})
	m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyF9})
	calls := 0
	m.services.saveKeys = func(dir string, keys keybind.Table) error { calls++; return config.SaveKeys(dir, keys) }
	cmd := m.pressInPicker(t, tea.KeyMsg{Type: tea.KeyEsc})
	m.services.saveKeys = func(string, keybind.Table) error { t.Fatal("accepted job borrowed a newer writer"); return nil }
	m.applyCmd(t, cmd)
	if calls != 1 {
		t.Fatalf("captured writer calls=%d", calls)
	}
}

func TestKeySaveCanRestoreOriginalBindingBehindPendingSave(t *testing.T) {
	m := keyPickerModel(t)
	original := m.services.keys
	m.settings.tables[0] = original.With(keybind.Detach, bindingOf(t, "f9"))
	m.saveKeys()
	m.settings.tables[0] = original
	m.saveKeys()
	m.drainEffects(t)
	if !m.services.keys.Equal(original) {
		t.Fatal("pending save overwrote the user's later return to the original binding")
	}
}
