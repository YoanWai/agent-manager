package ui

import (
	"path/filepath"
	"slices"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
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
	if time.Since(m.ledger.terminalKeyAt) < terminalKeyWindow {
		m.ledger.terminalKeyAt = time.Now()
		return m, nil
	}
	model, cmd := m.openTerminal()
	m.ledger.terminalKeyAt = time.Now()
	return model, cmd
}

func (m *Model) openTerminal() (tea.Model, tea.Cmd) {
	return m.openTerminalWithReader(systemTerminalDirectoryReader{
		tmux: m.services.tmux,
		dirs: systemDirectoryPreflight{git: m.services.gitDrv},
	})
}

type terminalDirectoryRequest struct {
	target        terminalDirectoryTarget
	paneID        string
	rawDir        string
	fallbacks     []string
	foregroundGen uint64
	rowKey        string
	returnID      string
	mode          mode
	editor        editorResolution
}

type terminalDirectoryTarget uint8

const (
	terminalDirectorySpawn terminalDirectoryTarget = iota
	terminalDirectoryEditor
)

type terminalDirectoryMsg struct {
	request terminalDirectoryRequest
	dir     string
	ok      bool
	editor  editorLaunch
}

type terminalDirectoryReader interface {
	resolve(terminalDirectoryRequest) (string, bool)
}

type systemTerminalDirectoryReader struct {
	tmux *tmux.Driver
	dirs directoryPreflight
}

func (r systemTerminalDirectoryReader) resolve(request terminalDirectoryRequest) (string, bool) {
	if request.paneID != "" {
		if path, err := r.tmux.PaneCurrentPath(request.paneID); err == nil && isDir(path) {
			if resolved, resolveErr := filepath.EvalSymlinks(path); resolveErr == nil {
				path = resolved
			}
			return path, true
		}
	}
	return r.dirs.resolve(request.rawDir, request.fallbacks)
}

func terminalDirectoryCmd(request terminalDirectoryRequest, reader terminalDirectoryReader) tea.Cmd {
	request.fallbacks = slices.Clone(request.fallbacks)
	return func() tea.Msg {
		dir, ok := reader.resolve(request)
		var editor editorLaunch
		if ok && request.target == terminalDirectoryEditor {
			editor = request.editor.prepare(dir)
		}
		return terminalDirectoryMsg{request: request, dir: dir, ok: ok, editor: editor}
	}
}

func (m *Model) openTerminalWithReader(reader terminalDirectoryReader) (tea.Model, tea.Cmd) {
	request, ok := m.captureTerminalDirectory(terminalDirectorySpawn)
	if !ok {
		m.errBar.text = "no directory to open a terminal in"
		return m, nil
	}
	toolName, _ := m.shellTool()
	w, h := m.paneTargetSize()
	spawn := spawnRequest{
		kind: spawnShell, toolName: toolName,
		pane:         sessioncmd.PaneSize{Width: w, Height: h},
		terminalDir:  request,
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
	request := terminalDirectoryRequest{target: target, foregroundGen: m.gens.foreground, rowKey: rowKey(entry), mode: m.mode}
	if entry.isGroup {
		request.rawDir = m.workspace.groupPaths[entry.group]
		request.fallbacks = m.groupDirectoryFallbacks(entry.group)
	} else {
		request.paneID = entry.sess.ID
		request.fallbacks = []string{entry.sess.Cwd}
	}
	return request, true
}

func (m *Model) groupDirectoryFallbacks(group string) []string {
	var fallbacks []string
	for path := parentGroup(group); path != ""; path = parentGroup(path) {
		if dir := m.workspace.groupPaths[path]; dir != "" {
			fallbacks = append(fallbacks, dir)
		}
	}
	return append(fallbacks, m.env.workDir)
}

func (m *Model) handleTerminalDirectory(msg terminalDirectoryMsg) (tea.Model, tea.Cmd) {
	if msg.request.target == terminalDirectoryEditor {
		entry, selected := m.selectedRow()
		if m.effects.quitting || msg.request.foregroundGen != m.gens.foreground || m.mode != msg.request.mode || !selected || rowKey(entry) != msg.request.rowKey {
			return m, nil
		}
	}
	if !msg.ok {
		if msg.request.target == terminalDirectoryEditor {
			m.errBar.text = "directory no longer exists: " + msg.dir
		} else {
			m.errBar.text = "no directory to open a terminal in"
		}
		return m, nil
	}
	if msg.request.target == terminalDirectoryEditor {
		returnTo := editorReturnTarget{}
		if msg.request.returnID != "" {
			returnTo = editorReturnTarget{
				sessionID:     msg.request.returnID,
				foregroundGen: msg.request.foregroundGen,
				mode:          msg.request.mode,
			}
		}
		return m.launchEditor(msg.editor, returnTo)
	}
	return m, nil
}

func (m *Model) sessionDir(sess store.Session) string {
	if path := m.workspace.panes[sess.ID].Path; path != "" {
		return path
	}
	return sess.Cwd
}
