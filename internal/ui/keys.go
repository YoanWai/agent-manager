package ui

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/YoanWai/agent-manager/internal/clipboard"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/sysstat"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeForm:
		return m.handleFormKey(msg)
	case modeConfirmDelete:
		return m.handleConfirmKey(msg)
	case modeRename:
		return m.handleRenameKey(msg)
	case modeSettings:
		return m.handleSettingsKey(msg)
	case modeMove:
		return m.handleMoveKey(msg)
	case modeRepoPick:
		return m.handleRepoPickKey(msg)
	case modeGroupForm:
		return m.handleGroupFormKey(msg)
	case modeDiff:
		return m.handleDiffKey(msg)
	case modeHelp:
		m.mode = modeList
		return m, nil
	}

	if m.searching {
		return m.handleSearchKey(msg)
	}
	if m.quick.active {
		return m.handleQuickKey(msg)
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		return m, m.moveCursor(-1)
	case "down", "j":
		return m, m.moveCursor(1)
	case "shift+up":
		return m.reorderSelected(-1)
	case "shift+down":
		return m.reorderSelected(1)
	case "enter":
		if entry, ok := m.selectedRow(); ok && entry.isGroup {
			m.toggleCollapse()
			return m, nil
		}
		return m.attachSelected()
	case "n":
		m.openForm()
	case "g":
		m.openGroupForm()
	case "v":
		return m.reviveSelected()
	case "a":
		return m.archiveSelected()
	case "u":
		return m.restoreSelected()
	case "d":
		m.prepareDelete()
	case " ", "space":
		m.openQuickMode()
	case "F":
		m.toggleCollapseAll()
	case "s":
		m.openSettings()
	case "t":
		m.showArchived = !m.showArchived
		m.requestRefresh()
	case "/":
		m.searching = true
		m.err = ""
	case "r":
		m.openRename()
	case "m":
		m.openMove()
	case "?":
		m.mode = modeHelp
	case "D", "x":
		return m, m.openDiff()
	case "ctrl+r":
		return m, m.openDiff()
	}
	return m, nil
}

// openDiff enters the full-screen review for the selected session,
// loading its diff. The whole review takes over the screen so the
// content scrolls freely instead of sharing the narrow sidebar.
func (m *Model) openDiff() tea.Cmd {
	if m.gitDrv == nil {
		m.err = "git not found in PATH"
		return nil
	}
	sess, ok := m.selected()
	if !ok {
		m.err = "select a session to diff"
		return nil
	}
	if m.diff.scrollByFile == nil {
		m.diff.scrollByFile = map[string]int{}
		m.diff.reviewed = map[string]map[string]bool{}
		m.diff.annotations = map[string][]annotation{}
		m.diff.hl = newHLCache()
		m.diff.sideBySide = m.defaultSplitLayout()
	}
	m.diff.active = true
	m.mode = modeDiff
	m.err = ""
	// Default to returning to the list; the in-session Ctrl+R path sets this
	// afterward when review should return to the session instead.
	m.diff.reattachID = ""
	return m.retargetDiff(sess)
}

// moveCursor shifts the selection and kicks off an immediate preview
// fetch for the newly selected session, so the sidebar follows the
// cursor without waiting for the next poll tick.
func (m *Model) moveCursor(delta int) tea.Cmd {
	if len(m.rows) == 0 {
		return nil
	}
	previous := m.cursor
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor >= len(m.rows) {
		m.cursor = 0
	}
	if m.cursor == previous {
		return nil
	}
	sess, ok := m.selected()
	if !ok {
		m.preview = ""
		m.proc = sysstat.ProcStat{}
		m.procFor = ""
		return nil
	}
	m.preview = ""
	return m.previewCmd(sess)
}

