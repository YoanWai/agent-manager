package settings

import (
	"slices"
	"strings"
	"testing"
)

// settings list prints the specs in this order; users and tests read it.
var listOrder = []string{
	DefaultToolSetting, ThemeSetting, ThemeAutoSetting, GlyphModeSetting, BackgroundSetting,
	ListDensitySetting, SessionLayoutSetting, HideHeaderSetting, HideStatsSetting, DiffLayoutSetting,
	QuickCloseSetting, FocusKeySetting, ArrowStepSetting, MouseSetting, WorktreeSetting, BaseFetchSetting,
	CoordinationSetting, NotificationsSetting, NotifyFinishedSetting, NotifyCommandSetting,
	EditorSetting, HiddenToolsSetting,
}

func TestSpecsKeepTheirOrder(t *testing.T) {
	specs := Specs([]string{"claude"})
	keys := make([]string, len(specs))
	for i, spec := range specs {
		keys[i] = spec.Key
	}
	if !slices.Equal(keys, listOrder) {
		t.Fatalf("Specs order %v, want %v", keys, listOrder)
	}
}

// Every dialog row paints one line that carries its label, and every
// two-way row reads, writes and restores through the table.
func TestTableCoversEveryRow(t *testing.T) {
	s := openState(testContext())
	lines := strings.Split(s.View(viewContext()).Body, "\n")
	if len(lines) != FieldCount {
		t.Fatalf("the dialog paints %d lines for %d fields", len(lines), FieldCount)
	}
	written := map[string]bool{}
	for _, value := range s.Values() {
		written[value.Key] = true
	}
	for _, setting := range table {
		if setting.Row == "" {
			continue
		}
		if !strings.Contains(lines[setting.Field], setting.Row) {
			t.Errorf("line %d does not carry %q: %q", setting.Field, setting.Row, lines[setting.Field])
		}
		if setting.saved() && !written[setting.Key] {
			t.Errorf("%s is never written by a save", setting.Key)
		}
		if setting.saved() && !setting.Proactive && !slices.Contains(ReadbackKeys, setting.Key) {
			t.Errorf("%s is never read back after a failed save", setting.Key)
		}
		if setting.Flag == nil {
			continue
		}
		for _, on := range []bool{true, false} {
			if setting.on(setting.spelling(on)) != on {
				t.Errorf("%s does not round-trip %v through %q", setting.Key, on, setting.spelling(on))
			}
		}
		if setting.on("") != setting.on(setting.Default) {
			t.Errorf("%s reads an unset value as %v, its default %q as %v", setting.Key, setting.on(""), setting.Default, setting.on(setting.Default))
		}
		restored := s
		restored.Restore([]Readback{{Key: setting.Key, Value: setting.True}}, "", nil, nil)
		if !*setting.Flag(&restored) || !restored.Flag(setting.Key) {
			t.Errorf("%s does not restore from %q", setting.Key, setting.True)
		}
	}
	for _, key := range FlagKeys() {
		if byKey(key).Flag == nil {
			t.Errorf("FlagKeys names %s, which has no flag", key)
		}
	}
	if FlagOn(HideStatsSetting, "on") != true || FlagOn(ThemeSetting, "nord") != false {
		t.Fatal("FlagOn does not read the table")
	}
}
