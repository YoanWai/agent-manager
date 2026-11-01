package ui

import (
	"fmt"
	"os"
	"time"

	"github.com/YoanWai/agent-manager/internal/clipboard"
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/termbrowser"
	"github.com/YoanWai/agent-manager/internal/termseq"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	uisession "github.com/YoanWai/agent-manager/internal/ui/session"
	uilaunch "github.com/YoanWai/agent-manager/internal/ui/spawn/launch"
	uiterminal "github.com/YoanWai/agent-manager/internal/ui/terminal"
	tea "github.com/charmbracelet/bubbletea"
)

// terminalKeyWindow is how long after handling a t or a b another one reads
// as autorepeat rather than a second request: longer than the pause before a
// held key starts repeating and than the interval it repeats at. It runs
// from the end of the spawn, so the quiet gap between two deliberate
// tabs is this window plus the spawn.
const terminalKeyWindow = 250 * time.Millisecond

func (m *Model) shellTool() (string, config.Tool) {
	return m.services.cfg.Terminal()
}

// isShell reports whether a session's row runs the terminal.
func (m *Model) isShell(toolName string) bool {
	return toolName == config.TerminalTool
}

func (m *Model) isBrowser(toolName string) bool {
	return toolName == config.BrowserTool
}

// syncBrowserFrames tells the browsers started on the manager's server how
// to reach the terminal this manager runs in. The variable goes on the
// running server and into this process, which a server it starts inherits.
func (m *Model) syncBrowserFrames() tea.Msg {
	frames := termbrowser.FramesFor(os.Getenv("TERM_PROGRAM"), termseq.Remote())
	var err error
	if frames == "" {
		err = os.Unsetenv(termbrowser.FramesEnv)
	} else {
		err = os.Setenv(termbrowser.FramesEnv, frames)
	}
	if err == nil {
		err = m.services.tmux.SetServerEnvironment(termbrowser.FramesEnv, frames)
	}
	if err != nil {
		return errMsg{err}
	}
	return nil
}

// isTool reports whether a session's row runs a tool pane rather than
// an agent. A name the binary no longer ships answers false: a name we
// cannot resolve is not something we can claim runs a tool.
func (m *Model) isTool(toolName string) bool {
	return m.services.cfg.IsTool(toolName)
}

func (m *Model) terminalKey() (tea.Model, tea.Cmd) { return m.tabKey(m.openTerminal) }

func (m *Model) browserKey() (tea.Model, tea.Cmd) { return m.tabKey(m.openTerminalBrowser) }

// tabKey opens a tab unless the key arrived inside the burst a held key
// sends. Each keystroke pushes the window out, and the async spawn completion
// starts it again, so a burst queued while tmux creates the pane still makes
// one tab.
func (m *Model) tabKey(open func() (tea.Model, tea.Cmd)) (tea.Model, tea.Cmd) {
	if time.Since(m.ledger.lifecycle.TerminalKeyAt) < terminalKeyWindow {
		m.ledger.lifecycle.TerminalKeyAt = time.Now()
		return m, nil
	}
	model, cmd := open()
	m.ledger.lifecycle.TerminalKeyAt = time.Now()
	return model, cmd
}

func (m *Model) openTerminal() (tea.Model, tea.Cmd) {
	return m.openTerminalWithReader(newSystemTerminalDirectoryReader(m))
}

// openTerminalBrowser is named apart from the openBrowser seam, which opens
// a link in the desktop browser.
func (m *Model) openTerminalBrowser() (tea.Model, tea.Cmd) {
	return m.openTab(uilaunch.Browser, config.BrowserTool, newSystemTerminalDirectoryReader(m))
}

// terminalDirectoryRequest is where a shell row or the editor opens. The
// terminal package resolves it; this file only captures and routes it.
type terminalDirectoryRequest struct {
	target        terminalDirectoryTarget
	directory     uiterminal.Directory
	foregroundGen uint64
	rowKey        string
	returnID      string
	screen        screenID
	editor        editorResolution
}

