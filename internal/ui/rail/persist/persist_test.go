package persist

import (
	"encoding/json"
	"testing"

	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
)

func TestSavedFoldsComeBackOnTheNextRun(t *testing.T) {
	st := openStore(t)
	folds := []uirail.HostFold{{Host: "box", Path: "web"}}
	if err := SaveFolds(st, []string{"backend/api", "backend"}, folds); err != nil {
		t.Fatalf("save folds: %v", err)
	}

	rail := Load(st)
	if !rail.IsCollapsed("backend") || !rail.IsCollapsed("backend/api") {
		t.Fatal("local folds were not restored")
	}
	if got := rail.HostCollapsed(); len(got) != 1 || got[0] != folds[0] {
		t.Fatalf("connection folds = %v, want %v", got, folds)
	}
}

func TestSavedFoldsAreSortedAndReplaced(t *testing.T) {
	st := openStore(t)
	if err := SaveFolds(st, []string{"b", "a"}, nil); err != nil {
		t.Fatalf("save folds: %v", err)
	}
	raw, err := st.Setting(CollapsedSetting)
	if err != nil {
		t.Fatalf("read setting: %v", err)
	}
	if raw != `["a","b"]` {
		t.Fatalf("collapsed setting = %s, want sorted", raw)
	}
	if err := SaveFolds(st, nil, nil); err != nil {
		t.Fatalf("save empty folds: %v", err)
	}
	if got := Collapsed(st); len(got) != 0 {
		t.Fatalf("unfolding everything left %v", got)
	}
}

func TestJunkFoldSettingsReadAsNoFolds(t *testing.T) {
	st := openStore(t)
	for _, key := range []string{CollapsedSetting, HostCollapsedSetting} {
		if err := st.SetSetting(key, "{not json"); err != nil {
			t.Fatalf("seed %s: %v", key, err)
		}
	}
	if got := Collapsed(st); got != nil {
		t.Fatalf("junk local folds read as %v", got)
	}
	if got := Load(st).HostCollapsed(); len(got) != 0 {
		t.Fatalf("junk connection folds read as %v", got)
	}
}

func TestSavedCollapsedSnapshotRoundTripsThroughJSON(t *testing.T) {
	st := openStore(t)
	if err := SaveFolds(st, []string{"backend"}, nil); err != nil {
		t.Fatalf("save folds: %v", err)
	}
	raw, err := st.Setting(CollapsedSetting)
	if err != nil {
		t.Fatalf("read setting: %v", err)
	}
	var stored []string
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		t.Fatalf("stored snapshot is not the path list the Rail reads: %v", err)
	}
	if len(stored) != 1 || stored[0] != "backend" {
		t.Fatalf("stored = %v", stored)
	}
}
