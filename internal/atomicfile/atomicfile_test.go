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
