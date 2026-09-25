package store

import (
	"testing"
)

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