// reorderSelected moves the selected session among its group siblings,
// or the selected group among the groups sharing its parent.
func (m *Model) reorderSelected(delta int) (tea.Model, tea.Cmd) {
	entry, ok := m.selectedRow()
	if !ok {
		return m, nil
	}
	var moved bool
	var err error
	if entry.isGroup {
		moved, err = m.store.ReorderGroup(entry.group, delta)
	} else {
		moved, err = m.store.ReorderSession(entry.sess.ID, delta, m.showArchived)
	}
	if err != nil {
		m.err = err.Error()
		return m, nil
	}
	if !moved {
		edge := "top"
		if delta > 0 {
			edge = "bottom"
		}
		what := "group"
		if !entry.isGroup {
			what = "session"
		}
		m.err = fmt.Sprintf("%s already at the %s of its level", what, edge)
		return m, nil
	}
	// Mirror the swap in memory so the list redraws instantly; the next
	// poll re-reads the authoritative order from the store.
	if entry.isGroup {
		m.swapGroupLocal(entry.group, delta)
	} else {
		m.swapSessionLocal(entry.sess.ID, delta)
	}
	m.rebuildRows()
	m.requestRefresh()
	return m, nil
}

func (m *Model) swapSessionLocal(id string, delta int) {
	step := 1
	if delta < 0 {
		step = -1
	}
	for i, sess := range m.sessions {
		if sess.ID != id {
			continue
		}
		neighbor := i + step
		if neighbor >= 0 && neighbor < len(m.sessions) && m.sessions[neighbor].Group == sess.Group {
			m.sessions[i], m.sessions[neighbor] = m.sessions[neighbor], m.sessions[i]
		}
		return
	}
}

func (m *Model) swapGroupLocal(path string, delta int) {
	parent := parentGroup(path)
	isSibling := func(name string) bool {
		return parentGroup(name) == parent
	}
	step := 1
	if delta < 0 {
		step = -1
	}
	current := -1
	for i, name := range m.groups {
		if name == path {
			current = i
			break
		}
	}
	if current < 0 {
		return
	}
	for i := current + step; i >= 0 && i < len(m.groups); i += step {
		if isSibling(m.groups[i]) {
			m.groups[current], m.groups[i] = m.groups[i], m.groups[current]
			return
		}
	}
}

func (m *Model) toggleCollapse() {
	entry, ok := m.selectedRow()
	if !ok {
		return
	}
	path := entry.group
	if !entry.isGroup {
		path = entry.sess.Group
	}
	if path == "" {
		return
	}
	m.collapsed[path] = !m.collapsed[path]
	m.persistCollapsed()
	m.rebuildRows()
}

// toggleCollapseAll folds every group when any is open, and unfolds all
// when they are already collapsed, so one key flips the whole tree.
func (m *Model) toggleCollapseAll() {
	groups := groupClosure(m.groups, m.sessions)
	anyOpen := false
	for group := range groups {
		if !m.collapsed[group] {
			anyOpen = true
			break
		}
	}
	for group := range groups {
		m.collapsed[group] = anyOpen
	}
	m.persistCollapsed()
	m.rebuildRows()
}

func (m *Model) attachSelected() (tea.Model, tea.Cmd) {
	sess, ok := m.selected()
	if !ok {
		return m, nil
	}
	if !m.tmux.Exists(sess.ID) {
		m.err = "session is dead - press v to revive"
		return m, nil
	}
	m.err = ""
	if err := m.acknowledgeFinished(sess); err != nil {
		m.err = err.Error()
		return m, nil
	}
	return m, m.attachCmd(sess.ID)
}

// acknowledgeFinished marks a finished session idle and acked so entering it
// clears the alert while the pane still shows the acknowledged turn.
func (m *Model) acknowledgeFinished(sess store.Session) error {
	if sess.Status != status.Finished {
		return nil
	}
	if err := m.store.UpdateStatus(sess.ID, status.Idle); err != nil {
		return err
	}
	return m.store.SetAcked(sess.ID, true)
}

func (m *Model) attachCmd(id string) tea.Cmd {
	// Flip the window back to auto-sizing so it fills the terminal on attach;
	// attachDoneMsg re-pins it to the preview width on detach.
	if err := m.tmux.PrepareAttach(id); err != nil {
		m.err = err.Error()
		return nil
	}
	return tea.ExecProcess(m.tmux.AttachCommand(id), func(err error) tea.Msg {
		return attachDoneMsg{sessID: id, err: err}
	})
}

