package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/hooks"
)

func TestRunRenameWritesNameFile(t *testing.T) {
	dir := t.TempDir()
	if err := runRename([]string{"fix auth bug"}, "abcd1234", dir); err != nil {
		t.Fatalf("runRename: %v", err)
	}
	raw, err := os.ReadFile(hooks.NewManager(dir).NameFile("abcd1234"))
	if err != nil {
		t.Fatalf("read name file: %v", err)
	}
	if string(raw) != "fix auth bug" {
		t.Fatalf("name file = %q", raw)
	}
}

func TestRunRenameValidation(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		label     string
		args      []string
		sessionID string
	}{
		{"no args", nil, "abcd1234"},
		{"two args", []string{"a", "b"}, "abcd1234"},
		{"blank name", []string{"  "}, "abcd1234"},
		{"missing session id", []string{"name"}, ""},
		{"traversal session id", []string{"name"}, "../evil"},
		{"uppercase session id", []string{"name"}, "ABCD1234"},
	}
	for _, c := range cases {
		if err := runRename(c.args, c.sessionID, dir); err == nil {
			t.Fatalf("%s: want error", c.label)
		}
	}
}

func TestRunReviewRepoWritesMailbox(t *testing.T) {
	repo := t.TempDir()
	for _, args := range [][]string{{"init"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	configDir := t.TempDir()
	if err := runReviewRepo([]string{repo}, "abc123", configDir); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(hooks.NewManager(configDir).ReviewRepoFile("abc123"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(raw)) == "" {
		t.Fatal("mailbox should hold the resolved repo root")
	}
}

func TestRunReviewRepoRejectsBadInput(t *testing.T) {
	configDir := t.TempDir()
	if err := runReviewRepo([]string{t.TempDir()}, "", configDir); err == nil {
		t.Error("missing session id should fail")
	}
	if err := runReviewRepo([]string{t.TempDir()}, "abc123", configDir); err == nil {
		t.Error("a path that is not a repo should fail")
	}
	if err := runReviewRepo(nil, "abc123", configDir); err == nil {
		t.Error("a missing path argument should fail")
	}
}
