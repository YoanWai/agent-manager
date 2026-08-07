package ui

import (
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// captureEditor swaps both editor seams: PATH answers only for the names
// given, and the launch is recorded instead of run.
func captureEditor(t *testing.T, installed ...string) *[]string {
	t.Helper()
	var launched []string
	prevLook, prevStart := lookPath, startEditor
	lookPath = func(name string) (string, error) {
		if slices.Contains(installed, name) {
			return "/usr/bin/" + name, nil
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
	if !slices.Equal(*launched, want) {
		t.Fatalf("launched %v, want %v", *launched, want)
	}
	if !strings.Contains(m.errBar.text, "code") {
		t.Fatalf("status line should name the editor, got %q", m.errBar.text)
	}
}

// A configured editor outranks anything found on PATH, and an argument
// carrying a space stays one argument without a shell to group it.
func TestOpenEditorPrefersConfiguredCommand(t *testing.T) {
	m := buildModel(t)
	launched := captureEditor(t, "code")
	m.cfg.Editor = `open -a 'Visual Studio Code'`
	dir := t.TempDir()
	if err := m.store.CreateGroup("backend", dir); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "backend")

	m.openEditor()

	want := []string{"open", "-a", "Visual Studio Code", dir}
	if !slices.Equal(*launched, want) {
		t.Fatalf("launched %v, want %v", *launched, want)
	}
}

// The line is argv, not a script: a repo that sets EDITOR in an .envrc gets
// no shell to write into, so the operators stay literal text.
func TestEditorLineIsNeverHandedToAShell(t *testing.T) {
	cmd, ok := editorCommand(`code; touch /tmp/pwned`, "/repo")
	if !ok {
		t.Fatal("editorCommand refused a usable line")
	}
	want := []string{"code;", "touch", "/tmp/pwned", "/repo"}
	if !slices.Equal(cmd.Args, want) {
		t.Fatalf("argv = %v, want %v", cmd.Args, want)
	}
}

func TestSplitEditorLineGroupsOnQuotes(t *testing.T) {
	for _, tc := range []struct {
		line string
		want []string
	}{
		{"code", []string{"code"}},
		{"code -n", []string{"code", "-n"}},
		{`open -a "Visual Studio Code"`, []string{"open", "-a", "Visual Studio Code"}},
		{`'/Applications/My App/bin/edit' -w`, []string{"/Applications/My App/bin/edit", "-w"}},
		{"   ", nil},
		{"", nil},
	} {
		if got := splitEditorLine(tc.line); !slices.Equal(got, tc.want) {
			t.Errorf("splitEditorLine(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
}

// $EDITOR is usually the editor set for git, so it only decides when this
// machine has no GUI editor at all - and a terminal editor takes the screen
// rather than being started where it cannot draw.
func TestResolveEditorFallsBackToEnvironment(t *testing.T) {
	m := buildModel(t)
	captureEditor(t)
	t.Setenv("EDITOR", "nvim")

	if got := m.resolveEditor(); got != "nvim" {
		t.Fatalf("resolveEditor() = %q, want nvim", got)
	}
	if detachedEditors[editorName("nvim")] {
		t.Fatal("nvim draws in this terminal and must not start detached")
	}
}

// An editor nobody listed is handed the screen: wrong for a windowed one
// costs a repaint, wrong for a terminal one loses the editor entirely.
func TestUnknownEditorTakesTheScreen(t *testing.T) {
	m := buildModel(t)
	launched := captureEditor(t)
	m.cfg.Editor = "my-own-edit-wrapper"
	dir := t.TempDir()
	createSession(t, m, "agent", dir, "")
	m.selectSessionRow(t, "agent")

	if _, cmd := m.openEditor(); cmd == nil {
		t.Fatal("an unknown editor should run through ExecProcess")
	}
	if len(*launched) != 0 {
		t.Fatalf("an unknown editor must not start detached, got %v", *launched)
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
