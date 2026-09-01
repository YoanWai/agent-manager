package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
)

// guiEditors are probed on PATH when nothing is configured, in the order
// preferred.
var guiEditors = []string{"code", "cursor", "windsurf", "zed", "subl", "idea"}

// detachedEditors open a window of their own and return at once, leaving
// the manager on screen. Everything else takes the terminal over, which is
// the safer way round: an editor that draws in the terminal is simply
// broken when started detached, while a windowed one run through
// ExecProcess returns immediately and costs a repaint. An unknown name
// therefore takes the screen rather than disappearing into the background.
var detachedEditors = map[string]bool{
	"code": true, "code-insiders": true, "cursor": true, "windsurf": true,
	"zed": true, "subl": true, "idea": true,
	// The OS openers hand the path to whichever app is registered for it
	// and exit, so a configured "open -a ..." belongs here too.
	"open": true, "xdg-open": true,
}

// lookPath and startEditor are the seams tests swap to control which
// editors this machine has and to observe the launch instead of running it.
var (
	lookPath    = exec.LookPath
	startEditor = func(cmd *exec.Cmd) error {
		if err := cmd.Start(); err != nil {
			return err
		}
		// The manager runs for days at a time; without this every o would
		// leave the finished editor process behind holding its pipes.
		go func() { _ = cmd.Wait() }()
		return nil
	}
)

// Waiting for this result prevents a failed launch from being reported as open.
type diffFileCheckedMsg struct {
	sessID   string
	repoRoot string
	gen      int
	path     string
	err      error
}

type editorDoneMsg struct {
	name       string
	path       string
	err        error
	tookScreen bool
}

func (m *Model) openEditor() (tea.Model, tea.Cmd) {
	if _, ok := m.selectedRow(); !ok {
		return m, nil
	}
	dir, ok := m.rowDir()
	if !ok {
		m.errBar.text = "directory no longer exists: " + dir
		return m, nil
	}
	return m.launchEditor(dir)
}

func (m *Model) openDiffFile() (tea.Model, tea.Cmd) {
	fd := m.currentFileDiff()
	if fd == nil || m.diffFileHidden(fd) || m.diff.set.Repo.Root == "" {
		return m, nil
	}
	return m, diffFileCheckCmd(diffFileCheckedMsg{
		sessID:   m.diff.sessID,
		repoRoot: m.diff.repoSel,
		gen:      m.diff.gen,
		path:     filepath.Join(m.diff.set.Repo.Root, fd.File.Path),
	})
}

// Reading the filesystem is I/O, which Update must not do: a slow stat
// would hold the next keystroke.
func diffFileCheckCmd(msg diffFileCheckedMsg) tea.Cmd {
	return func() tea.Msg {
		_, msg.err = os.Stat(msg.path)
		return msg
	}
}

func (m *Model) handleDiffFileChecked(msg diffFileCheckedMsg) (tea.Model, tea.Cmd) {
	// A review closed, retargeted, or moved to another file while the stat
	// ran asked for a file the screen no longer shows.
	if !m.diff.active || msg.sessID != m.diff.sessID || msg.repoRoot != m.diff.repoSel || msg.gen != m.diff.gen {
		return m, nil
	}
	fd := m.currentFileDiff()
	if fd == nil || filepath.Join(m.diff.set.Repo.Root, fd.File.Path) != msg.path {
		return m, nil
	}
	if msg.err != nil {
		if os.IsNotExist(msg.err) {
			m.errBar.text = "file no longer exists: " + msg.path
		} else {
			m.errBar.text = "checking file " + msg.path + ": " + msg.err.Error()
		}
		return m, nil
	}
	return m.launchEditor(msg.path)
}

func (m *Model) launchEditor(path string) (tea.Model, tea.Cmd) {
	line := m.resolveEditor()
	cmd, ok := editorCommand(line, path)
	if !ok {
		m.errBar.text = `no editor found: set editor = "code" in config.toml`
		return m, nil
	}
	m.errBar.text = ""
	if !detachedEditors[editorName(line)] {
		return m, execTerminalProcess(cmd, func(err error) tea.Msg {
			return editorDoneMsg{err: err, tookScreen: true}
		})
	}
	return m, startEditorCmd(cmd, editorName(line), path)
}

// Starting a process is exec, which Update must not do: a slow launch
// would hold the next keystroke.
func startEditorCmd(cmd *exec.Cmd, name, path string) tea.Cmd {
	return func() tea.Msg {
		if err := startEditor(cmd); err != nil {
			return editorDoneMsg{err: err}
		}
		return editorDoneMsg{name: name, path: path}
	}
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

// Editor settings and environment variables are parsed as argv, never shell code.
func editorCommand(line, path string) (*exec.Cmd, bool) {
	argv := splitEditorLine(line)
	if len(argv) == 0 {
		return nil, false
	}
	args := append(append([]string{}, argv[1:]...), path)
	return exec.Command(argv[0], args...), true
}

// splitEditorLine splits an editor line into argv, grouping on single and
// double quotes so an argument can carry spaces ("open -a 'Visual Studio
// Code'"). Quoting is all it borrows from a shell; an unclosed quote runs
// to the end of the line rather than failing, since the line is a setting
// rather than a program.
func splitEditorLine(line string) []string {
	var argv []string
	var current strings.Builder
	quote := rune(0)
	quoted := false
	flush := func() {
		if current.Len() > 0 || quoted {
			argv = append(argv, current.String())
			current.Reset()
			quoted = false
		}
	}
	for _, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
				continue
			}
			current.WriteRune(r)
		case r == '\'' || r == '"':
			quote, quoted = r, true
		case unicode.IsSpace(r):
			flush()
		default:
			current.WriteRune(r)
		}
	}
	flush()
	return argv
}

// editorName is the command word an editor line starts with: what decides
// where it draws, and what the status line calls it.
func editorName(line string) string {
	argv := splitEditorLine(line)
	if len(argv) == 0 {
		return ""
	}
	return filepath.Base(argv[0])
}
