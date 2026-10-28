package store

import (
	"reflect"
	"strings"
	"testing"
)

var transferSpecs = []SettingSpec{
	{Key: "theme", Values: []string{"classic", "nord"}},
	{Key: "editor"},
	{Key: "notify_command"},
}

var allConfiguration = ConfigurationSections{Settings: true, Presets: true, Groups: true, Connections: true}

func TestConfigurationRoundTripAndMerge(t *testing.T) {
	source := reopen(t, t.TempDir())
	original := Configuration{
		Version:  1,
		Settings: map[string]string{"theme": "nord", "editor": "code --wait", "notify_command": "notify --token secret", "keybindings.list": `{"new_session":["N"]}`, "keybindings.session": `{"detach":["ctrl+q"]}`},
		Presets:  []SessionPreset{{Name: "review", Instructions: "Review the diff"}},
		Groups: []ConfigurationGroup{
			{Name: "z-last-alphabetically", Path: "/projects", Worktree: "on", Base: "main", Environment: "work"},
			{Name: "a-first-alphabetically", Path: `C:\projects`, Worktree: "off"},
		},
		Connections: []Connection{{Name: "z-server", Destination: "user@host", Binary: "~/bin/agent-manager"}, {Name: "a-server", Destination: "alias"}},
	}
	if err := source.ImportConfiguration(original, transferSpecs); err != nil {
		t.Fatal(err)
	}
	if err := source.CreateSession(sample("source-session", original.Groups[0].Name)); err != nil {
		t.Fatal(err)
	}
	if err := source.SetSetting("pane_size", "120x40"); err != nil {
		t.Fatal(err)
	}
	exported, err := source.ExportConfiguration(allConfiguration, transferSpecs)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(exported, original) {
		t.Fatalf("export = %#v, want %#v", exported, original)
	}
	target := reopen(t, t.TempDir())
	if err := target.ImportConfiguration(Configuration{Version: 1,
		Settings:    map[string]string{"theme": "classic"},
		Presets:     []SessionPreset{{Name: "review", Instructions: "Old text"}, {Name: "unrelated", Instructions: "Keep me"}},
		Groups:      []ConfigurationGroup{{Name: original.Groups[0].Name, Path: "/old"}, {Name: "unrelated"}},
		Connections: []Connection{{Name: "z-server", Destination: "old-host"}, {Name: "unrelated", Destination: "keep-host"}},
	}, transferSpecs); err != nil {
		t.Fatal(err)
	}
	if err := target.SetGroupArchived(original.Groups[0].Name, true); err != nil {
		t.Fatal(err)
	}
	if err := target.SetSetting("pane_size", "80x24"); err != nil {
		t.Fatal(err)
	}
	if err := target.CreateSession(sample("target-session", original.Groups[0].Name)); err != nil {
		t.Fatal(err)
	}
	if err := target.ImportConfiguration(exported, transferSpecs); err != nil {
		t.Fatal(err)
	}
	got, err := target.ExportConfiguration(allConfiguration, transferSpecs)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Settings, original.Settings) || len(got.Presets) != 2 || len(got.Groups) != 3 || len(got.Connections) != 3 {
		t.Fatalf("merged = %#v", got)
	}
	for _, connection := range got.Connections {
		if connection.Name == "z-server" && connection.Destination != "user@host" {
			t.Fatalf("connection was not updated = %#v", connection)
		}
		if connection.Name == "unrelated" && connection.Destination != "keep-host" {
			t.Fatalf("unrelated connection changed = %#v", connection)
		}
	}
	sessions, err := target.ListSessions(true)
	if err != nil || len(sessions) != 1 || sessions[0].ID != "target-session" {
		t.Fatalf("sessions = %#v, %v", sessions, err)
	}
	keys, err := target.ListKeys()
	if err != nil {
		t.Fatal(err)
	}
	if action, _ := keys.ActionFor("N"); action != "new_session" {
		t.Fatalf("imported key action = %q", action)
	}
	preset, _, err := target.SessionPreset("review")
	if err != nil || preset.Instructions != "Review the diff" {
		t.Fatalf("preset = %#v, %v", preset, err)
	}
	groups, err := target.Groups()
	if err != nil {
		t.Fatal(err)
	}
	if groups[0].Path != "/projects" || !groups[0].Archived {
		t.Fatalf("group = %#v", groups[0])
	}
	size, err := target.Setting("pane_size")
	if err != nil || size != "80x24" {
		t.Fatalf("runtime setting = %q, %v", size, err)
	}
	if err := target.ImportConfiguration(exported, transferSpecs); err != nil {
		t.Fatal(err)
	}
	again, err := target.ExportConfiguration(allConfiguration, transferSpecs)
	if err != nil || !reflect.DeepEqual(got, again) {
		t.Fatalf("repeat import = %#v, %v", again, err)
	}
}