type terminalDirectoryTarget uint8

const (
	terminalDirectorySpawn terminalDirectoryTarget = iota
	terminalDirectoryEditor
)

type terminalDirectoryMsg struct {
	request terminalDirectoryRequest
	msg     uiterminal.DirectoryMsg
}

type terminalDirectoryReader = uiterminal.DirectoryReader

func terminalDirectoryCmd(request terminalDirectoryRequest, reader terminalDirectoryReader) tea.Cmd {
	var prepare func(string) uisession.Launch
	if request.target == terminalDirectoryEditor {
		prepare = request.editor.Prepare
	}
	return func() tea.Msg {
		return terminalDirectoryMsg{request: request, msg: uiterminal.Resolve(request.directory, reader, prepare)}
	}
}

func (m *Model) openTerminalWithReader(reader terminalDirectoryReader) (tea.Model, tea.Cmd) {
	toolName, _ := m.shellTool()
	return m.openTab(uilaunch.Shell, toolName, reader)
}

// openTab spawns a shell or a browser under the selected agent, beside a
// selected shell or browser, or into the selected group.
func (m *Model) openTab(kind uilaunch.Kind, toolName string, reader terminalDirectoryReader) (tea.Model, tea.Cmd) {
	if m.rail.ShowArchived() {
		return m, nil
	}
	request, ok := m.captureTerminalDirectory(terminalDirectorySpawn)
	if !ok {
		m.reportErr("no directory to open a terminal in")
		return m, nil
	}
	w, h := m.paneTargetSize()
	spawn := uilaunch.Request{
		Kind: kind, ToolName: toolName,
		Pane:         sessioncmd.PaneSize{Width: w, Height: h},
		TerminalDir:  uilaunch.TerminalDirectoryRequest{PaneID: request.directory.PaneID, RawDir: request.directory.RawDir, Fallbacks: request.directory.Fallbacks},
		TerminalRead: terminalReader{reader: reader},
	}
	entry, _ := m.selectedRow()
	spawn.Group = entry.group
	if !entry.isGroup {
		spawn.Group = entry.sess.Group
		if m.isTool(entry.sess.Tool) {
			spawn.ParentID = entry.sess.ParentID
		} else {
			spawn.ParentID = entry.sess.ID
		}
	}
	// The way a terminal an agent opens does: the group's, else the
	// selected session's own, so a shell beside a work agent is a work shell.
	spawn.Environment = m.groupEnv(spawn.Group)
	if spawn.Environment == "" && !entry.isGroup {
		spawn.Environment = entry.sess.Environment
	}
	spawn.Name = sessioncmd.ShellName(toolName, spawn.ParentID, newID()[:4], m.workspace.sessions)
	m.dispatchSpawn(spawn)
	return m, m.nextEffectCmd()
}

func (m *Model) captureTerminalDirectory(target terminalDirectoryTarget) (terminalDirectoryRequest, bool) {
	entry, ok := m.selectedRow()
	if !ok {
		return terminalDirectoryRequest{}, false
	}
	request := terminalDirectoryRequest{target: target, foregroundGen: m.gens.foreground, rowKey: rowKey(entry), screen: m.screenID()}
	if entry.isGroup {
		request.directory.RawDir = m.workspace.groupPaths[entry.group]
		request.directory.Fallbacks = m.groupDirectoryFallbacks(entry.group)
	} else {
		request.directory.PaneID = entry.sess.ID
		request.directory.Fallbacks = []string{entry.sess.Cwd}
	}
	return request, true
}

func (m *Model) groupDirectoryFallbacks(group string) []string {
	var fallbacks []string
	for path := uirail.ParentGroup(group); path != ""; path = uirail.ParentGroup(path) {
		if dir := m.workspace.groupPaths[path]; dir != "" {
			fallbacks = append(fallbacks, dir)
		}
	}
	return append(fallbacks, m.env.workDir)
}

