package ui

import (
	"os"
	"os/exec"

	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	uisession "github.com/YoanWai/agent-manager/internal/ui/session"
	tea "github.com/charmbracelet/bubbletea"
)

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
	screen        screenID
}

// resumes reports whether a returning editor goes back into the session it
// was opened from: nothing the user did since may have moved the screen on.
func (t editorReturnTarget) resumes(foregroundGen uint64, current screenID, quitting bool) bool {
	return t.sessionID != "" && !quitting && t.foregroundGen == foregroundGen && t.screen == current
}

type editorFileCheckedMsg struct {
	result        uireview.FileCheckResult
	editor        editorLaunch
	foregroundGen uint64
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
	return m, reviewFileCheckCmd(*request, m.captureEditorResolution(), m.gens.foreground)
}

func (m *Model) handleDiffFileChecked(msg editorFileCheckedMsg) (tea.Model, tea.Cmd) {
	if m.effects.Quitting || m.base != baseReview || msg.foregroundGen != m.gens.foreground {
		return m, nil
	}
	path, accepted := m.review.ApplyFileCheck(msg.result)
	if !accepted {
		return m, nil
	}
	if msg.result.Err != nil {
		m.reportErr(reviewOpenPathError(path, msg.result.Err))
		return m, nil
	}
	return m.launchEditor(msg.editor, editorReturnTarget{})
}

func (m *Model) launchEditor(editor editorLaunch, returnTo editorReturnTarget) (tea.Model, tea.Cmd) {
	if editor.Command == nil {
		m.reportErr("no editor found: pick one in Settings > editor")
		return m, nil
	}
	m.clearErr()
	if editor.TakesScreen() {
		return m, execTerminalProcess(editor.Command, func(err error) tea.Msg {
			return editorDoneMsg{err: err, tookScreen: true, returnTo: returnTo}
		})
	}
	return m, startEditorCmd(editor.Command, editor.Name, editor.Path, returnTo)
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

func (m *Model) routeEditorMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case editorFileCheckedMsg:
		return routed(m.handleDiffFileChecked(msg))

	case editorDoneMsg:
		var resume tea.Cmd
		if msg.tookScreen {
			// The terminal comes back from an editor the way it comes back
			// from an attach: painted in the editor's background, and
			// without the mouse reporting focus mode armed on the way in.
			SyncTerminalColors()
			if m.focused() {
				resume = tea.EnableMouseCellMotion
			}
		}
		if msg.err != nil {
			// Going back into the session would hide the only account of
			// what went wrong, so a failed editor keeps the list.
			m.reportErr(msg.err.Error())
			return routed(m, resume)
		}
		if msg.name != "" {
			m.reportDone("opened " + msg.path + " in " + msg.name)
		}
		if target := msg.returnTo; target.resumes(m.gens.foreground, m.screenID(), m.effects.Quitting) {
			return routed(m, tea.Batch(resume, m.reattach(target.sessionID, m.review.Generation())))
		}
		return routed(m, resume)
	}
	return nil, nil, false
}

// The root keeps the resolution and the launch names the rest of the package
// reads: terminal.go, review_adapter.go and settings_adapter.go are wired to
// them, and their call sites belong to other prefixes.

type editorResolution struct {
	configured string
	manager    string
	visual     string
	editor     string
	gui        []string
	lookup     func(string) (string, error)
}

type editorLaunch = uisession.Launch

// detectedEditors captures what an unset Settings row falls back to.
func detectedEditors() editorResolution {
	return editorResolution{
		manager: os.Getenv("AGENT_MANAGER_EDITOR"),
		visual:  os.Getenv("VISUAL"),
		editor:  os.Getenv("EDITOR"),
		gui:     uisession.GUIEditors(),
		lookup:  lookPath,
	}
}

func (r editorResolution) resolve() string {
	return uisession.Resolve(r.configured, r.manager, r.visual, r.editor, r.gui, r.lookup)
}

func (r editorResolution) prepare(path string) editorLaunch {
	line := r.resolve()
	if command, ok := uisession.Command(line, path); ok {
		return uisession.Launch{Command: command, Name: uisession.Name(line), Path: path}
	}
	return uisession.Launch{}
}

func splitEditorLine(line string) []string { return uisession.SplitLine(line) }