func (m *Model) reattach(id string) tea.Cmd {
	if !m.tmux.Exists(id) {
		m.err = "session is dead - press v to revive"
		return nil
	}
	m.err = ""
	sess, err := m.store.Get(id)
	if err != nil {
		m.err = err.Error()
		return nil
	}
	if err := m.acknowledgeFinished(sess); err != nil {
		m.err = err.Error()
		return nil
	}
	return m.attachCmd(id)
}

// reviveSelected relaunches a dead session's tmux session under the same
// id, keeping its name, group, and history. Tools with a revive_command
// resume where they left off (e.g. claude --continue).
func (m *Model) reviveSelected() (tea.Model, tea.Cmd) {
	sess, ok := m.selected()
	if !ok {
		return m, nil
	}
	if m.tmux.Exists(sess.ID) {
		m.err = "session is still running; revive only applies to dead sessions"
		return m, nil
	}
	tool, ok := m.cfg.Tools[sess.Tool]
	if !ok {
		m.err = "tool " + sess.Tool + " is no longer configured"
		return m, nil
	}
	if info, err := os.Stat(sess.Cwd); err != nil || !info.IsDir() {
		m.err = "working directory no longer exists: " + sess.Cwd
		return m, nil
	}
	baseCommand := tool.ReviveCommand
	if baseCommand == "" {
		baseCommand = tool.Command
	}
	// When the session's own conversation id is known, resume that exact
	// conversation instead of the working directory's most recent one,
	// which would be the wrong conversation whenever sessions share a cwd.
	if sess.AgentSessionID != "" && tool.ResumeByIDCommand != "" {
		baseCommand = strings.ReplaceAll(tool.ResumeByIDCommand, "{id}", sess.AgentSessionID)
	}
	command, env, err := m.buildLaunch(tool, baseCommand, sess.ID)
	if err != nil {
		m.err = err.Error()
		return m, nil
	}
	if err := m.tmux.Create(sess.ID, sess.Cwd, command, env, m.previewPaneWidth(), m.height); err != nil {
		m.err = err.Error()
		return m, nil
	}
	if err := m.tmux.SetLabel(sess.ID, sessionLabel(sess.Group, sess.Name)); err != nil {
		m.err = err.Error()
		return m, nil
	}
	if err := m.store.UpdateStatus(sess.ID, tool.DefaultStatus); err != nil {
		m.err = err.Error()
		return m, nil
	}
	// A leftover ack from the previous life must not swallow the revived
	// agent's first finished alert.
	if err := m.store.SetAcked(sess.ID, false); err != nil {
		m.err = err.Error()
		return m, nil
	}
	m.err = ""
	m.requestRefresh()
	return m, nil
}

func (m *Model) archiveSelected() (tea.Model, tea.Cmd) {
	return m.setSelectedArchived(true)
}

func (m *Model) restoreSelected() (tea.Model, tea.Cmd) {
	return m.setSelectedArchived(false)
}

// setSelectedArchived archives or restores the selected row: a group row
// takes its whole subtree, a session row takes just that session.
func (m *Model) setSelectedArchived(archived bool) (tea.Model, tea.Cmd) {
	entry, ok := m.selectedRow()
	if !ok {
		return m, nil
	}
	if archived {
		if err := m.snapshotForArchive(entry); err != nil {
			m.err = err.Error()
			return m, nil
		}
	}
	var err error
	if entry.isGroup {
		err = m.store.SetGroupArchived(entry.group, archived)
	} else {
		err = m.store.SetArchived(entry.sess.ID, archived)
	}
	if err != nil {
		m.err = err.Error()
		return m, nil
	}
	m.err = ""
	m.requestRefresh()
	return m, nil
}

