package ui

import (
	"testing"

	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	tea "github.com/charmbracelet/bubbletea"
)

func TestOpenSettingsDefersBlockedStoreRead(t *testing.T) {
	m := buildModel(t)
	reader := &blockedSettingReader{
		started: make(chan struct{}),
		release: make(chan struct{}),
		values:  map[string]string{uisettings.WorktreeSetting: "on"},
	}
	opened := make(chan tea.Cmd, 1)
	go func() { opened <- m.openSettingsWithReader(reader) }()

	var cmd tea.Cmd
	select {
	case <-reader.started:
		close(reader.release)
		<-opened
		t.Fatal("opening settings read the store on the update path")
	case cmd = <-opened:
	}
	if cmd == nil {
		t.Fatal("opening settings did not return the deferred refresh")
	}
	select {
	case <-reader.started:
		t.Fatal("settings reader ran before its Bubble Tea command")
	default:
	}

	// Opening batches the store read with the editor probe; each runs as
	// its own command, so the blocked read holds only itself.
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatal("opening settings should batch the store read with the editor probe")
	}
	completed := make(chan tea.Msg, len(batch))
	for _, each := range batch {
		go func() { completed <- each() }()
	}
	<-reader.started
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 151, Height: 45})
	m = updated.(*Model)
	if m.layout.width != 151 {
		t.Fatal("a blocked settings reader blocked an unrelated window update")
	}
	close(reader.release)
	for range batch {
		m.applyTestMsg(t, <-completed)
	}
	if !m.settingsCard().State().WorktreeDefault {
		t.Fatal("the accepted deferred refresh did not update the dialog")
	}
}

func TestOpenCLIPickerDoesNotReadStore(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.openSettings())
	if err := m.services.store.Close(); err != nil {
		t.Fatal(err)
	}
	m.errBar.text = ""
	m.settingsCard().State().CLIHidden = nil
	m.openSettingsCLIPicker(t)
	if m.errBar.text != "" {
		t.Fatalf("opening the CLI picker read the closed store: %q", m.errBar.text)
	}
}

func TestSettingsOpenRefreshesExternalChanges(t *testing.T) {
	m := buildModel(t)
	if err := m.services.store.SetSetting(uisettings.WorktreeSetting, "on"); err != nil {
		t.Fatal(err)
	}
	if err := m.services.store.SetSetting(uisettings.HiddenToolsSetting, "claude"); err != nil {
		t.Fatal(err)
	}

	cmd := m.openSettings()
	if m.settingsCard().State().WorktreeDefault || m.settingsCard().State().CLIHidden["claude"] {
		t.Fatal("opening should paint the cached preferences before the refresh")
	}
	m.applyTestMsg(t, cmd())
	if !m.settingsCard().State().WorktreeDefault || !m.settingsCard().State().CLIHidden["claude"] {
		t.Fatalf("external settings were not refreshed: worktree=%t hidden=%v", m.settingsCard().State().WorktreeDefault, m.settingsCard().State().CLIHidden)
	}
}

func TestSettingsLoadRefusesReopenedDialog(t *testing.T) {
	m := buildModel(t)
	cmd := m.openSettings()
	firstGeneration := m.settings.gen
	if err := m.services.store.SetSetting(uisettings.WorktreeSetting, "on"); err != nil {
		t.Fatal(err)
	}
	loaded := cmd()

	m.openSettings()
	if m.settings.gen == firstGeneration {
		t.Fatal("reopening settings reused the old generation")
	}
	m.applyTestMsg(t, loaded)
	if m.settingsCard().State().WorktreeDefault {
		t.Fatal("the first dialog's load overwrote the reopened dialog")
	}
}

func TestSettingsLoadRefusesEditsAndSubpickers(t *testing.T) {
	t.Run("staged edit", func(t *testing.T) {
		m := buildModel(t)
		cmd := m.openSettings()
		m.settingsCard().State().Field = uisettings.FieldWorktree
		m.cycleSetting(1)
		m.applyTestMsg(t, cmd())
		if !m.settingsCard().State().WorktreeDefault {
			t.Fatal("the async load overwrote a staged setting")
		}
	})

	t.Run("CLI picker", func(t *testing.T) {
		m := buildModel(t)
		cmd := m.openSettings()
		if err := m.services.store.SetSetting(uisettings.HiddenToolsSetting, "claude"); err != nil {
			t.Fatal(err)
		}
		m.openSettingsCLIPicker(t)
		m.applyTestMsg(t, cmd())
		if m.settingsCard().State().CLIHidden["claude"] {
			t.Fatal("the async load replaced an open CLI picker")
		}
	})

	t.Run("key picker", func(t *testing.T) {
		m := buildModel(t)
		cmd := m.openSettings()
		m.settingsCard().State().KeyPicker = true
		before := m.settingsCard().State().WorktreeDefault
		if err := m.services.store.SetSetting(uisettings.WorktreeSetting, "on"); err != nil {
			t.Fatal(err)
		}
		m.applyTestMsg(t, cmd())
		if m.settingsCard().State().WorktreeDefault != before {
			t.Fatal("the async load replaced an open key picker")
		}
	})
}

func TestSettingsReopenUsesOptimisticPendingSave(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	m.settingsCard().State().Field = uisettings.FieldWorktree
	m.cycleSetting(1)
	_, save := m.handleSettingsKey(key("enter"))
	if save == nil {
		t.Fatal("settings save was not queued")
	}

	// Only the editor PATH probe may run; a store load would race the save.
	if refresh := m.openSettings(); refresh != nil {
		if _, probe := refresh().(editorsProbedMsg); !probe {
			t.Fatal("a dialog reopened over a pending save should use the optimistic cache")
		}
	}
	if !m.settingsCard().State().WorktreeDefault {
		t.Fatal("the reopened dialog lost the pending worktree choice")
	}
	m.applyCmd(t, save)
}
