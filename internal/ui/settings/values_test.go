package settings

import "testing"

func TestRestoreUsesTheSameDefaultsAsOpening(t *testing.T) {
	for name, values := range map[string]map[string]string{
		"unset":    {DiffLayoutSetting: "", FocusKeySetting: "", ArrowStepSetting: "", NotificationsSetting: ""},
		"enabled":  {DiffLayoutSetting: "split", FocusKeySetting: "focus", ArrowStepSetting: "on", NotificationsSetting: "on"},
		"disabled": {DiffLayoutSetting: "unified", FocusKeySetting: "attach", ArrowStepSetting: "off", NotificationsSetting: "off"},
	} {
		t.Run(name, func(t *testing.T) {
			want := FromSnapshot(Snapshot{Values: values})
			got := FromSnapshot(Snapshot{})
			var restored []Readback
			for key, raw := range values {
				restored = append(restored, Readback{Key: key, Value: raw})
			}
			got.Restore(restored, "", nil, nil)
			if got.LayoutSplit != want.LayoutSplit || got.EnterFocuses != want.EnterFocuses ||
				got.ArrowStep != want.ArrowStep || got.Notifications != want.Notifications {
				t.Fatalf("restored layout/focus/arrows/notifications = %v/%v/%v/%v, want %v/%v/%v/%v",
					got.LayoutSplit, got.EnterFocuses, got.ArrowStep, got.Notifications,
					want.LayoutSplit, want.EnterFocuses, want.ArrowStep, want.Notifications)
			}
		})
	}
}