// snapshotForArchive freezes each still-live pane as the session's stored
// snapshot, so the archived preview survives the tmux window going away.
func (m *Model) snapshotForArchive(entry treeRow) error {
	sessions := []store.Session{entry.sess}
	if entry.isGroup {
		var err error
		sessions, err = m.store.SessionsInSubtree(entry.group)
		if err != nil {
			return err
		}
	}
	for _, sess := range sessions {
		if !m.tmux.Exists(sess.ID) {
			continue
		}
		pane, err := m.tmux.CapturePane(sess.ID)
		if err != nil || pane == "" {
			continue
		}
		if err := m.store.SetSnapshot(sess.ID, pane); err != nil {
			return err
		}
	}
	return nil
}

func (m *Model) prepareDelete() {
	entry, ok := m.selectedRow()
	if !ok {
		return
	}
	if !entry.isGroup {
		m.confirm = confirmTarget{
			label:    "delete " + entry.sess.Name + "? kills its tmux session.",
			sessions: []store.Session{entry.sess},
		}
		m.mode = modeConfirmDelete
		return
	}
	subtree, err := m.store.SessionsInSubtree(entry.group)
	if err != nil {
		m.err = err.Error()
		return
	}
	subgroups := 0
	for _, g := range m.groups {
		if strings.HasPrefix(g, entry.group+"/") {
			subgroups++
		}
	}
	label := fmt.Sprintf("delete group %s (%d subgroups, %d sessions incl. archived)? kills their tmux sessions.",
		entry.group, subgroups, len(subtree))
	m.confirm = confirmTarget{isGroup: true, path: entry.group, label: label, sessions: subtree}
	m.mode = modeConfirmDelete
}

func (m *Model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	defer func() { m.mode = modeList }()
	switch msg.String() {
	case "y", "enter":
		for _, sess := range m.confirm.sessions {
			if err := m.tmux.Kill(sess.ID); err != nil {
				m.err = err.Error()
				return m, nil
			}
			if err := m.hooks.Remove(sess.ID); err != nil {
				m.err = err.Error()
				return m, nil
			}
			if err := m.hooks.RemoveName(sess.ID); err != nil {
				m.err = err.Error()
				return m, nil
			}
			if err := m.hooks.RemoveReviewRepo(sess.ID); err != nil {
				m.err = err.Error()
				return m, nil
			}
			delete(m.pickedRepos, sess.ID)
			if err := m.store.Delete(sess.ID); err != nil {
				m.err = err.Error()
				return m, nil
			}
		}
		if m.confirm.isGroup {
			if err := m.store.DeleteGroup(m.confirm.path); err != nil {
				m.err = err.Error()
				return m, nil
			}
			for path := range m.collapsed {
				if path == m.confirm.path || strings.HasPrefix(path, m.confirm.path+"/") {
					delete(m.collapsed, path)
				}
			}
			m.persistCollapsed()
		}
		m.confirm = confirmTarget{}
		m.requestRefresh()
		return m, nil
	}
	m.confirm = confirmTarget{}
	return m, nil
}

func (m *Model) openRename() {
	entry, ok := m.selectedRow()
	if !ok {
		return
	}
	input := textinput.New()
	input.CharLimit = 60
	input.Prompt = ""
	input.Focus()
	if entry.isGroup {
		input.SetValue(baseName(entry.group))
		dir := textField("default working directory", 400)
		dir.Prompt = ""
		dirValue := m.groupPaths[entry.group]
		if dirValue == "" {
			dirValue = m.groupDefaultDir(entry.group)
		}
		dir.SetValue(dirValue)
		m.pathSugg.reset()
		m.rename = renameTarget{isGroup: true, path: entry.group, input: input, dir: dir}
	} else {
		input.SetValue(entry.sess.Name)
		m.rename = renameTarget{sessID: entry.sess.ID, input: input}
	}
	m.mode = modeRename
	m.err = ""
}

func (m *Model) renameFocus(delta int) {
	m.pathSugg.reset()
	m.rename.focus = (m.rename.focus + delta + 2) % 2
	m.rename.input.Blur()
	m.rename.dir.Blur()
	if m.rename.focus == 0 {
		m.rename.input.Focus()
	} else {
		m.rename.dir.Focus()
	}
}

