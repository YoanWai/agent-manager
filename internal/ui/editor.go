package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	tea "github.com/charmbracelet/bubbletea"
)

// guiEditors are probed on PATH, in the order preferred: Settings offers
// the ones found, and the first found opens when nothing is picked.
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

type editorDoneMsg struct {
	name       string
	path       string
	err        error
	tookScreen bool
	returnTo   editorReturnTarget
}

type editorReturnTarget struct {
	sessionID     string
	foregroundGen uint64
	mode          mode
}

// editorResolution captures every input to executable discovery before a
// command leaves Update. resolve may touch PATH, so only a tea.Cmd calls it.
type editorResolution struct {
	configured string
	manager    string
	visual     string
	editor     string
	gui        []string
	lookup     func(string) (string, error)
}

type editorFileCheckedMsg struct {
	result        uireview.FileCheckResult
	editor        editorLaunch
	foregroundGen uint64
}

// editorLaunch is fully prepared by a worker command. In particular, creating
// command has already resolved a bare executable through exec.LookPath, so the
// Update path only classifies and dispatches the captured process.
type editorLaunch struct {
	command *exec.Cmd
	name    string
	path    string
}

func (m *Model) openEditor() (tea.Model, tea.Cmd) {
	return m.openEditorWithReaderForReturn(systemTerminalDirectoryReader{
		tmux: m.services.tmux,
		dirs: systemDirectoryPreflight{git: m.services.gitDrv},
	}, "")
}

func (m *Model) openEditorWithReader(reader terminalDirectoryReader) (tea.Model, tea.Cmd) {
	return m.openEditorWithReaderForReturn(reader, "")
}

func (m *Model) openEditorWithReaderForReturn(reader terminalDirectoryReader, returnID string) (tea.Model, tea.Cmd) {
	request, ok := m.captureTerminalDirectory(terminalDirectoryEditor)
	if !ok {
		return m, nil
	}
	request.returnID = returnID
	request.editor = m.captureEditorResolution()
	return m, terminalDirectoryCmd(request, reader)
}

func (m *Model) openDiffFile() (tea.Model, tea.Cmd) {
	request := m.review.OpenFileRequest()
	if request == nil {
		return m, nil
	}
	return m, reviewFileCheckCmd(*request, m.captureEditorResolution(), m.foregroundGen)
}

func (m *Model) handleDiffFileChecked(msg editorFileCheckedMsg) (tea.Model, tea.Cmd) {
	if m.effects.quitting || m.mode != modeDiff || msg.foregroundGen != m.foregroundGen {
		return m, nil
	}
	path, accepted := m.review.ApplyFileCheck(msg.result)
	if !accepted {
		return m, nil
	}
	if msg.result.Err != nil {
		m.errBar.text = reviewOpenPathError(path, msg.result.Err)
		return m, nil
	}
	return m.launchEditor(msg.editor, editorReturnTarget{})
}

func (m *Model) launchEditor(editor editorLaunch, returnTo editorReturnTarget) (tea.Model, tea.Cmd) {
	if editor.command == nil {
		m.errBar.text = "no editor found: pick one in Settings > editor"
		return m, nil
	}
	m.errBar.text = ""
	if !detachedEditors[editor.name] {
		return m, execTerminalProcess(editor.command, func(err error) tea.Msg {
			return editorDoneMsg{err: err, tookScreen: true, returnTo: returnTo}
		})
	}
	return m, startEditorCmd(editor.command, editor.name, editor.path, returnTo)
}

// Starting a process is exec, which Update must not do: a slow launch
// would hold the next keystroke.
func startEditorCmd(cmd *exec.Cmd, name, path string, returnTo editorReturnTarget) tea.Cmd {
	return func() tea.Msg {
		if err := startEditor(cmd); err != nil {
			return editorDoneMsg{err: err, returnTo: returnTo}
		}
		return editorDoneMsg{name: name, path: path, returnTo: returnTo}
	}
}

func (m *Model) captureEditorResolution() editorResolution {
	resolution := detectedEditors()
	resolution.configured = m.services.editor
	return resolution
}

// detectedEditors captures what an unset Settings row falls back to.
func detectedEditors() editorResolution {
	return editorResolution{
		manager: os.Getenv("AGENT_MANAGER_EDITOR"),
		visual:  os.Getenv("VISUAL"),
		editor:  os.Getenv("EDITOR"),
		gui:     slices.Clone(guiEditors),
		lookup:  lookPath,
	}
}

// resolve picks the command that opens a directory: the one Settings holds,
// then $AGENT_MANAGER_EDITOR, then a GUI editor this machine has. $VISUAL
// and $EDITOR come last because they usually name the editor set for git
// commit messages, not the one a project is meant to open in.
func (r editorResolution) resolve() string {
	for _, line := range []string{r.configured, r.manager} {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	if r.lookup != nil {
		for _, name := range r.gui {
			if _, err := r.lookup(name); err == nil {
				return name
			}
		}
	}
	for _, fallback := range []string{r.visual, r.editor} {
		if line := strings.TrimSpace(fallback); line != "" {
			return line
		}
	}
	return ""
}

// prepare performs both executable selection and exec.Cmd construction in the
// worker that already owns directory or file validation. exec.Command may call
// exec.LookPath for a bare command, so constructing it later in Update would
// reintroduce filesystem work on the event loop.
func (r editorResolution) prepare(path string) editorLaunch {
	line := r.resolve()
	command, ok := editorCommand(line, path)
	if !ok {
		return editorLaunch{}
	}
	return editorLaunch{command: command, name: editorName(line), path: path}
}

// Settings and environment lines are parsed as argv, never shell code.
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
