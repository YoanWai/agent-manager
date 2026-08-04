package ui

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The reset reaches the terminal verbatim: a wheel notch stays inert in the
// list only while the terminal is not translating it into arrow keys.
func TestDisableAlternateScrollEmitsReset(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	saved := os.Stdout
	os.Stdout = write
	emitErr := DisableAlternateScroll()
	os.Stdout = saved
	if err := write.Close(); err != nil {
		t.Fatalf("close write end: %v", err)
	}
	if emitErr != nil {
		t.Fatalf("DisableAlternateScroll: %v", emitErr)
	}
	got, err := io.ReadAll(read)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if want := "\x1b[?1007l"; string(got) != want {
		t.Fatalf("emitted %q, want %q", got, want)
	}
}

// Re-enabling alternate scroll anywhere puts the wheel back on the session
// cursor, which is the bug this reset exists to keep fixed (#110).
func TestNoAlternateScrollEnableInTree(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if name := entry.Name(); name == ".git" || name == ".claude" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || path == filepath.Join(root, "internal", "ui", "altscroll_test.go") {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(source), `?1007h`) {
			rel, _ := filepath.Rel(root, path)
			t.Errorf("%s turns alternate scroll back on", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}