func (m *Model) handleRenameKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	pathSuggesting := m.rename.isGroup && m.rename.focus == 1 && m.pathSugg.active()
	switch msg.String() {
	case "esc":
		if pathSuggesting {
			m.pathSugg.reset()
			return m, nil
		}
		m.mode = modeList
		return m, nil
	case "tab", "up", "down":
		if !m.rename.isGroup {
			break
		}
		if pathSuggesting {
			switch msg.String() {
			case "tab":
				m.applyPathSuggestion()
			case "up":
				m.pathSugg.move(-1)
			case "down":
				m.pathSugg.move(1)
			}
			return m, nil
		}
		m.renameFocus(1)
		return m, nil
	case "enter":
		if pathSuggesting && m.pathSugg.chosen {
			m.applyPathSuggestion()
			return m, nil
		}
		return m.applyRename()
	}
	var cmd tea.Cmd
	if m.rename.focus == 0 {
		m.rename.input, cmd = m.rename.input.Update(msg)
	} else {
		m.rename.dir, cmd = m.rename.dir.Update(msg)
		m.pathSugg.recompute(m.rename.dir.Value())
	}
	return m, cmd
}

func (m *Model) applyRename() (tea.Model, tea.Cmd) {
	name := strings.TrimSpace(m.rename.input.Value())
	name = strings.ReplaceAll(name, "/", "-")
	if name == "" {
		m.err = "name cannot be empty"
		return m, nil
	}
	if m.rename.isGroup {
		parent := parentGroup(m.rename.path)
		dir, ok := resolveExistingDir(m.rename.dir.Value(), m.groupDefaultDir(parent))
		if !ok {
			m.err = "default path does not exist: " + dir
			return m, nil
		}
		newPath := name
		if parent != "" {
			newPath = parent + "/" + name
		}
		if err := m.store.RenameGroup(m.rename.path, newPath); err != nil {
			m.err = err.Error()
			return m, nil
		}
		// CreateGroup upserts, so it doubles as the default-path setter.
		if err := m.store.CreateGroup(newPath, dir); err != nil {
			m.err = err.Error()
			return m, nil
		}
		m.renameGroupLocally(m.rename.path, newPath, dir)
		m.relabelSubtree(newPath)
	} else {
		if err := m.store.RenameSession(m.rename.sessID, name); err != nil {
			m.err = err.Error()
			return m, nil
		}
		for i := range m.sessions {
			if m.sessions[i].ID == m.rename.sessID {
				m.sessions[i].Name = name
			}
		}
		m.relabelSession(m.rename.sessID)
	}
	m.rebuildRows()
	m.mode = modeList
	m.requestRefresh()
	return m, nil
}

// renameGroupLocally rewrites the in-memory tree right away, so the
// frames between saving and the poller's next refresh already show the
// new name and path instead of flashing the stale ones.
func (m *Model) renameGroupLocally(old, newPath, dir string) {
	moved := func(group string) (string, bool) {
		if group == old || strings.HasPrefix(group, old+"/") {
			return newPath + group[len(old):], true
		}
		return group, false
	}
	for i := range m.groups {
		m.groups[i], _ = moved(m.groups[i])
	}
	for i := range m.sessions {
		m.sessions[i].Group, _ = moved(m.sessions[i].Group)
	}
	groupPaths := make(map[string]string, len(m.groupPaths))
	for group, path := range m.groupPaths {
		group, _ = moved(group)
		groupPaths[group] = path
	}
	groupPaths[newPath] = dir
	m.groupPaths = groupPaths
	for group, folded := range m.collapsed {
		if renamed, ok := moved(group); ok {
			delete(m.collapsed, group)
			m.collapsed[renamed] = folded
		}
	}
	m.persistCollapsed()
}

// captureClipboardImage is the seam the quick bar uses to read a pasted
// image; tests swap it for a fake.
var captureClipboardImage = clipboard.ReadImage

