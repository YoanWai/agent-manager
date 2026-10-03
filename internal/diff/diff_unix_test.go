//go:build unix

package diff

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/YoanWai/agent-manager/internal/git"
	"golang.org/x/sys/unix"
)

func TestBuildSetSkipsUntrackedSpecialFiles(t *testing.T) {
	driver, dir := testRepo(t)
	write(t, dir, "tracked.go", "package a\n")
	commit(t, dir, "init")
	write(t, dir, "regular.go", "package a\n")
	if err := unix.Mkfifo(filepath.Join(dir, "blocked.pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.go")
	if err := os.WriteFile(outside, []byte("package outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "escape.go")); err != nil {
		t.Fatal(err)
	}

	set, err := BuildSet(driver, dir, git.ScopeUncommitted, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Files) != 1 || set.Files[0].File.Path != "regular.go" {
		t.Fatalf("files = %+v, want only regular.go", set.Files)
	}
}
