package tmux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateExecutesLaunchScriptAsPaneRoot(t *testing.T) {
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	stub := filepath.Join(dir, "tmux")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" >> "+ShellQuote(calls)+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	driver := &Driver{bin: stub, socket: testSocket}
	id := "launch-root"
	t.Cleanup(func() { _ = os.Remove(launchScriptPath(id)) })
	if err := driver.Create(id, dir, "cat", nil, 80, 24); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	for _, arg := range strings.Split(string(args), "\n") {
		if strings.Contains(arg, launchScriptPath(id)) {
			if !strings.HasPrefix(arg, "exec sh ") {
				t.Fatalf("launch leaves an extra shell above the pane script: %q", arg)
			}
			return
		}
	}
	t.Fatal("launch command absent")
}
