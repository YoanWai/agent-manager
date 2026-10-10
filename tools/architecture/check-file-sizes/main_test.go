package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestEverySourceFileIsUnderTheCap(t *testing.T) {
	over, err := oversized("../../..")
	if err != nil {
		t.Fatal(err)
	}
	if len(over) > 0 {
		t.Fatalf("source files at or over %d lines; split them by concern: %q", maxLines, over)
	}
}

func TestOversized(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, lines int) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.Repeat("\n", lines)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("small.go", maxLines-1)
	write("big.go", maxLines)
	write("big_test.go", maxLines*2)
	over, err := oversized(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"big.go (1000)"}; !reflect.DeepEqual(over, want) {
		t.Fatalf("over = %q, want %q", over, want)
	}
}
