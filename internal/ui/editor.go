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

// editorDoneMsg ends an editor request. The status line waits on it rather
// than announcing the editor from openEditor, so a launch that fails is
// never reported as one that opened: name and dir are filled once a
// windowed editor is running, and err carries a launch that failed or a
// terminal editor that exited badly.
type editorDoneMsg struct {
	name string
	dir  string
	err  error
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
	line := m.resolveEditor()
	cmd, ok := editorCommand(line, dir)
	if !ok {
		m.errBar.text = `no editor found: set editor = "code" in config.toml`
		return m, nil
	}
	m.errBar.text = ""
	if !detachedEditors[editorName(line)] {
		return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
			return editorDoneMsg{err: err}
		})
	}
	return m, startEditorCmd(cmd, editorName(line), dir)
}

// Starting a process is exec, which Update must not do: a slow launch
// would hold the next keystroke.
func startEditorCmd(cmd *exec.Cmd, name, dir string) tea.Cmd {
	return func() tea.Msg {
		if err := startEditor(cmd); err != nil {
			return editorDoneMsg{err: err}
		}
		return editorDoneMsg{name: name, dir: dir}
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

// editorCommand builds the launch from an editor line and the directory to
// open, reporting false when the line names nothing to run. The command
// runs directly rather than through a shell: two of the four places an
// editor line comes from are environment variables, and a repo that sets
// EDITOR in an .envrc must not get a shell to write into. Nothing in the
// line is expanded or substituted, and the directory is always its own
// argument, whatever it contains.
func editorCommand(line, dir string) (*exec.Cmd, bool) {
	argv := splitEditorLine(line)
	if len(argv) == 0 {
		return nil, false
	}
	args := append(append([]string{}, argv[1:]...), dir)
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