func TestConfigurationKeepsKeysOfANewerRelease(t *testing.T) {
	stored := `{"detach":["ctrl+s"],"from_a_newer_release":["f12"]}`
	source := reopen(t, t.TempDir())
	if err := source.SetSetting(keysSetting("session"), stored); err != nil {
		t.Fatal(err)
	}
	exported, err := source.ExportConfiguration(allConfiguration, transferSpecs)
	if err != nil {
		t.Fatal(err)
	}
	target := reopen(t, t.TempDir())
	if err := target.ImportConfiguration(exported, transferSpecs); err != nil {
		t.Fatal(err)
	}
	raw, err := target.Setting(keysSetting("session"))
	if err != nil || raw != stored {
		t.Fatalf("imported keys = %s, %v", raw, err)
	}
}

func TestConfigurationImportRejectsInvalidDataWithoutWriting(t *testing.T) {
	cases := []struct {
		name string
		data Configuration
	}{
		{"version", Configuration{Version: 2}},
		{"runtime key", Configuration{Version: 1, Settings: map[string]string{"pane_size": "1x1"}}},
		{"setting", Configuration{Version: 1, Settings: map[string]string{"theme": "unknown"}}},
		{"keys", Configuration{Version: 1, Settings: map[string]string{"keybindings.session": `{"detach":[]}`}}},
		{"preset", Configuration{Version: 1, Presets: []SessionPreset{{Name: "bad", Instructions: ""}}}},
		{"duplicate preset", Configuration{Version: 1, Presets: []SessionPreset{{Name: "one", Instructions: "a"}, {Name: "one", Instructions: "b"}}}},
		{"group", Configuration{Version: 1, Groups: []ConfigurationGroup{{Name: ""}}}},
		{"duplicate group", Configuration{Version: 1, Groups: []ConfigurationGroup{{Name: "one"}, {Name: "one"}}}},
		{"worktree", Configuration{Version: 1, Groups: []ConfigurationGroup{{Name: "one", Worktree: "bad"}}}},
		{"connection", Configuration{Version: 1, Connections: []Connection{{Name: "host", Destination: "-oProxyCommand=bad"}}}},
		{"duplicate connection", Configuration{Version: 1, Connections: []Connection{{Name: "host", Destination: "a"}, {Name: "host", Destination: "b"}}}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			st := reopen(t, t.TempDir())
			before, err := st.Settings()
			if err != nil {
				t.Fatal(err)
			}
			if err := st.ImportConfiguration(test.data, transferSpecs); err == nil {
				t.Fatal("invalid import succeeded")
			}
			after, err := st.Settings()
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("settings changed = %#v, %v", after, err)
			}
		})
	}
}

func TestConfigurationImportRollsBackDatabaseFailure(t *testing.T) {
	st := reopen(t, t.TempDir())
	if err := st.SetSetting("theme", "classic"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`CREATE TRIGGER reject_preset BEFORE INSERT ON session_presets BEGIN SELECT RAISE(ABORT, 'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	err := st.ImportConfiguration(Configuration{Version: 1, Settings: map[string]string{"theme": "nord"}, Presets: []SessionPreset{{Name: "review", Instructions: "Review"}}}, transferSpecs)
	if err == nil || !strings.Contains(err.Error(), "test failure") {
		t.Fatalf("import = %v", err)
	}
	theme, err := st.Setting("theme")
	if err != nil || theme != "classic" {
		t.Fatalf("theme = %q, %v", theme, err)
	}
}
