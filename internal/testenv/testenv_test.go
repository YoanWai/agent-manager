package testenv

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestIsolateGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	bound := map[string]string{
		"GIT_DIR":        "/elsewhere/.git",
		"GIT_WORK_TREE":  "/elsewhere",
		"GIT_INDEX_FILE": "/elsewhere/.git/index",
		"GIT_COMMON_DIR": "/elsewhere/.git",
	}
	for key, value := range bound {
		t.Setenv(key, value)
	}
	t.Setenv("GIT_EDITOR", "true")
	if err := IsolateGit(); err != nil {
		t.Fatal(err)
	}
	left := map[string]string{}
	for _, key := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_EDITOR"} {
		if value, ok := os.LookupEnv(key); ok {
			left[key] = value
		}
	}
	if want := map[string]string{"GIT_EDITOR": "true"}; !maps.Equal(left, want) {
		t.Fatalf("environment after IsolateGit = %v, want %v", left, want)
	}
}

func TestIsolateGitReportsAFailedGit(t *testing.T) {
	SkipPOSIXShell(t)
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	err := IsolateGit()
	if want := "git rev-parse --local-env-vars: exit status 3"; err == nil || err.Error() != want {
		t.Fatalf("IsolateGit() = %v, want %s", err, want)
	}
}

func TestIsolateGitWithoutGit(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("GIT_DIR", "/elsewhere/.git")
	if err := IsolateGit(); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("GIT_DIR"); got != "/elsewhere/.git" {
		t.Fatalf("GIT_DIR = %q, want it left alone", got)
	}
}
