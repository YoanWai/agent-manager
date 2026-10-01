package ui

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func (m *Model) openQuickMode() tea.Cmd {
	return m.openQuickModeWithReader(storeSettingWriter{st: m.services.store})
}

func (m *Model) openQuickModeWithReader(reader settingsValueReader) tea.Cmd {
	names, index := m.cachedSpawnToolSelection()
	if len(names) == 0 {
		m.errBar.text = "no CLIs enabled: open settings (s), then CLIs, to turn some on"
		return nil
	}
	input := textarea.New()
	input.CharLimit = 2000
	input.Placeholder = "type and press enter"
	input.ShowLineNumbers = false
	input.SetPromptFunc(2, func(lineIndex int) string {
		if lineIndex == 0 {
			return keyStyle.Render("❯ ")
		}
		return "  "
	})
	input.FocusedStyle.CursorLine = lipgloss.NewStyle()
	input.SetHeight(1)
	input.Focus()
	m.errBar.text = ""
	m.forgetWorktreeCapability()
	m.quick = quickState{
		active:         true,
		composer:       composer{input: input, maxRows: quickBarMaxRows, gen: m.nextComposerGen()},
		toolNames:      names,
		toolIndex:      index,
		closeAfterSend: m.settingsCache.value(quickCloseSetting) == "close",
		worktree:       m.cachedSpawnWorktreeDefault(m.quickTargetGroup()),
	}
	if m.settingsPending > 0 {
		return m.quickWorktreeProbeCmd(false)
	}
	return settingsLoadCmd(settingsLoadRequest{target: settingsLoadQuick, generation: uint64(m.quick.gen)}, reader)
}

func (m *Model) applyCachedQuickDefaults() {
	m.quick.toolNames, m.quick.toolIndex = m.cachedSpawnToolSelection()
	m.quick.closeAfterSend = m.settingsCache.value(quickCloseSetting) == "close"
	if !m.quick.worktreeTouched {
		m.quick.worktree = m.cachedSpawnWorktreeDefault(m.quickTargetGroup())
	}
}

// handleQuickKey runs while the quick bar is docked in the sidebar: arrows
// keep moving the selection (the target follows the cursor) unless the
// caret has a prompt row to move to, enter submits against whatever is
// selected, and every other key is typed text.
func (m *Model) handleQuickKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.quick.active = false
		// Reopening the bar starts a fresh prompt, so the images this one
		// was holding have nowhere left to be referenced from.
		m.quick.release()
		return m, nil
	case "up":
		if cmd, stepped := m.quick.stepRow(msg); stepped {
			return m, cmd
		}
		return m, tea.Batch(m.moveCursor(-1), m.quickWorktreeProbeCmd(false))
	case "down":
		if cmd, stepped := m.quick.stepRow(msg); stepped {
			return m, cmd
		}
		return m, tea.Batch(m.moveCursor(1), m.quickWorktreeProbeCmd(false))
	case "tab", "alt+m":
		if len(m.quick.toolNames) > 0 {
			m.quick.toolIndex = (m.quick.toolIndex + 1) % len(m.quick.toolNames)
		}
		m.quick.defaultsTouched = true
		return m, nil
	case "shift+tab":
		if n := len(m.quick.toolNames); n > 0 {
			m.quick.toolIndex = (m.quick.toolIndex + n - 1) % n
		}
		m.quick.defaultsTouched = true
		return m, nil
	case "ctrl+t", "alt+w":
		capable, known := m.cachedWorktreeCapability(m.quickTargetDir())
		if !known {
			return m, m.quickWorktreeProbeCmd(true)
		}
		if !capable {
			m.errBar.text = "worktree sessions need a git repository: " + m.quickTargetDir() + " is not one"
			return m, nil
		}
		m.errBar.text = ""
		m.quick.worktree = !m.quick.worktree
		m.quick.worktreeTouched = true
		m.quick.defaultsTouched = true
		return m, nil
	case "enter":
		return m.submitQuick()
	}
	if cmd, handled := m.composerKey(composerQuick, msg); handled {
		return m, cmd
	}
	return m, m.quick.typeKey(msg)
}

