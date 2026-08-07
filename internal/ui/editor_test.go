package ui

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// resolved is a path as tmux reports it back: symlinks followed, which on
// macOS is what the /var temp directories sit behind.
func resolved(t *testing.T, dir string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("resolve %q: %v", dir, err)
	}
	return real
}

// captureEditor swaps both editor seams: PATH answers only for the names
// given, and the launch is recorded instead of run.
func captureEditor(t *testing.T, installed ...string) *[]string {
	t.Helper()
	var launched []string
	prevLook, prevStart := lookPath, startEditor
	lookPath = func(name string) (string, error) {
		for _, have := range installed {
			if have == name {
				return "/usr/bin/" + name, nil
			}
		}
		return "", errors.New("not found")
	}
	startEditor = func(cmd *exec.Cmd) error {
		launched = cmd.Args
		return nil
	}
	t.Cleanup(func() { lookPath, startEditor = prevLook, prevStart })
	for _, key := range []string{"AGENT_MANAGER_EDITOR", "VISUAL", "EDITOR"} {
		t.Setenv(key, "")
	}
	return &launched
}

func TestOpenEditorLaunchesGUIEditorOnSessionDirectory(t *testing.T) {
	m := buildModel(t)
	launched := captureEditor(t, "code")
	dir := t.TempDir()
	createSession(t, m, "agent", dir, "")
	m.selectSessionRow(t, "agent")

	m.openEditor()

	// The live pane answers with the directory tmux resolved, which on
	// macOS is the target of the /var symlink the temp dir sits behind.
	want := []string{"code", resolved(t, dir)}
	if len(*launched) != 2 || (*launched)[0] != want[0] || (*launched)[1] != want[1] {
		t.Fatalf("launched %v, want %v", *launched, want)
	}
	if !strings.Contains(m.errBar.text, "code") {
		t.Fatalf("status line should name the editor, got %q", m.errBar.text)
	}
}

// A configured editor outranks anything found on PATH, and one carrying
// arguments still passes the directory as a single argument.
func TestOpenEditorPrefersConfiguredCommand(t *testing.T) {
	m := buildModel(t)
	launched := captureEditor(t, "code")
	m.cfg.Editor = "cursor -n"
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "backend")

	m.openEditor()

	// argv is sh -c <script> sh <dir>, the script's own $0 included.
	args := *launched
	if len(args) != 5 || args[0] != "sh" || args[1] != "-c" || args[4] != dir {
		t.Fatalf("launched %v, want sh -c with %q as the trailing argument", args, dir)
	}
	if !strings.HasPrefix(args[2], "cursor -n") {
		t.Fatalf("launched script = %q, want it to run cursor -n", args[2])
	}
}

// $EDITOR is usually the editor set for git, so it only decides when this
// machine has no GUI editor at all.
func TestResolveEditorFallsBackToEnvironment(t *testing.T) {
	m := buildModel(t)
	captureEditor(t)
	t.Setenv("EDITOR", "nvim")

	if got := m.resolveEditor(); got != "nvim" {
		t.Fatalf("resolveEditor() = %q, want nvim", got)
	}
	if !terminalEditors[editorName("nvim")] {
		t.Fatal("nvim should be recognised as drawing in this terminal")
	}
}

func TestOpenEditorWithoutAnyEditorExplainsItself(t *testing.T) {
	m := buildModel(t)
	launched := captureEditor(t)
	dir := t.TempDir()
	createSession(t, m, "agent", dir, "")
	m.selectSessionRow(t, "agent")

	m.openEditor()

	if len(*launched) != 0 {
		t.Fatalf("nothing should launch without an editor, got %v", *launched)
	}
	if !strings.Contains(m.errBar.text, "config.toml") {
		t.Fatalf("status line should point at the setting, got %q", m.errBar.text)
	}
}
