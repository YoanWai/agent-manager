package atomicfile

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
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
	if runtime.GOOS == "windows" {
		return // Windows has no Unix permission bits to assert.
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Fatalf("permissions = %o", got)
	}
}

func TestWriteFileRejectsAParentThatIsAFile(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(blocker, "cache.json")
	if err := WriteFile(path, []byte("content"), 0o600); err == nil {
		t.Fatal("WriteFile succeeded through a parent that is a file")
	}
	data, err := os.ReadFile(blocker)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "keep" {
		t.Fatalf("blocking file changed to %q", data)
	}
}

func TestWriteFileRejectsAReadOnlyDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	if runtime.GOOS == "windows" {
		t.Skip("chmod cannot deny writes on Windows")
	}
	dir := filepath.Join(t.TempDir(), "read-only")
	if err := os.Mkdir(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Errorf("restore directory permissions: %v", err)
		}
	})
	path := filepath.Join(dir, "cache.json")
	if err := WriteFile(path, []byte("content"), 0o600); err == nil {
		t.Fatal("WriteFile succeeded in a read-only directory")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("destination exists after failed write: %v", err)
	}
}

func TestWriteFileRemovesAStagingFileAfterFailure(t *testing.T) {
	for _, tc := range []struct {
		name           string
		reopenReadOnly bool
	}{
		{name: "chmod"},
		{name: "write", reopenReadOnly: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "cache.json")
			createTemp := func(dir, pattern string) (*os.File, error) {
				file, err := os.CreateTemp(dir, pattern)
				if err != nil {
					return nil, err
				}
				name := file.Name()
				if err := file.Close(); err != nil {
					return nil, err
				}
				if tc.reopenReadOnly {
					return os.Open(name)
				}
				return file, nil
			}
			if err := writeFile(path, []byte("content"), 0o600, createTemp); err == nil {
				t.Fatal("writeFile succeeded after the staging operation failed")
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				names := make([]string, len(entries))
				for i, entry := range entries {
					names[i] = entry.Name()
				}
				t.Fatalf("failed write left files behind: %v", names)
			}
		})
	}
}

// WriteIfChanged is what keeps concurrent launches from rewriting a generated
// file a reader is looking at, so an unchanged write must not touch the file.
func TestWriteIfChangedLeavesAnIdenticalFileAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "cache.json")
	if err := WriteIfChanged(path, []byte("same"), 0o640); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if err := WriteIfChanged(path, []byte("same"), 0o640); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("an identical write rewrote the file: %v -> %v", before.ModTime(), after.ModTime())
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "same" {
		t.Fatalf("content = %q, err = %v", got, err)
	}
}

func TestWriteIfChangedRewritesChangedContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.json")
	if err := WriteIfChanged(path, []byte("first"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := WriteIfChanged(path, []byte("second"), 0o640); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "second" {
		t.Fatalf("content = %q, err = %v", got, err)
	}
}
