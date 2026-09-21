package federation

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/git"
)

func TestRemoteReviewPreservesLazyFileAndLoadedLines(t *testing.T) {
	dir := t.TempDir()
	gitRun := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git: %s %v", out, err)
		}
	}
	gitRun("init", "-q")
	gitRun("config", "user.email", "test@example.com")
	gitRun("config", "user.name", "Test")
	path := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(path, []byte("old\n"), 0600); err != nil {
		t.Fatal(err)
	}
	gitRun("add", ".")
	gitRun("commit", "-qm", "initial")
	if err := os.WriteFile(path, []byte("new\n"), 0600); err != nil {
		t.Fatal(err)
	}
	metadata, err := BuildReview(dir, git.ScopeUncommitted, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(metadata.Set.Files) != 1 || metadata.Set.Files[0].Loaded() {
		t.Fatalf("not lazy: %+v", metadata)
	}
	loaded, err := BuildReview(dir, git.ScopeUncommitted, "", "", "sample.txt")
	if err != nil {
		t.Fatal(err)
	}
	c := fixture(t, []Host{{Name: "remote", SSH: "server", Controller: "ctl", ReviewBinary: "/opt/native manager"}})
	c.run = func(_ context.Context, cmd *exec.Cmd) ([]byte, error) {
		if !strings.Contains(cmd.Args[len(cmd.Args)-1], "'review-data'") {
			t.Fatal(cmd.Args)
		}
		if !strings.Contains(cmd.Args[len(cmd.Args)-1], "'/opt/native manager'") {
			t.Fatal("review did not use its configured helper", cmd.Args)
		}
		return json.Marshal(loaded)
	}
	result, err := c.Review(context.Background(), "remote", dir, git.ScopeUncommitted, "", "", "sample.txt")
	if err != nil {
		t.Fatal(err)
	}
	if result.File == nil || !result.File.Loaded() || !result.File.StatKnown() || len(result.File.Changes) == 0 {
		t.Fatalf("lost diff state: %+v", result.File)
	}
	if _, err := BuildReview(dir, git.ScopeUncommitted, "", "", "../secret"); err == nil {
		t.Fatal("accepted unrelated file")
	}
	if _, err := BuildReview(dir, git.Scope(44), "", "", ""); err == nil {
		t.Fatal("accepted invalid scope")
	}
}
