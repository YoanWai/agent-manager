package store

import (
	"strings"
	"testing"
)

func TestSettingSpecCheckAcceptsOneOfTheChoices(t *testing.T) {
	spec := SettingSpec{Key: "list_density", Values: []string{"compact", "comfortable"}}
	if err := spec.Check("comfortable"); err != nil {
		t.Fatalf("Check(comfortable) = %v", err)
	}
}

func TestSettingSpecCheckRefusesAValueOutsideTheChoicesAndNamesThem(t *testing.T) {
	spec := SettingSpec{Key: "list_density", Values: []string{"compact", "comfortable"}}
	err := spec.Check("cozy")
	if err == nil {
		t.Fatal("Check(cozy) accepted a value outside the choices")
	}
	for _, want := range []string{"list_density", `"cozy"`, "compact, comfortable"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Check(cozy) = %q, want it to name %s", err, want)
		}
	}
}

func TestSettingSpecCheckTakesAnyTextWhenThereAreNoChoices(t *testing.T) {
	spec := SettingSpec{Key: "editor"}
	for _, value := range []string{"", "code -n", "a line with spaces and $VARS"} {
		if err := spec.Check(value); err != nil {
			t.Fatalf("Check(%q) = %v", value, err)
		}
	}
}

func TestSettingSpecCheckTakesACommaListOfTheChoices(t *testing.T) {
	spec := SettingSpec{Key: "hidden_tools", Values: []string{"claude", "codex", "gemini"}, List: true}
	for _, value := range []string{"", "codex", "codex,gemini", "codex, gemini"} {
		if err := spec.Check(value); err != nil {
			t.Fatalf("Check(%q) = %v", value, err)
		}
	}
	err := spec.Check("codex,grok")
	if err == nil || !strings.Contains(err.Error(), `"grok"`) {
		t.Fatalf("Check(codex,grok) = %v, want it to refuse grok", err)
	}
}

func TestSettingsListsEveryStoredRow(t *testing.T) {
	st := newTestStore(t)
	for key, value := range map[string]string{"theme": "nord", "notifications": "off"} {
		if err := st.SetSetting(key, value); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := st.Settings()
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	if rows["theme"] != "nord" || rows["notifications"] != "off" {
		t.Fatalf("Settings = %v", rows)
	}
}
