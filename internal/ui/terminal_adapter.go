package ui

import (
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	uisession "github.com/YoanWai/agent-manager/internal/ui/session"
	uilaunch "github.com/YoanWai/agent-manager/internal/ui/spawn/launch"
	uiterminal "github.com/YoanWai/agent-manager/internal/ui/terminal"
	tea "github.com/charmbracelet/bubbletea"
)

// terminalKeyWindow is how long after handling a T another one reads as
// autorepeat rather than a second request: longer than the pause before a
// held key starts repeating and than the interval it repeats at. It runs
// from the end of the spawn, so the quiet gap between two deliberate
// shells is this window plus the spawn.
const terminalKeyWindow = 250 * time.Millisecond

func (m *Model) shellTool() (string, config.Tool) {
	return m.services.cfg.ShellTool()
}

// isShell reports whether a session's tool opens a shell rather than an
// agent. A tool the binary no longer ships answers false: a name we cannot
// resolve is not something we can claim runs a shell.
func (m *Model) isShell(toolName string) bool {
	return m.services.cfg.Tools[toolName].Shell
}

// terminalKey spawns a shell unless T arrived inside the burst a held key
// sends. Each keystroke pushes the window out, and the async spawn completion
// starts it again, so a burst queued while tmux creates the pane still makes
// one shell.
func (m *Model) terminalKey() (tea.Model, tea.Cmd) {
	if time.Since(m.ledger.lifecycle.TerminalKeyAt) < terminalKeyWindow {
		m.ledger.lifecycle.TerminalKeyAt = time.Now()
		return m, nil
	}
	model, cmd := m.openTerminal()
	m.ledger.lifecycle.TerminalKeyAt = time.Now()
	return model, cmd
}

func (m *Model) openTerminal() (tea.Model, tea.Cmd) {
	return m.openTerminalWithReader(newSystemTerminalDirectoryReader(m))
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
		prepare = request.editor.prepare
	}
	return func() tea.Msg {
		return terminalDirectoryMsg{request: request, msg: uiterminal.Resolve(request.directory, reader, prepare)}
	}
}

func (m *Model) openTerminalWithReader(reader terminalDirectoryReader) (tea.Model, tea.Cmd) {
	request, ok := m.captureTerminalDirectory(terminalDirectorySpawn)
	if !ok {
		m.reportErr("no directory to open a terminal in")
		return m, nil
	}
	toolName, _ := m.shellTool()
	w, h := m.paneTargetSize()
	spawn := spawnRequest{
		kind: uilaunch.Shell, toolName: toolName,
		pane:         sessioncmd.PaneSize{Width: w, Height: h},
		terminalDir:  request.directory,
		terminalRead: reader,
	}
	entry, _ := m.selectedRow()
	spawn.group = entry.group
	if !entry.isGroup {
		spawn.group = entry.sess.Group
		if m.isShell(entry.sess.Tool) {
			spawn.parentID = entry.sess.ParentID
		} else {
			spawn.parentID = entry.sess.ID
		}
	}
	spawn.name = sessioncmd.ShellName(toolName, spawn.parentID, newID()[:4], m.workspace.sessions)
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
