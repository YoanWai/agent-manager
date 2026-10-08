package store

import "testing"

func TestPaneSizeRoundTripsAndRefusesJunk(t *testing.T) {
	st := newTestStore(t)
	width, height, err := st.PaneSize()
	if err != nil || width != 0 || height != 0 {
		t.Fatalf("unrecorded pane size = %dx%d, err = %v", width, height, err)
	}
	if err := st.SetPaneSize(131, 47); err != nil {
		t.Fatalf("SetPaneSize: %v", err)
	}
	if width, height, err = st.PaneSize(); err != nil || width != 131 || height != 47 {
		t.Fatalf("pane size = %dx%d, err = %v, want 131x47", width, height, err)
	}
	for _, junk := range []string{"wide", "131x47zz", "131", "x47"} {
		if err := st.SetSetting(paneSizeSetting, junk); err != nil {
			t.Fatalf("SetSetting: %v", err)
		}
		if _, _, err := st.PaneSize(); err == nil {
			t.Fatalf("pane size %q was read as a size", junk)
		}
	}
}

func TestCoordinationWaitsForTheUserUntilSetProactive(t *testing.T) {
	st := newTestStore(t)
	if proactive, err := st.ProactiveCoordination(); err != nil || proactive {
		t.Fatalf("an unset store is proactive = %v, err = %v; want on request", proactive, err)
	}
	for _, want := range []bool{true, false} {
		if err := st.SetProactiveCoordination(want); err != nil {
			t.Fatalf("SetProactiveCoordination(%v): %v", want, err)
		}
		if proactive, err := st.ProactiveCoordination(); err != nil || proactive != want {
			t.Fatalf("proactive = %v, err = %v; want %v", proactive, err, want)
		}
	}
}

func TestSettingsCLIChoicesRoundTrip(t *testing.T) {
	st := newTestStore(t)
	if tool, err := st.DefaultTool(); err != nil || tool != "" {
		t.Fatalf("unset default tool = %q, %v; want empty, nil", tool, err)
	}
	if hidden, err := st.HiddenTools(); err != nil || len(hidden) != 0 {
		t.Fatalf("unset hidden tools = %v, %v; want none", hidden, err)
	}
	if err := st.SetDefaultTool("codex"); err != nil {
		t.Fatalf("SetDefaultTool: %v", err)
	}
	if tool, err := st.DefaultTool(); err != nil || tool != "codex" {
		t.Fatalf("default tool = %q, %v; want codex", tool, err)
	}
	if err := st.SetHiddenTools(map[string]bool{"grok": true, "codex": true, "pi": false}); err != nil {
		t.Fatalf("SetHiddenTools: %v", err)
	}
	if raw, err := st.Setting(hiddenToolsSetting); err != nil || raw != "codex,grok" {
		t.Fatalf("stored hidden tools = %q, %v; want the sorted names that are on", raw, err)
	}
	if err := st.SetSetting(hiddenToolsSetting, "codex, grok"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	hidden, err := st.HiddenTools()
	if err != nil || len(hidden) != 2 || !hidden["codex"] || !hidden["grok"] {
		t.Fatalf("hidden tools = %v, %v; want codex and grok", hidden, err)
	}
}
