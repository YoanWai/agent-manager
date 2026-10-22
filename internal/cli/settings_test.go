package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/store"
)

func sampleSpecs() ([]store.SettingSpec, error) {
	return []store.SettingSpec{
		{Key: "theme", Values: []string{"classic", "nord"}, Default: "classic", Row: "theme"},
		{Key: "editor", Row: "editor"},
		{Key: "hidden_tools", Values: []string{"claude", "codex"}, List: true, Row: "CLIs"},
	}, nil
}

func settingsStore(t *testing.T, configDir string) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(configDir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestSettingsListNamesEveryKeyWithItsValueRowAndChoices(t *testing.T) {
	configDir := t.TempDir()
	if err := settingsStore(t, configDir).SetSetting("theme", "nord"); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runSettingsList(&out, sampleSpecs, nil, configDir); err != nil {
		t.Fatalf("settings list: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("settings list printed %d lines, want one per key:\n%s", len(lines), out.String())
	}
	for _, want := range []string{"theme", "nord", "classic, nord", "editor", "any text", "CLIs", "claude, codex"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("settings list lacks %q:\n%s", want, out.String())
		}
	}
}

func TestSettingsListJSONCarriesEveryField(t *testing.T) {
	var out bytes.Buffer
	if err := runSettingsList(&out, sampleSpecs, []string{"--json"}, t.TempDir()); err != nil {
		t.Fatalf("settings list --json: %v", err)
	}
	var records []settingRecord
	if err := json.Unmarshal(out.Bytes(), &records); err != nil {
		t.Fatalf("settings list --json is not a JSON array: %v\n%s", err, out.String())
	}
	if len(records) != 3 || records[0].Key != "theme" || records[0].Value != "classic" || records[0].Default != "classic" ||
		records[0].Row != "theme" || len(records[0].Values) != 2 || records[2].List != true {
		t.Fatalf("settings list --json = %+v", records)
	}
}

func TestSettingsGetPrintsTheStoredValueOrTheDefault(t *testing.T) {
	configDir := t.TempDir()
	var out bytes.Buffer
	if err := runSettingsGet(&out, sampleSpecs, []string{"theme"}, configDir); err != nil {
		t.Fatalf("settings get: %v", err)
	}
	if out.String() != "classic\n" {
		t.Fatalf("settings get theme on a fresh store = %q, want the default", out.String())
	}
	if err := settingsStore(t, configDir).SetSetting("theme", "nord"); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := runSettingsGet(&out, sampleSpecs, []string{"theme", "--json"}, configDir); err != nil {
		t.Fatalf("settings get --json: %v", err)
	}
	var record settingRecord
	if err := json.Unmarshal(out.Bytes(), &record); err != nil || record.Key != "theme" || record.Value != "nord" {
		t.Fatalf("settings get theme --json = %q (%v)", out.String(), err)
	}
}

func TestSettingsGetRefusesAnUnknownKey(t *testing.T) {
	err := runSettingsGet(&bytes.Buffer{}, sampleSpecs, []string{"colour"}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), `"colour"`) || !strings.Contains(err.Error(), "settings list") {
		t.Fatalf("settings get colour = %v, want it refused with a pointer to settings list", err)
	}
}

func TestSettingsSetWritesOneOfTheChoices(t *testing.T) {
	configDir := t.TempDir()
	var out bytes.Buffer
	if err := runSettingsSet(&out, sampleSpecs, []string{"theme", "nord"}, configDir); err != nil {
		t.Fatalf("settings set: %v", err)
	}
	if out.String() != "theme set to nord\n" {
		t.Fatalf("settings set output = %q", out.String())
	}
	if got, _ := settingsStore(t, configDir).Setting("theme"); got != "nord" {
		t.Fatalf("stored theme = %q, want nord", got)
	}
}

func TestSettingsSetRefusesAValueOutsideTheChoices(t *testing.T) {
	configDir := t.TempDir()
	err := runSettingsSet(&bytes.Buffer{}, sampleSpecs, []string{"theme", "dracula"}, configDir)
	if err == nil || !strings.Contains(err.Error(), "classic, nord") {
		t.Fatalf("settings set theme dracula = %v, want it refused with the choices", err)
	}
	if got, _ := settingsStore(t, configDir).Setting("theme"); got != "" {
		t.Fatalf("a refused value reached the store: %q", got)
	}
}

