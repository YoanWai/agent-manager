package diff

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/git"
)

func BenchmarkBuildSetManyFiles(b *testing.B) {
	driver, err := git.New()
	if err != nil {
		b.Skip("git not installed")
	}
	dir := b.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=bench", "GIT_AUTHOR_EMAIL=bench@example.com",
			"GIT_COMMITTER_NAME=bench", "GIT_COMMITTER_EMAIL=bench@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			b.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-b", "main")
	content := strings.Repeat("line with enough content to exercise the diff model\n", 400)
	for i := 0; i < 200; i++ {
		path := filepath.Join(dir, fmt.Sprintf("file-%03d.txt", i))
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	run("add", ".")
	run("commit", "-m", "baseline")
	for i := 0; i < 200; i++ {
		path := filepath.Join(dir, fmt.Sprintf("file-%03d.txt", i))
		if err := os.WriteFile(path, []byte("changed\n"+content), 0o644); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := BuildSet(driver, dir, git.ScopeUncommitted, ""); err != nil {
			b.Fatal(err)
		}
	}
}