// attachQuickImage saves a clipboard image to a temp file and records it as
// an attachment shown beside the prompt; its path is appended to the message
// on submit so the agent can open it. It reports whether it handled the
// keypress: an empty clipboard returns false so the caller can fall back to a
// plain text paste, while a real failure is surfaced through m.err.
func (m *Model) attachQuickImage() bool {
	data, ext, err := captureClipboardImage()
	if err != nil {
		if errors.Is(err, clipboard.ErrNoImage) {
			return false
		}
		m.err = err.Error()
		return true
	}
	path, err := clipboard.SaveToTemp(data, ext)
	if err != nil {
		m.err = err.Error()
		return true
	}
	m.quick.attachments = append(m.quick.attachments, path)
	m.err = ""
	return true
}

// quickMessage is the text delivered on submit: the typed prompt with any
// attachment paths appended so the target agent can open them.
func (m *Model) quickMessage() string {
	text := strings.TrimSpace(m.quick.input.Value())
	if len(m.quick.attachments) == 0 {
		return text
	}
	paths := strings.Join(m.quick.attachments, " ")
	if text == "" {
		return paths
	}
	return text + " " + paths
}

func (m *Model) openQuickMode() {
	input := textarea.New()
	input.CharLimit = 2000
	input.Placeholder = "type and press enter"
	input.ShowLineNumbers = false
	input.SetPromptFunc(2, func(lineIndex int) string {
		if lineIndex == 0 {
			return "> "
		}
		return ""
	})
	input.FocusedStyle.CursorLine = lipgloss.NewStyle()
	input.SetHeight(1)
	input.Focus()
	m.err = ""
	names, index := m.defaultToolSelection()
	m.quick = quickState{active: true, input: input, toolNames: names, toolIndex: index}
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
	case "ctrl+v":
		if m.attachQuickImage() {
			return m, nil
		}
		// No image on the clipboard: fall through so the textarea's own
		// ctrl+v text paste still works.
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
	return m, cmd
}

// submitQuick answers the selected session, or spawns a new session with
// the prompt embedded when a group is selected. The bar stays active so
// consecutive prompts flow without re-arming.
func (m *Model) submitQuick() (tea.Model, tea.Cmd) {
	entry, ok := m.selectedRow()
	if !ok {
		m.err = "nothing selected"
		return m, nil
	}
	text := m.quickMessage()
	if text == "" {
		m.err = "prompt cannot be empty"
		return m, nil
	}
	if entry.isGroup {
		return m.quickSpawn(entry.group, text)
	}
	if !m.tmux.Exists(entry.sess.ID) {
		m.err = "session is dead - press v to revive"
		return m, nil
	}
	if err := m.tmux.SendText(entry.sess.ID, text); err != nil {
		m.err = err.Error()
		return m, nil
	}
	// The prompt is delivered: clear the input before anything else can
	// fail, so a retry cannot send it twice.
	m.quick.input.SetValue("")
	m.quick.attachments = nil
	m.err = ""
	// A queued answer means the user expects a fresh finished alert.
	if err := m.store.SetAcked(entry.sess.ID, false); err != nil {
		m.err = "prompt sent, but clearing the alert ack failed: " + err.Error()
	}
	m.requestRefresh()
	return m, nil
}

func (m *Model) quickSpawn(group, prompt string) (tea.Model, tea.Cmd) {
	if strings.HasPrefix(prompt, "-") {
		m.err = `prompt cannot start with "-": the tool would read it as a flag`
		return m, nil
	}
	toolName := m.quickTool()
	if toolName == "" {
		m.err = "no tools configured"
		return m, nil
	}
	dir, ok := resolveExistingDir(m.groupPaths[group], m.groupDefaultDir(group))
	if !ok {
		m.err = "group has no valid default path: " + dir
		return m, nil
	}
	name := toolName + "-" + newID()[:4]
	if err := m.spawnSession(toolName, name, dir, group, prompt, true); err != nil {
		m.err = err.Error()
		return m, nil
	}
	m.quick.input.SetValue("")
	m.quick.attachments = nil
	m.err = ""
	return m, m.refreshCmd()
}

// quickTool is the spawn CLI for the current quick-mode run: the settings
// default until tab cycles it.
func (m *Model) quickTool() string {
	if len(m.quick.toolNames) == 0 {
		return ""
	}
	return m.quick.toolNames[m.quick.toolIndex]
}

