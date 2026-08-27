package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func (m *Model) openQuickMode() {
	names, index := m.defaultToolSelection()
	if len(names) == 0 {
		m.errBar.text = "no CLIs enabled: open settings (s), then CLIs, to turn some on"
		return
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
		closeAfterSend: m.quickCloseAfterSend(),
		worktree:       m.defaultWorktree(),
	}
}

// defaultToolSelection returns enabled tool names with the index of
// the configured default, ready to seed a tool picker.
func (m *Model) defaultToolSelection() ([]string, int) {
	names := m.enabledToolNames()
	current := m.defaultTool()
	index := 0
	for i, name := range names {
		if name == current {
			index = i
		}
	}
	return names, index
}

// handleQuickKey runs while the quick bar is docked in the sidebar: arrows
// keep moving the selection (the target follows the cursor), enter submits
// against whatever is selected, and every other key is typed text.
func (m *Model) handleQuickKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.quick.active = false
		// Reopening the bar starts a fresh prompt, so the images this one
		// was holding have nowhere left to be referenced from.
		m.quick.release()
		return m, nil
	case "up":
		return m, m.moveCursor(-1)
	case "down":
		return m, m.moveCursor(1)
	case "tab", "alt+m":
		if len(m.quick.toolNames) > 0 {
			m.quick.toolIndex = (m.quick.toolIndex + 1) % len(m.quick.toolNames)
		}
		return m, nil
	case "shift+tab", "alt+w":
		dir := m.quickTargetDir()
		if !m.worktreeCapable(dir) {
			m.errBar.text = "worktree sessions need a git repository: " + dir + " is not one"
			return m, nil
		}
		m.errBar.text = ""
		m.quick.worktree = !m.quickWorktreeOn()
		m.quick.worktreeTouched = true
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
	if m.isShell(entry.sess.Tool) {
		m.errBar.text = shellPromptHint(entry.sess.Name)
		return m, nil
	}
	if !m.tmux.Exists(entry.sess.ID) {
		m.errBar.text = deadSessionHint
		return m, nil
	}
	if err := m.tmux.SendText(entry.sess.ID, text); err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
	// The prompt is delivered: clear the input before anything else can
	// fail, so a retry cannot send it twice.
	m.clearQuickAfterSend()
	m.errBar.text = ""
	// A queued answer means the user expects a fresh finished alert.
	if err := m.store.SetAcked(entry.sess.ID, false); err != nil {
		m.errBar.text = "prompt sent, but clearing the alert ack failed: " + err.Error()
	}
	if err := m.store.SetLastPrompt(entry.sess.ID, text); err != nil {
		m.errBar.text = "prompt sent, but recording it for the row failed: " + err.Error()
	}
	m.requestRefresh()
	return m, nil
}

func (m *Model) quickSpawn(group, prompt string) (tea.Model, tea.Cmd) {
	if strings.HasPrefix(prompt, "-") {
		m.errBar.text = `prompt cannot start with "-": the tool would read it as a flag`
		return m, nil
	}
	toolName := m.quickTool()
	if toolName == "" {
		m.errBar.text = "no tools configured"
		return m, nil
	}
	dir, ok := resolveExistingDir(m.groupPaths[group], m.groupDefaultDir(group))
	if !ok {
		m.errBar.text = "group has no valid default path: " + dir
		return m, nil
	}
	name := toolName + "-" + newID()[:4]
	if err := m.spawnSession(toolName, name, dir, group, prompt, true, m.quickWorktreeOn()); err != nil {
		m.reportLaunchError(err)
		// A spawn the hint dialog refused leaves nothing to send, so the
		// bar closes instead of swallowing the list keys behind the dialog.
		if m.mode == modeLaunchHint {
			m.quick.active = false
			m.quick.release()
		}
		return m, nil
	}
	// Spawned sessions start outside the attention set; clear so the new row shows.
	m.statusFilter = statusFilterAll
	m.clearQuickAfterSend()
	m.errBar.text = ""
	return m, m.refreshCmd()
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

// quickWorktreeOn is the worktree state the quick bar shows and spawns
// with: the target group's default until shift+tab overrides it, and off
// whenever the target directory cannot host a worktree.
func (m *Model) quickWorktreeOn() bool {
	if !m.worktreeCapable(m.quickTargetDir()) {
		return false
	}
	if m.quick.worktreeTouched {
		return m.quick.worktree
	}
	return m.groupWorktree(m.quickTargetGroup())
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
	dir, _ := resolveExistingDir(m.groupPaths[group], m.groupDefaultDir(group))
	return dir
}

// quickTool is the spawn CLI for the current quick-mode run: the settings
// default until tab cycles it.
func (m *Model) quickTool() string {
	if len(m.quick.toolNames) == 0 {
		return ""
	}
	return m.quick.toolNames[m.quick.toolIndex]
}

// quickCloseAfterSend reports whether the quick bar should dismiss itself
// once a prompt is delivered. Staying open is the default; a stored "close"
// choice opts in. A store error is surfaced but still yields the default.
func (m *Model) quickCloseAfterSend() bool {
	chosen, err := m.store.Setting(quickCloseSetting)
	if err != nil {
		m.errBar.text = "reading quick prompt setting: " + err.Error()
		return false
	}
	return chosen == "close"
}
