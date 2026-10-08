package main

import (
	"os"
	"path/filepath"
	"reflect"
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
