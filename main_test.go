package main

import (
	"os"
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
