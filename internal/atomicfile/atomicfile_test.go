package atomicfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileCreatesAndReplacesAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "cache.json")
	if err := WriteFile(path, []byte("first"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(path, []byte("second"), 0o640); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "second" {
		t.Fatalf("content = %q", raw)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Fatalf("permissions = %o", got)
	}
}