func (m *Model) handleTerminalDirectory(msg terminalDirectoryMsg) (tea.Model, tea.Cmd) {
	if msg.request.target == terminalDirectoryEditor {
		entry, selected := m.selectedRow()
		if m.effects.Quitting || msg.request.foregroundGen != m.gens.foreground || m.screenID() != msg.request.screen || !selected || rowKey(entry) != msg.request.rowKey {
			return m, nil
		}
	}
	if !msg.msg.OK {
		if msg.request.target == terminalDirectoryEditor {
			m.reportErr("directory no longer exists: " + msg.msg.Dir)
		} else {
			m.reportErr("no directory to open a terminal in")
		}
		return m, nil
	}
	if msg.request.target == terminalDirectoryEditor {
		returnTo := editorReturnTarget{}
		if msg.request.returnID != "" {
			returnTo = editorReturnTarget{
				sessionID:     msg.request.returnID,
				foregroundGen: msg.request.foregroundGen,
				screen:        msg.request.screen,
			}
		}
		return m.launchEditor(msg.msg.Editor, returnTo)
	}
	return m, nil
}

func (m *Model) sessionDir(sess store.Session) string {
	if path := m.workspace.panes[sess.ID].Path; path != "" {
		return path
	}
	return sess.Cwd
}

func newSystemTerminalDirectoryReader(m *Model) terminalDirectoryReader {
	return uiterminal.SystemDirectoryReader{
		PanePath: m.services.tmux.PaneCurrentPath,
		Dirs:     preflightReader{reader: systemDirectoryPreflight{git: m.services.gitDrv}},
		IsDir:    uilaunch.IsDir,
	}
}

// Seams keep link tests from mutating the desktop and clipboard.
var (
	openBrowser    = uiterminal.DefaultOpenBrowser
	copyBrowserURL = clipboard.WriteText
	openURL        = uiterminal.DefaultOpener
)

// remoteTerminal is swapped by tests to pin the session to one side of SSH.
var remoteTerminal = termseq.Remote

// The routing and the error bar read these names, so the package's messages
// arrive in the root's own shapes.
type (
	browserOpenMsg struct {
		target  string
		err     error
		copyErr error
	}
	linkPageMsg    struct{ url string }
	linkOpenErrMsg struct{ err error }
)

func linkSeams() uiterminal.Seams {
	return uiterminal.Seams{
		Open:   func(target string) error { return openBrowser(target) },
		Opener: func(url string) error { return openURL(url) },
		Copy:   func(target string) error { return copyBrowserURL(target) },
		Remote: func() bool { return remoteTerminal() },
	}
}

func openLink(target string) tea.Cmd { return terminalMsgCmd(uiterminal.OpenLink(target, linkSeams())) }

func openLinkCmd(url string) tea.Cmd { return terminalMsgCmd(uiterminal.OpenLinkCmd(url, linkSeams())) }

// terminalMsgCmd turns a link's outcome into the message the root routes.
func terminalMsgCmd(cmd tea.Cmd) tea.Cmd {
	return func() tea.Msg {
		switch msg := cmd().(type) {
		case uiterminal.BrowserOpenMsg:
			return browserOpenMsg{target: msg.Target, err: msg.Err, copyErr: msg.CopyErr}
		case uiterminal.LinkPageMsg:
			return linkPageMsg{url: msg.URL}
		case uiterminal.LinkOpenErrMsg:
			return linkOpenErrMsg{err: msg.Err}
		default:
			return msg
		}
	}
}

func (m *Model) handleBrowserOpen(msg browserOpenMsg) {
	if msg.err == nil {
		return
	}
	if msg.copyErr == nil {
		m.reportErr(fmt.Sprintf("could not open link; URL copied to clipboard: %v", msg.err))
		return
	}
	m.reportErr(fmt.Sprintf("could not open %s: %v; copying URL: %v", msg.target, msg.err, msg.copyErr))
}

func DisableAlternateScroll() error { return uiterminal.DisableAlternateScroll() }

// ResetPointer gives the terminal its own pointer back when the manager exits.
func ResetPointer() { uiterminal.ShowHand(false) }