// defaultTool is the CLI quick spawn launches: the settings choice when it
// still exists in the config, else the first tool alphabetically. A store
// error still yields the fallback but is surfaced, never swallowed.
func (m *Model) defaultTool() string {
	names := sortedToolNames(m.cfg)
	if len(names) == 0 {
		return ""
	}
	chosen, err := m.store.Setting("default_tool")
	if err != nil {
		m.err = "reading default tool setting: " + err.Error()
		return names[0]
	}
	if chosen != "" {
		if _, ok := m.cfg.Tools[chosen]; ok {
			return chosen
		}
	}
	return names[0]
}

const diffLayoutSetting = "diff_layout"

// defaultSplitLayout reports whether review mode should open in split
// (side-by-side) layout. Split is the default; a stored "unified" choice
// opts out. A store error is surfaced but still yields the split default.
func (m *Model) defaultSplitLayout() bool {
	chosen, err := m.store.Setting(diffLayoutSetting)
	if err != nil {
		m.err = "reading diff layout setting: " + err.Error()
		return true
	}
	return chosen != "unified"
}

func (m *Model) openSettings() {
	if len(m.cfg.Tools) == 0 {
		m.err = "no tools configured"
		return
	}
	m.err = ""
	names, index := m.defaultToolSelection()
	m.settings = settingsState{
		toolNames:   names,
		toolIndex:   index,
		layoutSplit: m.defaultSplitLayout(),
	}
	m.mode = modeSettings
}

func (m *Model) handleSettingsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k", "down", "j":
		m.settings.field = (m.settings.field + 1) % settingsFieldCount
	case "left", "h", "right", "l":
		if m.settings.field == settingsFieldTool {
			m.settings.toolIndex = (m.settings.toolIndex + 1) % len(m.settings.toolNames)
		} else {
			m.settings.layoutSplit = !m.settings.layoutSplit
		}
	case "enter", "esc":
		if err := m.store.SetSetting("default_tool", m.settings.toolNames[m.settings.toolIndex]); err != nil {
			m.err = err.Error()
		}
		layout := "split"
		if !m.settings.layoutSplit {
			layout = "unified"
		}
		if err := m.store.SetSetting(diffLayoutSetting, layout); err != nil {
			m.err = err.Error()
		}
		m.mode = modeList
	}
	return m, nil
}

func (m *Model) openMove() {
	sess, ok := m.selected()
	if !ok {
		return
	}
	m.moveID = sess.ID
	m.rebuildGroupOptions(sess.Group)
	m.mode = modeMove
	m.err = ""
}

func (m *Model) handleMoveKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeList
		return m, nil
	case "up":
		m.moveGroupCursor(-1)
		return m, nil
	case "down":
		m.moveGroupCursor(1)
		return m, nil
	case "enter":
		group := m.selectedGroupPath()
		if err := m.store.MoveSession(m.moveID, group); err != nil {
			m.err = err.Error()
			return m, nil
		}
		m.relabelSession(m.moveID)
		m.mode = modeList
		m.requestRefresh()
		return m, nil
	}
	return m, nil
}

// relabelSession refreshes one session's tmux status-bar label from the db.
func (m *Model) relabelSession(id string) {
	sess, err := m.store.Get(id)
	if err != nil {
		m.err = err.Error()
		return
	}
	if !m.tmux.Exists(id) {
		return
	}
	if err := m.tmux.SetLabel(id, sessionLabel(sess.Group, sess.Name)); err != nil {
		m.err = err.Error()
	}
}

// relabelSubtree refreshes labels for every session under a group path.
func (m *Model) relabelSubtree(path string) {
	sessions, err := m.store.SessionsInSubtree(path)
	if err != nil {
		m.err = err.Error()
		return
	}
	for _, sess := range sessions {
		m.relabelSession(sess.ID)
	}
}

func (m *Model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter", "esc":
		m.searching = false
	case "backspace":
		if len(m.search) > 0 {
			m.search = m.search[:len(m.search)-1]
		}
		m.rebuildRows()
	default:
		if len(msg.String()) == 1 {
			m.search += msg.String()
			m.rebuildRows()
		}
	}
	return m, nil
}
