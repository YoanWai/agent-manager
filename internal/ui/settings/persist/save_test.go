package persist

import (
	"errors"
	"testing"

	"github.com/YoanWai/agent-manager/internal/keybind"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
)

// scriptWriter scripts failing writes and reads so a partial outcome can be
// proven deterministically.
type scriptWriter struct {
	failOn    string
	failGetOn string
	values    map[string]string
}

func (w *scriptWriter) SetSetting(key, value string) error {
	if w.failOn == key {
		return errors.New("scripted write failure")
	}
	w.values[key] = value
	return nil
}

func (w *scriptWriter) SetProactiveCoordination(proactive bool) error {
	if w.failOn == uisettings.CoordinationSetting {
		return errors.New("scripted write failure")
	}
	if proactive {
		w.values[uisettings.CoordinationSetting] = "proactive"
	}
	return nil
}

func (w *scriptWriter) Setting(key string) (string, error) {
	if w.failGetOn == key {
		return "", errors.New("scripted read failure")
	}
	return w.values[key], nil
}

// A partial failure commits what it can in order and stops at the failing
// key, reporting how many landed.
func TestRunSaveCommitsInOrderUntilItFails(t *testing.T) {
	writer := &scriptWriter{failOn: uisettings.ThemeAutoSetting, values: map[string]string{uisettings.ThemeSetting: "nord"}}
	request := SaveRequest{Values: []uisettings.Value{
		{Key: uisettings.ThemeSetting, Value: "dracula"},
		{Key: uisettings.ThemeAutoSetting, Value: "off"},
		{Key: uisettings.DiffLayoutSetting, Value: "unified"},
	}}
	result, err := RunSave(request, writer)
	if err == nil {
		t.Fatal("expected the scripted failure")
	}
	if result.Committed != 1 || result.Tot != 3 {
		t.Fatalf("committed=%d tot=%d, want 1 of 3", result.Committed, result.Tot)
	}
	if writer.values[uisettings.ThemeSetting] != "dracula" {
		t.Fatal("the first write did not commit")
	}
	if _, ok := writer.values[uisettings.DiffLayoutSetting]; ok {
		t.Fatal("a write after the failure ran")
	}
	if result.Restored == nil {
		t.Fatal("a partial failure must read the committed keys back")
	}
}

// The hidden-CLI set is written last and counts toward the total.
func TestRunSaveWritesTheHiddenSetLast(t *testing.T) {
	writer := &scriptWriter{failOn: uisettings.HiddenToolsSetting, values: map[string]string{}}
	request := SaveRequest{
		Values: []uisettings.Value{{Key: uisettings.ThemeSetting, Value: "nord"}},
		Hidden: []string{"claude"},
	}
	result, err := RunSave(request, writer)
	if err == nil {
		t.Fatal("expected the hidden write to fail")
	}
	if result.Committed != 1 || result.Tot != 2 {
		t.Fatalf("committed=%d tot=%d, want 1 of 2", result.Committed, result.Tot)
	}
}

// The coordination row goes through the proactive writer, not a plain
// setting write.
func TestRunSaveRoutesCoordinationThroughTheProactiveWriter(t *testing.T) {
	writer := &scriptWriter{values: map[string]string{}}
	request := SaveRequest{Values: []uisettings.Value{
		{Key: uisettings.CoordinationSetting, Value: "on", Proactive: true},
	}}
	if _, err := RunSave(request, writer); err != nil {
		t.Fatal(err)
	}
	if writer.values[uisettings.CoordinationSetting] != "proactive" {
		t.Fatalf("coordination=%q, want the proactive marker", writer.values[uisettings.CoordinationSetting])
	}
}

// A read that fails is never presented as the committed value.
func TestSettingsReadbackCarriesAReadError(t *testing.T) {
	writer := &scriptWriter{failGetOn: uisettings.ListDensitySetting, values: map[string]string{}}
	values, _, _ := SettingsReadback(writer)
	var found bool
	for _, value := range values {
		if value.Key != uisettings.ListDensitySetting {
			continue
		}
		found = true
		if value.Err == nil {
			t.Fatal("the failed read lost its error")
		}
	}
	if !found {
		t.Fatal("the density key is missing from the read-back")
	}
}

// A symbols value the frame cannot paint is a read error.
func TestReadKeysRejectsAnUnknownSymbolsValue(t *testing.T) {
	writer := &scriptWriter{values: map[string]string{uisettings.GlyphModeSetting: "unknown"}}
	value := ReadKeys(writer, []string{uisettings.GlyphModeSetting})[0]
	if value.Err == nil {
		t.Fatal("an unknown symbols value must report its read error")
	}
}

// A save's captured slices are copied, so a later dialog edit never
// retargets an accepted save.
func TestSaveRequestCloneCopiesMutableSlices(t *testing.T) {
	request := SaveRequest{
		Values: []uisettings.Value{{Key: "theme", Value: "nord"}},
		Hidden: []string{"claude"},
	}
	copied := request.Clone()
	request.Values[0].Value = "changed"
	request.Hidden[0] = "changed"
	if copied.Values[0].Value == "changed" || copied.Hidden[0] == "changed" {
		t.Fatal("Clone kept the caller's slices")
	}
}

// A key save commits list first, then session, and stops on the first
// failure.
func TestRunKeysCommitsListThenSession(t *testing.T) {
	var order []string
	save := func(table keybind.Table) error {
		order = append(order, table.Scope())
		if table.Scope() == keybind.ScopeSession {
			return errors.New("scripted session failure")
		}
		return nil
	}
	result, err := RunKeys(KeysRequest{List: keybind.DefaultList(), Session: keybind.DefaultSession(), ListChanged: true, SessionChanged: true}, save)
	if err == nil {
		t.Fatal("expected the session write to fail")
	}
	if !result.ListCommitted || result.SessionCommitted {
		t.Fatalf("list=%v session=%v, want only the list committed", result.ListCommitted, result.SessionCommitted)
	}
	if len(order) != 2 || order[0] != keybind.ScopeList || order[1] != keybind.ScopeSession {
		t.Fatalf("write order %v, want list then session", order)
	}
}