func TestSettingsSetTakesAnyEditorLineAndClearsOnEmpty(t *testing.T) {
	configDir := t.TempDir()
	if err := runSettingsSet(&bytes.Buffer{}, sampleSpecs, []string{"editor", "code -n"}, configDir); err != nil {
		t.Fatalf("settings set editor: %v", err)
	}
	if got, _ := settingsStore(t, configDir).Setting("editor"); got != "code -n" {
		t.Fatalf("stored editor = %q", got)
	}
	var out bytes.Buffer
	if err := runSettingsSet(&out, sampleSpecs, []string{"editor", ""}, configDir); err != nil {
		t.Fatalf("settings set editor '': %v", err)
	}
	if out.String() != "editor cleared\n" {
		t.Fatalf("clearing printed %q", out.String())
	}
}

func TestSettingsSetTakesACommaListWhereTheKeyDoes(t *testing.T) {
	configDir := t.TempDir()
	if err := runSettingsSet(&bytes.Buffer{}, sampleSpecs, []string{"hidden_tools", "codex,claude"}, configDir); err != nil {
		t.Fatalf("settings set hidden_tools: %v", err)
	}
	err := runSettingsSet(&bytes.Buffer{}, sampleSpecs, []string{"hidden_tools", "codex,grok"}, configDir)
	if err == nil || !strings.Contains(err.Error(), `"grok"`) {
		t.Fatalf("settings set hidden_tools codex,grok = %v", err)
	}
}

func TestSettingsSetJSONEchoesTheWrite(t *testing.T) {
	var out bytes.Buffer
	if err := runSettingsSet(&out, sampleSpecs, []string{"theme", "nord", "--json"}, t.TempDir()); err != nil {
		t.Fatalf("settings set --json: %v", err)
	}
	var record settingRecord
	if err := json.Unmarshal(out.Bytes(), &record); err != nil || record.Key != "theme" || record.Value != "nord" {
		t.Fatalf("settings set --json = %q (%v)", out.String(), err)
	}
}

func TestSettingsCreatesTheStoreBeforeTheFirstRun(t *testing.T) {
	configDir := filepath.Join(t.TempDir(), "agent-manager")
	if err := runSettingsSet(&bytes.Buffer{}, sampleSpecs, []string{"theme", "nord"}, configDir); err != nil {
		t.Fatalf("settings set with no config dir yet: %v", err)
	}
	if got, _ := settingsStore(t, configDir).Setting("theme"); got != "nord" {
		t.Fatalf("stored theme = %q", got)
	}
}

func TestSettingsVerbsCheckTheirOperands(t *testing.T) {
	configDir := t.TempDir()
	cases := []struct {
		name string
		run  func() error
	}{
		{"list operand", func() error { return runSettingsList(&bytes.Buffer{}, sampleSpecs, []string{"theme"}, configDir) }},
		{"get none", func() error { return runSettingsGet(&bytes.Buffer{}, sampleSpecs, nil, configDir) }},
		{"set one", func() error { return runSettingsSet(&bytes.Buffer{}, sampleSpecs, []string{"theme"}, configDir) }},
		{"set three", func() error {
			return runSettingsSet(&bytes.Buffer{}, sampleSpecs, []string{"theme", "nord", "extra"}, configDir)
		}},
	}
	for _, tc := range cases {
		if err := tc.run(); err == nil || !strings.Contains(err.Error(), "usage: agent-manager settings") {
			t.Errorf("%s: %v, want the usage line", tc.name, err)
		}
	}
	var out bytes.Buffer
	if err := runSettingsSet(&out, sampleSpecs, []string{"-h"}, configDir); !errors.Is(err, ErrUsageShown) || !strings.Contains(out.String(), usageSettingsSet) {
		t.Fatalf("settings set -h = %v, %q", err, out.String())
	}
}
