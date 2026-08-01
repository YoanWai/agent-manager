package ui

import (
	"errors"
	"os"
	"strconv"
	"strings"

	"github.com/YoanWai/agent-manager/internal/clipboard"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// attachQuickImageCmd reads the clipboard image off the UI thread and
// writes it once into the pastes directory. Returning a Cmd keeps the TUI
// responsive and shows a pasting chip while the OS clipboard is read.
func (m *Model) attachQuickImageCmd(id int) tea.Cmd {
	return func() tea.Msg {
		path, err := captureClipboardImage()
		if err != nil {
			if errors.Is(err, clipboard.ErrNoImage) {
				return quickImageMsg{id: id, noImage: true}
			}
			return quickImageMsg{id: id, err: err}
		}
		return quickImageMsg{id: id, path: path}
	}
}

// handleQuickImageMsg applies an async clipboard result to the chip the
// paste reserved: the path fills it in, a real error surfaces and takes
// the chip back out, and no-image falls through to a text paste.
func (m *Model) handleQuickImageMsg(msg quickImageMsg) (tea.Model, tea.Cmd) {
	att := m.quickAttachment(msg.id)
	if att == nil || !m.quick.active {
		if msg.path != "" {
			_ = os.Remove(msg.path)
		}
		if att != nil {
			m.dropQuickAttachment(msg.id)
		}
		return m, nil
	}
	if msg.err != nil {
		cmd := m.removeQuickImage(msg.id)
		m.errBar.text = msg.err.Error()
		return m, cmd
	}
	if msg.noImage {
		cmd := m.removeQuickImage(msg.id)
		m.quick.input.SetHeight(quickBarMaxRows)
		var pasteCmd tea.Cmd
		m.quick.input, pasteCmd = m.quick.input.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
		return m, tea.Batch(cmd, pasteCmd)
	}
	att.path = msg.path
	m.errBar.text = ""
	return m, nil
}

// removeQuickImage takes a chip out of the text by id, wherever it sits.
func (m *Model) removeQuickImage(id int) tea.Cmd {
	for _, span := range m.quickTokenSpans() {
		if span.id == id {
			return m.removeQuickToken(span)
		}
	}
	m.dropQuickAttachment(id)
	return nil
}

// quickMessage is the text delivered on submit: the typed prompt with each
// chip swapped back for its path, so the paths reach the agent in the
// order and the places the user pasted them.
func (m *Model) quickMessage() string {
	value := imageTokenPattern.ReplaceAllStringFunc(m.quick.input.Value(), func(token string) string {
		id, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(token, "[Image #"), "]"))
		if err != nil {
			return token
		}
		att := m.quickAttachment(id)
		if att == nil || att.path == "" {
			return token
		}
		return att.path
	})
	return strings.TrimSpace(value)
}

func (m *Model) openQuickMode() {
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
	names, index := m.defaultToolSelection()
	m.quick = quickState{
		active:         true,
		input:          input,
		toolNames:      names,
		toolIndex:      index,
		closeAfterSend: m.quickCloseAfterSend(),
		worktree:       m.defaultWorktree(),
	}
}

// defaultToolSelection returns the sorted tool names with the index of
// the configured default, ready to seed a tool picker.
func (m *Model) defaultToolSelection() ([]string, int) {
	names := sortedToolNames(m.cfg)
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
		m.releaseQuickAttachments()
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
		m.quick.worktree = !m.quickWorktreeOn()
		m.quick.worktreeTouched = true
		return m, nil
	case "ctrl+v":
		if m.quickPasting() {
			return m, nil
		}
		if !m.quickRoomForToken(m.quick.lastImageID + 1) {
			m.errBar.text = "prompt is full - shorten it before pasting an image"
			return m, nil
		}
		// The chip goes in at the caret now and fills in when the
		// off-thread clipboard read lands, so it holds the spot the user
		// pasted at even while they keep typing.
		m.quick.lastImageID++
		id := m.quick.lastImageID
		m.quick.attachments = append(m.quick.attachments, quickAttachment{id: id})
		m.insertQuickToken(&m.quick.attachments[len(m.quick.attachments)-1])
		m.errBar.text = ""
		return m, m.attachQuickImageCmd(id)
	case "left":
		if span, ok := m.tokenEndingAt(m.quickCursorOffset()); ok {
			m.quick.input.SetCursor(m.quickCursorColumn() - span.length())
			return m, nil
		}
	case "right":
		if span, ok := m.tokenStartingAt(m.quickCursorOffset()); ok {
			m.quick.input.SetCursor(m.quickCursorColumn() + span.length())
			return m, nil
		}
	case "backspace", "ctrl+h":
		if span, ok := m.tokenEndingAt(m.quickCursorOffset()); ok {
			return m, m.removeQuickToken(span)
		}
	case "delete":
		if span, ok := m.tokenStartingAt(m.quickCursorOffset()); ok {
			return m, m.removeQuickToken(span)
		}
	case "enter":
		return m.submitQuick()
	}
	// Update repositions its viewport against the height set at the last
	// render; a keystroke that adds a wrapped row would scroll that first
	// row away for good. Full cap height here keeps the viewport pinned,
	// and the next render shrinks the bar back to the rows the text needs.
	m.quick.input.SetHeight(quickBarMaxRows)
	var cmd tea.Cmd
	m.quick.input, cmd = m.quick.input.Update(msg)
	// An edit that swallowed a chip (ctrl+u, word delete) releases its
	// image, and the caret never rests inside a chip.
	m.pruneQuickAttachments()
	m.snapQuickCursorOutOfToken()
	return m, cmd
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
	if m.quickPasting() {
		m.errBar.text = "still reading the pasted image - try again in a moment"
		return m, nil
	}
	text := m.quickMessage()
	if text == "" {
		m.errBar.text = "prompt cannot be empty"
		return m, nil
	}
	if entry.isGroup {
		return m.quickSpawn(entry.group, text)
	}
	if !m.tmux.Exists(entry.sess.ID) {
		m.errBar.text = "session is dead - press v to revive"
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
	worktree := m.quick.worktree
	if !m.quick.worktreeTouched {
		worktree = m.groupWorktree(group)
	}
	name := toolName + "-" + newID()[:4]
	if err := m.spawnSession(toolName, name, dir, group, prompt, true, worktree); err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
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
// with: the target group's default until shift+tab overrides it.
func (m *Model) quickWorktreeOn() bool {
	if m.quick.worktreeTouched {
		return m.quick.worktree
	}
	group := ""
	if entry, ok := m.selectedRow(); ok {
		if entry.isGroup {
			group = entry.group
		} else {
			group = entry.sess.Group
		}
	}
	return m.groupWorktree(group)
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
