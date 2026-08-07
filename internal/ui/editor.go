package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// guiEditors are probed on PATH when nothing is configured, in the order
// preferred. They open a project window of their own and return at once.
var guiEditors = []string{"code", "cursor", "windsurf", "zed", "subl", "idea"}

// terminalEditors draw in the terminal they are launched from. Started
// detached they would have nowhere to paint, so they take the screen over
// from the manager the way an attach does.
var terminalEditors = map[string]bool{
	"vi": true, "vim": true, "nvim": true, "nano": true, "emacs": true,
	"helix": true, "hx": true, "kak": true, "micro": true, "pico": true,
}

// lookPath and startEditor are the seams tests swap to control which
// editors this machine has and to observe the launch instead of running it.
var (
	lookPath    = exec.LookPath
	startEditor = func(cmd *exec.Cmd) error { return cmd.Start() }
)

// openEditor opens the directory under the cursor in the user's editor.
func (m *Model) openEditor() (tea.Model, tea.Cmd) {
	dir, ok := m.rowDir()
	if !ok {
		m.errBar.text = "no directory to open: " + dir
		return m, nil
	}
	line := m.resolveEditor()
	if line == "" {
		m.errBar.text = `no editor found: set editor = "code" in config.toml`
		return m, nil
	}
	if terminalEditors[editorName(line)] {
		return m, tea.ExecProcess(editorCommand(line, dir), func(err error) tea.Msg {
			if err != nil {
				return errMsg{err}
			}
			return nil
		})
	}
	if err := startEditor(editorCommand(line, dir)); err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
	m.errBar.text = "opened " + dir + " in " + editorName(line)
	return m, nil
}

// resolveEditor picks the command that opens a directory: the configured
// editor, then a GUI editor this machine has. $VISUAL and $EDITOR come
// last because they usually name the editor set for git commit messages,
// not the one a project is meant to open in.
func (m *Model) resolveEditor() string {
	candidates := []string{m.cfg.Editor, os.Getenv("AGENT_MANAGER_EDITOR")}
	for _, line := range candidates {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	for _, name := range guiEditors {
		if _, err := lookPath(name); err == nil {
			return name
		}
	}
	for _, key := range []string{"VISUAL", "EDITOR"} {
		if line := strings.TrimSpace(os.Getenv(key)); line != "" {
			return line
		}
	}
	return ""
}

// editorCommand appends the directory to an editor line. A line carrying
// arguments ("code -n") goes through sh, so the directory stays one
// argument however it is spelled.
func editorCommand(line, dir string) *exec.Cmd {
	if strings.ContainsAny(line, " \t") {
		return exec.Command("sh", "-c", line+` "$@"`, "sh", dir)
	}
	return exec.Command(line, dir)
}

// editorName is the command word an editor line starts with: what decides
// where it draws, and what the status line calls it.
func editorName(line string) string {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return ""
	}
	return filepath.Base(fields[0])
}