// submitQuick answers the selected session, or spawns a new session with
// the prompt embedded when a group is selected. The bar stays active by
// default so consecutive prompts flow without re-arming; the "after quick
// send" setting closes it instead.
func (m *Model) submitQuick() (tea.Model, tea.Cmd) {
	entry, ok := m.selectedRow()
	if !ok {
		m.errBar.text = "nothing selected"
		return m, nil
	}
	if m.quick.pasting() {
		m.errBar.text = "still reading the pasted image - try again in a moment"
		return m, nil
	}
	text := m.quick.message()
	if text == "" {
		m.errBar.text = "prompt cannot be empty"
		return m, nil
	}
	if entry.isGroup {
		return m.quickSpawn(entry.group, text)
	}
	request := quickSendRequest{
		session:        entry.sess,
		composerGen:    m.quick.gen,
		draft:          m.quick.input.Value(),
		text:           text,
		closeAfterSend: m.quick.closeAfterSend,
		images:         m.quick.attachments,
	}
	if !m.dispatchQuickSend(request) {
		return m, nil
	}
	m.errBar.text = ""
	return m, m.nextEffectCmd()
}

func (m *Model) quickSpawn(group, prompt string) (tea.Model, tea.Cmd) {
	return m.quickSpawnWithReader(group, prompt, systemDirectoryPreflight{git: m.services.gitDrv})
}

func (m *Model) quickSpawnWithReader(group, prompt string, reader directoryPreflight) (tea.Model, tea.Cmd) {
	if strings.HasPrefix(prompt, "-") {
		m.errBar.text = `prompt cannot start with "-": the tool would read it as a flag`
		return m, nil
	}
	toolName := m.quickTool()
	if toolName == "" {
		m.errBar.text = "no tools configured"
		return m, nil
	}
	name := toolName + "-" + newID()[:4]
	pickWorktree := m.cachedSpawnWorktreeDefault(group)
	if m.quick.worktreeTouched {
		pickWorktree = m.quick.worktree
	}
	paneW, paneH := m.paneTargetSize()
	request := spawnRequest{
		kind:         spawnQuick,
		toolName:     toolName,
		name:         name,
		group:        group,
		prompt:       prompt,
		autoNamed:    true,
		pickWorktree: pickWorktree,
		pane:         sessioncmd.PaneSize{Width: paneW, Height: paneH},
		composerGen:  m.quick.gen,
		images:       m.quick.attachments,
		draft:        m.quick.input.Value(),
		rawDir:       m.workspace.groupPaths[group],
		dirFallbacks: m.groupDirCandidates(group),
		wantWorktree: pickWorktree,
		dirReader:    reader,
	}
	m.errBar.text = ""
	m.dispatchSpawn(request)
	return m, m.nextEffectCmd()
}

// clearQuickAfterSend empties the bar for the next prompt, and dismisses it
// entirely when the settings toggle asks for that.
func (m *Model) clearQuickAfterSend() {
	m.quick.input.SetValue("")
	m.quick.attachments = nil
	if m.quick.closeAfterSend {
		m.quick.active = false
	}
}

func (m *Model) quickWorktreeOn() bool {
	capable, known := m.cachedWorktreeCapability(m.quickTargetDir())
	if !known || !capable {
		return false
	}
	if m.quick.worktreeTouched {
		return m.quick.worktree
	}
	return m.cachedSpawnWorktreeDefault(m.quickTargetGroup())
}

// quickTargetGroup is the group a quick spawn would land in: the selected
// group, or the group holding the selected session.
func (m *Model) quickTargetGroup() string {
	entry, ok := m.selectedRow()
	if !ok {
		return ""
	}
	if entry.isGroup {
		return entry.group
	}
	return entry.sess.Group
}

// quickTargetDir is the directory a quick spawn would launch in, resolved
// the same way quickSpawn resolves it.
func (m *Model) quickTargetDir() string {
	group := m.quickTargetGroup()
	return m.capturedAbsolutePath(m.workspace.groupPaths[group], m.capturedGroupDefaultDir(group))
}

// quickTool is the spawn CLI for the current quick-mode run: the settings
// default until tab cycles it.
func (m *Model) quickTool() string {
	if len(m.quick.toolNames) == 0 {
		return ""
	}
	return m.quick.toolNames[m.quick.toolIndex]
}
