package main

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestEveryTestFileNamesItsSource(t *testing.T) {
	orphans, err := orphanTests("../../..")
	if err != nil {
		t.Fatal(err)
	}
	if len(orphans) > 0 {
		t.Fatalf("test files with no source of the same name; rename them after their source or add them to allowed: %q", orphans)
	}
}

func TestEveryRootUISourceStartsWithItsFeature(t *testing.T) {
	strays, err := unfiledSources(filepath.Join("../../..", uiRoot))
	if err != nil {
		t.Fatal(err)
	}
	if len(strays) > 0 {
		t.Fatalf("root UI sources that start with no feature name; rename them <feature>_<concern>.go or add the feature: %q", strays)
	}
}

func TestPackagedFeaturesOnlyAdaptTheirPackage(t *testing.T) {
	unadapted, err := unadaptedSources(filepath.Join("../../..", uiRoot))
	if err != nil {
		t.Fatal(err)
	}
	if len(unadapted) > 0 {
		t.Fatalf("root UI sources of a packaged feature that are not its adapter or an effect; move the code into the package: %q", unadapted)
	}
}

func TestUnadaptedSources(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"rail_adapter.go", "rail_adapter_view.go", "rail_effect.go", "rail_reorder_effect.go", "rail_rows.go", "spawn_catalog.go", "status_bar.go", "rail_rows_test.go"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	unadapted, err := unadaptedSources(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"rail_rows.go"}; !reflect.DeepEqual(unadapted, want) {
		t.Fatalf("unadapted = %q, want %q", unadapted, want)
	}
}

// Each packaged feature has its package, and each extra is still there.
func TestPackagedFeaturesAndExtrasExist(t *testing.T) {
	for feature := range packaged {
		if info, err := os.Stat(filepath.Join("../../..", uiRoot, feature)); err != nil || !info.IsDir() {
			t.Errorf("packaged feature %s has no package under %s", feature, uiRoot)
		}
	}
	for name := range rootExtras {
		if _, err := os.Stat(filepath.Join("../../..", uiRoot, name)); err != nil {
			t.Errorf("root extra %s is gone; drop it from rootExtras", name)
		}
	}
}

// rootOnly is closed: a new UI feature starts as a package of its own.
func TestRootOnlyFeaturesAreClosed(t *testing.T) {
	closed := []string{"composer", "effects", "frame", "geometry", "group", "input", "launchhint", "model", "pathcomplete", "preview", "session", "startup", "status", "terminal", "workspace"}
	var got []string
	for feature := range rootOnly {
		got = append(got, feature)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, closed) {
		t.Fatalf("root-only features = %q, want %q: a new UI feature starts as a package under %s and joins packaged", got, closed, uiRoot)
	}
}

func TestUnfiledSources(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"settings.go", "settings_effect.go", "keys.go", "effect_settings.go", "misc.go", "misc_test.go"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	strays, err := unfiledSources(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"effect_settings.go", "misc.go"}; !reflect.DeepEqual(strays, want) {
		t.Fatalf("strays = %q, want %q", strays, want)
	}
}

func TestAllowedFilesStillExist(t *testing.T) {
	for path := range allowed {
		if _, err := os.Stat(filepath.Join("../../..", path)); err != nil {
			t.Errorf("allowed test file %s is gone; drop it from the list", path)
		}
	}
}

func TestPairing(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"rail.go", "effect_rail.go"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"rail_test.go", "rail_drag_test.go", "effect_rail_ordering_test.go", "orphan_test.go", "helpers_test.go", "rail_helpers_test.go", "diff_bench_test.go", "main_test.go", "railing_test.go"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	orphans, err := orphanTests(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"orphan_test.go", "railing_test.go"}; !reflect.DeepEqual(orphans, want) {
		t.Fatalf("orphans = %q, want %q", orphans, want)
	}
}
