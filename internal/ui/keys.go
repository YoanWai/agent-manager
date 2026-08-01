package ui

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/YoanWai/agent-manager/internal/clipboard"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/sysstat"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Resize mode owns the keyboard until the drag commits or the user
	// cancels: other bindings would fight the mouse-gated session.
	if m.resizeMode {
		switch msg.String() {
		case "left", "h":
			m.nudgeSplit(-1)
			return m, nil
		case "right", "l":
			m.nudgeSplit(1)
			return m, nil
		case "|", "enter":
			// Enter or a second | commits the working ratio.
			return m.exitResizeMode(true)
		case "esc":
			return m.exitResizeMode(false)
		case "q", "ctrl+c":
			m.persistSplitRatio()
			m.resizeMode = false
			m.splitDragging = false
			return m, tea.Quit
		default:
			return m, nil
		}
	}

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
	case modeFocus:
		return m.handleFocusKey(msg)
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
	case "shift+up", "K", "shift+k":
		return m.reorderSelected(-1)
	case "shift+down", "J", "shift+j":
		return m.reorderSelected(1)
	case "enter":
		if entry, ok := m.selectedRow(); ok && entry.isGroup {
			m.toggleCollapse()
			return m, nil
		}
		if m.enterFocuses() {
			return m.focusSelected()
		}
		return m.attachSelected()
	case "A", "shift+a":
		if m.enterFocuses() {
			return m.attachSelected()
		}
		return m.focusSelected()
	case "n":
		m.openForm()
	case "g":
		m.openGroupForm()
	case "v":
		return m.reviveSelected()
	case "V", "shift+v":
		return m.reviveAllDead()
	case "x":
		return m.killSelected()
	case "X", "shift+x":
		return m.killAllLive()
	case "a":
		return m.archiveSelected()
	case "u":
		return m.restoreSelected()
	case "d":
		m.prepareDelete()
	case " ", "space":
		m.openQuickMode()
	case "F", "shift+f":
		m.toggleCollapseAll()
	case "s":
		m.openSettings()
	case "|":
		return m.enterResizeMode()
	case "t":
		m.showArchived = !m.showArchived
		m.requestRefresh()
	case "e":
		return m, m.toggleEmptyGroups()
	case "/":
		m.searching = true
		m.err = ""
	case "r":
		m.openRename()
	case "m":
		m.openMove()
	case "?":
		m.mode = modeHelp
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
		m.diff.reviewed = map[string]map[string]uint64{}
		m.diff.annotations = map[string][]annotation{}
		m.diff.sideBySide = m.defaultSplitLayout()
	}
	if m.diff.hl == nil {
		m.diff.hl = newHLCache()
	}
	m.diff.active = true
	m.mode = modeDiff
	m.err = ""
	// Default to returning to the list; the in-session Ctrl+R path sets this
	// afterward when review should return to the session instead.
	m.diff.reattachID = ""
	m.applyStoredScope(sess.ID)
	return m.retargetDiff(sess)
}

// moveCursor shifts the selection and schedules a debounced preview
// fetch. Key-repeat only bumps the gen; a single capture runs after the
// cursor settles so holding j/k cannot pile up tmux work.
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
	m.preview = ""
	m.proc = sysstat.ProcStat{}
	m.procFor = ""
	if _, ok := m.selected(); !ok {
		return nil
	}
	m.previewGen++
	return m.schedulePreview()
}

// reorderSelected moves the selected session among its group siblings,
// or the selected group among the groups sharing its parent.
func (m *Model) reorderSelected(delta int) (tea.Model, tea.Cmd) {
	entry, ok := m.selectedRow()
	if !ok {
		return m, nil
	}
	if entry.isRoot() {
		m.err = "root stays at the top of the list"
		return m, nil
	}
	target, ok := m.visibleReorderTarget(entry, delta)
	if !ok {
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

	var err error
	var groupSiblings []string
	if entry.isGroup {
		groupSiblings = m.knownGroupSiblings(parentGroup(entry.group))
		err = m.store.SwapGroupOrder(entry.group, target.group, groupSiblings...)
	} else {
		err = m.store.SwapSessionOrder(entry.sess.ID, target.sess.ID)
	}
	if err != nil {
		m.err = err.Error()
		return m, nil
	}
	// Mirror the swap in memory so the list redraws instantly; the next
	// poll re-reads the authoritative order from the store.
	if entry.isGroup {
		m.materializeGroupsLocal(groupSiblings)
		m.swapGroupLocal(entry.group, target.group)
	} else {
		m.swapSessionLocal(entry.sess.ID, target.sess.ID)
	}
	m.err = ""
	m.rebuildRows()
	m.requestRefresh()
	return m, nil
}

// visibleReorderTarget finds the next rendered sibling. Filters and archive
// scope therefore cannot turn a successful reorder into an invisible swap.
func (m *Model) visibleReorderTarget(entry treeRow, delta int) (treeRow, bool) {
	step := 1
	if delta < 0 {
		step = -1
	}
	for i := m.cursor + step; i >= 0 && i < len(m.rows); i += step {
		candidate := m.rows[i]
		if candidate.isRoot() {
			// parentGroup("") is "" too, so root would match a top-level
			// group as its own sibling.
			continue
		}
		if entry.isGroup {
			if candidate.isGroup && parentGroup(candidate.group) == parentGroup(entry.group) {
				return candidate, true
			}
			continue
		}
		if !candidate.isGroup && candidate.sess.Group == entry.sess.Group {
			return candidate, true
		}
	}
	return treeRow{}, false
}

func (m *Model) knownGroupSiblings(parent string) []string {
	paths := groupClosure(m.groups, m.sessions)
	return childIndex(paths, m.groups)[parent]
}

func (m *Model) materializeGroupsLocal(paths []string) {
	known := make(map[string]bool, len(m.groups))
	for _, group := range m.groups {
		known[group] = true
	}
	for _, path := range paths {
		if !known[path] {
			m.groups = append(m.groups, path)
			known[path] = true
		}
	}
}

func (m *Model) swapSessionLocal(id, targetID string) {
	current, target := -1, -1
	for i, sess := range m.sessions {
		switch sess.ID {
		case id:
			current = i
		case targetID:
			target = i
		}
	}
	if current >= 0 && target >= 0 {
		m.sessions[current], m.sessions[target] = m.sessions[target], m.sessions[current]
	}
}

func (m *Model) swapGroupLocal(path, targetPath string) {
	current, target := -1, -1
	for i, name := range m.groups {
		switch name {
		case path:
			current = i
		case targetPath:
			target = i
		}
	}
	if current >= 0 && target >= 0 {
		m.groups[current], m.groups[target] = m.groups[target], m.groups[current]
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

// focusSelected enters focus mode: keys go to the selected session's pane
// while the manager, its rail and its live preview stay on screen.
func (m *Model) focusSelected() (tea.Model, tea.Cmd) {
	sess, ok := m.selected()
	if !ok {
		return m, nil
	}
	if sess.Archived {
		return m.attachSelected()
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
	m.mode = modeFocus
	// Focusing is deliberate, so the client opens now rather than waiting
	// for the cursor to settle, and any failure backoff is lifted.
	if m.focus != nil {
		m.focus.retryNow()
	}
	m.watchSelection()
	m.sel = focusSelection{}
	m.copied = 0
	m.cursorOn = true
	m.focusScroll = 0
	// Pane state from a previously focused session must not route this
	// one's wheel; the first pushed capture reports the real values.
	m.paneMouse = false
	m.paneMotion = false
	m.paneSGR = false
	m.paneHistory = 0
	// Mouse reporting makes the pane a closed window: clicks land here
	// instead of the host terminal, so a drag selects pane text alone and
	// never the rail beside it.
	return m, tea.Batch(tea.EnableMouseCellMotion, m.cursorBlink())
}

// leaveFocus returns to the list and gives the mouse back to the terminal,
// restoring native selection and wheel-as-arrows.
func (m *Model) leaveFocus() tea.Cmd {
	m.mode = modeList
	m.sel = focusSelection{}
	m.copied = 0
	return tea.Sequence(tea.DisableMouse, func() tea.Msg {
		_ = EnableAlternateScroll()
		return nil
	})
}

// handleFocusKey forwards every key into the focused pane. Ctrl+Q is the
// one reserved key: it returns to the list, mirroring detach from a real
// attach, and every plain character - q included - reaches the agent.
func (m *Model) handleFocusKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+q" {
		return m, m.leaveFocus()
	}
	sess, ok := m.selected()
	if !ok {
		return m, m.leaveFocus()
	}
	// Typing puts the cursor back on: a caret that blinks out mid-keystroke
	// reads as a dropped character.
	m.cursorOn = true
	// Keystrokes land at the live bottom, so the view follows them there.
	var resume tea.Cmd
	if m.scrolledBack() {
		m.focusScroll = 0
		resume = m.focusRegionCmd(sess.ID, 0)
	}
	command, ok := focusKeyCommand(tmux.SessionName(sess.ID), msg)
	if !ok {
		return m, resume
	}
	if m.focus == nil || !m.focus.attempt(command) {
		// Nothing went over the pipe; one forked send-keys keeps the key
		// from being swallowed.
		if err := m.tmux.SendRaw(command); err != nil {
			m.err = err.Error()
		}
	}
	return m, resume
}

// toggleEmptyGroups hides or restores group rows whose subtree has no
// sessions in the current active/archive view. It never changes the store.
func (m *Model) toggleEmptyGroups() tea.Cmd {
	previousKey := ""
	if entry, ok := m.selectedRow(); ok {
		previousKey = rowKey(entry)
	}
	m.hideEmptyGroups = !m.hideEmptyGroups
	m.rebuildRows()

	currentKey := ""
	if entry, ok := m.selectedRow(); ok {
		currentKey = rowKey(entry)
	}
	if currentKey == previousKey {
		return nil
	}

	m.preview = ""
	m.proc = sysstat.ProcStat{}
	m.procFor = ""
	m.previewGen++
	m.syncPollInput()
	if _, ok := m.selected(); ok {
		return m.schedulePreview()
	}
	return nil
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
	// attachDoneMsg re-pins it to the preview width on detach. Clearing the
	// cached hash first keeps the poller from reading this reflow as
	// streaming output, same as the detach-side resize (reflowSessions).
	// A failure here still attaches: the worst outcome is a stale window
	// size, which beats locking the session out (issue #114).
	var prepErr error
	m.poller.reflowSessions([]string{id}, func() {
		prepErr = m.tmux.PrepareAttach(id)
	})
	if prepErr != nil {
		m.err = prepErr.Error()
	}
	return tea.ExecProcess(m.tmux.AttachCommand(id), func(err error) tea.Msg {
		return attachDoneMsg{sessID: id, err: err}
	})
}

// warn carries a PrepareAttach failure: shown to the user, but the attach
// still proceeds, unlike err which cancels it.
type reattachPreparedMsg struct {
	sessID  string
	diffGen int
	err     error
	warn    string
}

func (m *Model) reattach(id string, diffGen int) tea.Cmd {
	driver := m.tmux
	stor := m.store
	poller := m.poller
	return func() tea.Msg {
		if !driver.Exists(id) {
			return reattachPreparedMsg{sessID: id, diffGen: diffGen, err: errors.New("session is dead - press v to revive")}
		}
		sess, err := stor.Get(id)
		if err != nil {
			return reattachPreparedMsg{sessID: id, diffGen: diffGen, err: err}
		}
		if sess.Status == status.Finished {
			if err := stor.UpdateStatus(sess.ID, status.Idle); err != nil {
				return reattachPreparedMsg{sessID: id, diffGen: diffGen, err: err}
			}
			if err := stor.SetAcked(sess.ID, true); err != nil {
				return reattachPreparedMsg{sessID: id, diffGen: diffGen, err: err}
			}
		}
		var prepErr error
		poller.reflowSessions([]string{id}, func() {
			prepErr = driver.PrepareAttach(id)
		})
		var warn string
		if prepErr != nil {
			warn = prepErr.Error()
		}
		return reattachPreparedMsg{sessID: id, diffGen: diffGen, warn: warn}
	}
}

// reviveSelected relaunches a dead session's tmux session under the same
// id, keeping its name, group, and history. Tools with a revive_command
// resume where they left off (e.g. claude --continue). On a group row it
// revives the whole subtree, mirroring the group kill.
func (m *Model) reviveSelected() (tea.Model, tea.Cmd) {
	entry, ok := m.selectedRow()
	if !ok {
		return m, nil
	}
	if entry.isGroup {
		return m.reviveMany(m.sessionsInGroup(entry.group), "no dead sessions to revive in "+entry.group)
	}
	if err := m.reviveSession(entry.sess); err != nil {
		m.err = err.Error()
		return m, nil
	}
	m.err = m.degradedResumeNotice(entry.sess)
	m.requestRefresh()
	return m, nil
}

// reviveAllDead relaunches every dead session in the current view, resuming
// each by its captured id where one exists.
func (m *Model) reviveAllDead() (tea.Model, tea.Cmd) {
	return m.reviveMany(m.visibleSessions(), "no dead sessions to revive")
}

// reviveMany relaunches every dead session in the list. It revives what it
// can and names the first failure rather than stopping, so one broken
// session does not block the rest.
func (m *Model) reviveMany(sessions []store.Session, emptyNotice string) (tea.Model, tea.Cmd) {
	revived, degraded := 0, 0
	var firstErr string
	for _, sess := range sessions {
		if sess.Status != status.Dead {
			continue
		}
		if err := m.reviveSession(sess); err != nil {
			if firstErr == "" {
				firstErr = err.Error()
			}
			continue
		}
		revived++
		if m.degradedResumeNotice(sess) != "" {
			degraded++
		}
	}
	switch {
	case revived == 0 && firstErr == "":
		m.err = emptyNotice
	case firstErr != "":
		m.err = fmt.Sprintf("revived %d, first error: %s", revived, firstErr)
	case degraded > 0:
		m.err = fmt.Sprintf("revived %d, %d without a captured id (used --continue)", revived, degraded)
	default:
		m.err = ""
	}
	m.requestRefresh()
	return m, nil
}

// sessionsInGroup lists the sessions the current view shows at or below a
// group, so a group action covers exactly the rows under it on screen.
func (m *Model) sessionsInGroup(path string) []store.Session {
	var sessions []store.Session
	for _, sess := range m.visibleSessions() {
		if inGroupSubtree(sess.Group, path) {
			sessions = append(sessions, sess)
		}
	}
	return sessions
}

// degradedResumeNotice warns when a revived session had to fall back to the
// working directory's most recent conversation because its own conversation
// id was never captured, which resumes the wrong conversation whenever
// sessions share a directory.
func (m *Model) degradedResumeNotice(sess store.Session) string {
	tool, ok := m.cfg.Tools[sess.Tool]
	if !ok || sess.AgentSessionID != "" || tool.ResumeByIDCommand == "" {
		return ""
	}
	return fmt.Sprintf("revived %s with --continue: no conversation id captured, may resume the wrong conversation", sess.Name)
}

// reviveSession relaunches one dead session under its old id, keeping its
// name, group, and history. When the session's own conversation id was
// captured, it resumes that exact conversation via the tool's
// resume_by_id_command instead of the working directory's most recent one,
// which would be the wrong conversation whenever sessions share a cwd.
func (m *Model) reviveSession(sess store.Session) error {
	if m.tmux.Exists(sess.ID) {
		return fmt.Errorf("session %s is still running; revive only applies to dead sessions", sess.Name)
	}
	tool, ok := m.cfg.Tools[sess.Tool]
	if !ok {
		return fmt.Errorf("tool %s is no longer configured", sess.Tool)
	}
	if info, err := os.Stat(sess.Cwd); err != nil || !info.IsDir() {
		return fmt.Errorf("working directory no longer exists: %s", sess.Cwd)
	}
	baseCommand := tool.ReviveCommand
	if baseCommand == "" {
		baseCommand = tool.Command
	}
	if sess.AgentSessionID != "" && tool.ResumeByIDCommand != "" {
		baseCommand = strings.ReplaceAll(tool.ResumeByIDCommand, "{id}", sess.AgentSessionID)
	}
	command, env, err := m.buildLaunch(sess.Tool, tool, baseCommand, sess.ID)
	if err != nil {
		return err
	}
	if err := m.tmux.Create(sess.ID, sess.Cwd, command, env, m.previewPaneWidth(), m.previewPaneHeight()); err != nil {
		return err
	}
	if err := m.tmux.SetLabel(sess.ID, sessionLabel(sess.Group, sess.Name)); err != nil {
		return err
	}
	if err := m.store.UpdateStatus(sess.ID, tool.DefaultStatus); err != nil {
		return err
	}
	// The session is alive again; any watcher backoff from its dead spell
	// no longer applies.
	if m.focus != nil {
		m.focus.retryNow()
	}
	// A leftover ack from the previous life must not swallow the revived
	// agent's first finished alert.
	return m.store.SetAcked(sess.ID, false)
}

// killSelected asks to end the selected session, or every live session
// under the selected group, freeing the RAM their agents hold while the
// rows stay put for v to revive.
func (m *Model) killSelected() (tea.Model, tea.Cmd) {
	entry, ok := m.selectedRow()
	if !ok {
		return m, nil
	}
	if entry.isGroup {
		live, err := m.liveSessions(m.sessionsInGroup(entry.group))
		if err != nil {
			m.err = err.Error()
			return m, nil
		}
		if len(live) == 0 {
			m.err = "no live sessions to kill in " + entry.group
			return m, nil
		}
		m.confirm = confirmTarget{
			isGroup:  true,
			path:     entry.group,
			action:   actionKill,
			sessions: live,
			label: fmt.Sprintf("kill group %s (%d live sessions)? frees their RAM, v revives them.",
				entry.group, len(live)),
		}
	} else {
		if !m.tmux.Exists(entry.sess.ID) {
			m.err = entry.sess.Name + " is already dead"
			return m, nil
		}
		m.confirm = confirmTarget{
			action:   actionKill,
			sessions: []store.Session{entry.sess},
			label:    fmt.Sprintf("kill %s? frees its RAM, v revives it.", entry.sess.Name),
		}
	}
	m.mode = modeConfirmDelete
	return m, nil
}

// killAllLive asks to end every live session in the current view, the
// batch counterpart to V.
func (m *Model) killAllLive() (tea.Model, tea.Cmd) {
	live, err := m.liveSessions(m.visibleSessions())
	if err != nil {
		m.err = err.Error()
		return m, nil
	}
	if len(live) == 0 {
		m.err = "no live sessions to kill"
		return m, nil
	}
	m.confirm = confirmTarget{
		action:   actionKill,
		sessions: live,
		label:    fmt.Sprintf("kill every live session (%d)? frees their RAM, v revives them.", len(live)),
	}
	m.mode = modeConfirmDelete
	return m, nil
}

// liveSessions narrows a list to the sessions that still hold a tmux
// window. One pane listing answers for all of them, so a wide selection
// costs one tmux call rather than one per session.
func (m *Model) liveSessions(sessions []store.Session) ([]store.Session, error) {
	panes, err := m.tmux.Panes()
	if err != nil {
		return nil, err
	}
	var live []store.Session
	for _, sess := range sessions {
		if panes[sess.ID] > 0 {
			live = append(live, sess)
		}
	}
	return live, nil
}

// killSession ends one session's tmux window, freeing everything its agent
// held, while the store row keeps the name, group, history and conversation
// id that revive needs. The pane is captured first so the preview still
// shows the agent's last output once the window is gone.
func (m *Model) killSession(sess store.Session) error {
	if !m.tmux.Exists(sess.ID) {
		return nil
	}
	if pane, err := m.tmux.CapturePane(sess.ID); err == nil && pane != "" {
		if err := m.setSnapshot(sess.ID, pane); err != nil {
			return err
		}
	}
	var killErr error
	// Runs under the poller's lock so no pass can capture a half-killed
	// pane, and drops the pane hash the revived session would be compared
	// against.
	m.poller.reflowSessions([]string{sess.ID}, func() {
		killErr = m.tmux.Kill(sess.ID)
	})
	if killErr != nil {
		return killErr
	}
	// The agent dies without running its session-end hook, so a leftover
	// status file would otherwise decide what the revived session reads as.
	if err := m.hooks.Remove(sess.ID); err != nil {
		return err
	}
	if err := m.store.UpdateStatus(sess.ID, status.Dead); err != nil {
		return err
	}
	for i := range m.sessions {
		if m.sessions[i].ID == sess.ID {
			m.sessions[i].Status = status.Dead
		}
	}
	return nil
}

func (m *Model) archiveSelected() (tea.Model, tea.Cmd) {
	entry, ok := m.selectedRow()
	if !ok {
		return m, nil
	}
	if entry.isGroup {
		subtree, err := m.store.SessionsInSubtree(entry.group)
		if err != nil {
			m.err = err.Error()
			return m, nil
		}
		m.confirm = confirmTarget{
			isGroup:  true,
			path:     entry.group,
			action:   actionArchive,
			sessions: subtree,
			label:    fmt.Sprintf("archive group %s (%d sessions)?", entry.group, len(subtree)),
		}
	} else {
		m.confirm = confirmTarget{
			action:   actionArchive,
			sessions: []store.Session{entry.sess},
			label:    fmt.Sprintf("archive %s?", entry.sess.Name),
		}
	}
	m.mode = modeConfirmDelete
	return m, nil
}

func (m *Model) restoreSelected() (tea.Model, tea.Cmd) {
	entry, ok := m.selectedRow()
	if !ok {
		return m, nil
	}
	if entry.isGroup {
		subtree, err := m.store.SessionsInSubtree(entry.group)
		if err != nil {
			m.err = err.Error()
			return m, nil
		}
		m.confirm = confirmTarget{
			isGroup:  true,
			path:     entry.group,
			action:   actionRestore,
			sessions: subtree,
			label:    fmt.Sprintf("restore group %s (%d sessions)?", entry.group, len(subtree)),
		}
	} else {
		m.confirm = confirmTarget{
			action:   actionRestore,
			sessions: []store.Session{entry.sess},
			label:    fmt.Sprintf("restore %s?", entry.sess.Name),
		}
	}
	m.mode = modeConfirmDelete
	return m, nil
}

// archivalSnapshot captures pane content for every still-live session
// in the confirm target, so the snapshot survives when the tmux window
// is later killed or the session window is reused for a new agent.
func (m *Model) archivalSnapshot() error {
	for _, sess := range m.confirm.sessions {
		if !m.tmux.Exists(sess.ID) {
			continue
		}
		pane, err := m.tmux.CapturePane(sess.ID)
		if err != nil || pane == "" {
			continue
		}
		if err := m.setSnapshot(sess.ID, pane); err != nil {
			return err
		}
	}
	return nil
}

func (m *Model) applyConfirmedArchived(archived bool) error {
	if m.confirm.isGroup {
		return m.store.SetGroupArchived(m.confirm.path, archived)
	}
	return m.store.SetArchived(m.confirm.sessions[0].ID, archived)
}

func (m *Model) prepareDelete() {
	entry, ok := m.selectedRow()
	if !ok {
		return
	}
	if entry.isRoot() {
		m.err = "root is the top level; delete the sessions under it instead"
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
	if m.showArchived {
		m.confirm = archivedGroupDelete(entry.group, subtree)
	} else {
		m.confirm = m.wholeGroupDelete(entry.group, subtree)
	}
	m.mode = modeConfirmDelete
}

// wholeGroupDelete targets the group as the active view shows it: the
// group ceases to exist, so its subtree goes with it, archived sessions
// included, leaving nothing stranded under a group that is gone.
func (m *Model) wholeGroupDelete(path string, subtree []store.Session) confirmTarget {
	subgroups := 0
	for _, g := range m.groups {
		if strings.HasPrefix(g, path+"/") {
			subgroups++
		}
	}
	return confirmTarget{
		isGroup:  true,
		path:     path,
		sessions: subtree,
		label: fmt.Sprintf("delete group %s (%d subgroups, %d sessions incl. archived)? kills their tmux sessions.",
			path, subgroups, len(subtree)),
	}
}

// archivedGroupDelete targets only what the archived view shows: the
// archived sessions under the group. The live sessions and the group
// itself belong to the active view and survive; the group row goes only
// once nothing is left beneath it.
func archivedGroupDelete(path string, subtree []store.Session) confirmTarget {
	var archived []store.Session
	for _, sess := range subtree {
		if sess.Archived {
			archived = append(archived, sess)
		}
	}
	return confirmTarget{
		isGroup:      true,
		archivedOnly: true,
		path:         path,
		sessions:     archived,
		label: fmt.Sprintf("delete %s from the archive (%d archived sessions)? kills their tmux sessions, live ones stay.",
			path, len(archived)),
	}
}

func (m *Model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	defer func() { m.mode = modeList }()
	switch msg.String() {
	case "y", "enter":
		switch m.confirm.action {
		case actionArchive:
			if err := m.archivalSnapshot(); err != nil {
				m.err = err.Error()
				return m, nil
			}
			if err := m.applyConfirmedArchived(true); err != nil {
				m.err = err.Error()
				return m, nil
			}
			m.err = ""
		case actionRestore:
			if err := m.applyConfirmedArchived(false); err != nil {
				m.err = err.Error()
				return m, nil
			}
			m.err = ""
		case actionKill:
			for _, sess := range m.confirm.sessions {
				if err := m.killSession(sess); err != nil {
					m.err = err.Error()
					return m, nil
				}
			}
			m.err = ""
			m.rebuildRows()
		case actionDelete:
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
				if err := m.hooks.RemoveReviewBase(sess.ID); err != nil {
					m.err = err.Error()
					return m, nil
				}
				if err := m.hooks.RemoveReviewScope(sess.ID); err != nil {
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
				removed, err := m.deleteConfirmedGroups()
				if err != nil {
					m.err = err.Error()
					return m, nil
				}
				for _, path := range removed {
					delete(m.collapsed, path)
				}
				m.persistCollapsed()
			}
		default:
			m.err = fmt.Sprintf("unknown confirm action %q", m.confirm.action)
			return m, nil
		}
		m.confirm = confirmTarget{}
		m.requestRefresh()
		return m, nil
	}
	m.confirm = confirmTarget{}
	return m, nil
}

// deleteConfirmedGroups removes the group rows the confirmed delete
// covers, reporting the paths that went so their fold state can go too.
func (m *Model) deleteConfirmedGroups() ([]string, error) {
	if m.confirm.archivedOnly {
		return m.store.PruneArchivedGroups(m.confirm.path)
	}
	return m.store.DeleteGroup(m.confirm.path)
}

func (m *Model) openRename() {
	entry, ok := m.selectedRow()
	if !ok {
		return
	}
	if entry.isRoot() {
		m.err = "root is the top level, not a group to rename"
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
		tools := sortedToolNames(m.cfg)
		toolIndex := 0
		for i, name := range tools {
			if name == entry.sess.Tool {
				toolIndex = i
				break
			}
		}
		// Current tool missing from config (removed block): keep it selectable
		// so save does not silently reassign to the first configured tool.
		if len(tools) == 0 || tools[toolIndex] != entry.sess.Tool {
			tools = append([]string{entry.sess.Tool}, tools...)
			toolIndex = 0
		}
		m.rename = renameTarget{
			sessID:    entry.sess.ID,
			input:     input,
			toolNames: tools,
			toolIndex: toolIndex,
		}
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
	case "tab":
		if !m.rename.isGroup {
			m.cycleRenameTool(1)
			return m, nil
		}
		if pathSuggesting {
			m.applyPathSuggestion()
			return m, nil
		}
		m.renameFocus(1)
		return m, nil
	case "shift+tab":
		if !m.rename.isGroup {
			m.cycleRenameTool(-1)
			return m, nil
		}
		if pathSuggesting {
			return m, nil
		}
		m.renameFocus(-1)
		return m, nil
	case "up", "down":
		if !m.rename.isGroup {
			break
		}
		if pathSuggesting {
			if msg.String() == "up" {
				if !m.pathSugg.move(-1) {
					m.renameFocus(-1)
				}
			} else {
				if !m.pathSugg.move(1) {
					m.renameFocus(1)
				}
			}
			return m, nil
		}
		if msg.String() == "up" {
			m.renameFocus(-1)
		} else {
			m.renameFocus(1)
		}
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

func (m *Model) cycleRenameTool(delta int) {
	if len(m.rename.toolNames) == 0 {
		return
	}
	n := len(m.rename.toolNames)
	m.rename.toolIndex = (m.rename.toolIndex + delta + n) % n
}

func (m *Model) renameTool() string {
	if len(m.rename.toolNames) == 0 {
		return ""
	}
	return m.rename.toolNames[m.rename.toolIndex]
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
		tool := m.renameTool()
		var prevTool string
		for i := range m.sessions {
			if m.sessions[i].ID == m.rename.sessID {
				prevTool = m.sessions[i].Tool
				break
			}
		}
		toolChanged := tool != "" && tool != prevTool
		if toolChanged {
			if err := m.store.UpdateTool(m.rename.sessID, tool); err != nil {
				m.err = err.Error()
				return m, nil
			}
		}
		for i := range m.sessions {
			if m.sessions[i].ID == m.rename.sessID {
				m.sessions[i].Name = name
				if toolChanged {
					m.sessions[i].Tool = tool
					m.sessions[i].AgentSessionID = ""
				}
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

// captureClipboardImage is the seam the quick bar uses to save a pasted
// image to a temp file; tests swap it for a fake.
var captureClipboardImage = clipboard.SaveImage

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
		m.err = msg.err.Error()
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
	m.err = ""
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
	m.err = ""
	names, index := m.defaultToolSelection()
	m.quick = quickState{
		active:         true,
		input:          input,
		toolNames:      names,
		toolIndex:      index,
		closeAfterSend: m.quickCloseAfterSend(),
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
	case "ctrl+v":
		if m.quickPasting() {
			return m, nil
		}
		if !m.quickRoomForToken(m.quick.lastImageID + 1) {
			m.err = "prompt is full - shorten it before pasting an image"
			return m, nil
		}
		// The chip goes in at the caret now and fills in when the
		// off-thread clipboard read lands, so it holds the spot the user
		// pasted at even while they keep typing.
		m.quick.lastImageID++
		id := m.quick.lastImageID
		m.quick.attachments = append(m.quick.attachments, quickAttachment{id: id})
		m.insertQuickToken(&m.quick.attachments[len(m.quick.attachments)-1])
		m.err = ""
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
		m.err = "nothing selected"
		return m, nil
	}
	if m.quickPasting() {
		m.err = "still reading the pasted image - try again in a moment"
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
	m.clearQuickAfterSend()
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
	m.clearQuickAfterSend()
	m.err = ""
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

const focusKeySetting = "focus_key"

// enterFocuses reports which key opens a session where. Enter focuses the
// preview and A attaches full screen by default; a stored "attach" choice
// swaps the pair. Cached on the model because the footer reads it every
// frame.
func (m *Model) enterFocuses() bool {
	return m.focusOnEnter
}

// storedFocusOnEnter reads the persisted key choice. A read failure yields
// the default pairing.
func storedFocusOnEnter(st *store.Store) bool {
	chosen, err := st.Setting(focusKeySetting)
	if err != nil {
		return true
	}
	return chosen != "attach"
}

const quickCloseSetting = "quick_prompt_close"

// quickCloseAfterSend reports whether the quick bar should dismiss itself
// once a prompt is delivered. Staying open is the default; a stored "close"
// choice opts in. A store error is surfaced but still yields the default.
func (m *Model) quickCloseAfterSend() bool {
	chosen, err := m.store.Setting(quickCloseSetting)
	if err != nil {
		m.err = "reading quick prompt setting: " + err.Error()
		return false
	}
	return chosen == "close"
}

func (m *Model) openSettings() {
	if len(m.cfg.Tools) == 0 {
		m.err = "no tools configured"
		return
	}
	m.err = ""
	names, index := m.defaultToolSelection()
	m.settings = settingsState{
		toolNames:      names,
		toolIndex:      index,
		themeIndex:     themeIndex(current.Name),
		layoutSplit:    m.defaultSplitLayout(),
		quickCloseSend: m.quickCloseAfterSend(),
		enterFocuses:   m.enterFocuses(),
	}
	m.mode = modeSettings
}

func (m *Model) handleSettingsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		m.settings.field = (m.settings.field + settingsFieldCount - 1) % settingsFieldCount
	case "down", "j":
		m.settings.field = (m.settings.field + 1) % settingsFieldCount
	case "left", "h":
		m.cycleSetting(-1)
	case "right", "l":
		m.cycleSetting(1)
	case "enter", "esc":
		if err := m.store.SetSetting("default_tool", m.settings.toolNames[m.settings.toolIndex]); err != nil {
			m.err = err.Error()
		}
		if err := m.store.SetSetting(themeSetting, themes[m.settings.themeIndex].Name); err != nil {
			m.err = err.Error()
		}
		layout := "split"
		if !m.settings.layoutSplit {
			layout = "unified"
		}
		if err := m.store.SetSetting(diffLayoutSetting, layout); err != nil {
			m.err = err.Error()
		}
		quickClose := "stay"
		if m.settings.quickCloseSend {
			quickClose = "close"
		}
		if err := m.store.SetSetting(quickCloseSetting, quickClose); err != nil {
			m.err = err.Error()
		}
		focusKey := "focus"
		if !m.settings.enterFocuses {
			focusKey = "attach"
		}
		if err := m.store.SetSetting(focusKeySetting, focusKey); err != nil {
			m.err = err.Error()
		}
		m.focusOnEnter = m.settings.enterFocuses
		m.mode = modeList
	}
	return m, nil
}

// cycleSetting steps the focused setting by one. The theme applies as it
// is stepped so the picker doubles as a live preview of the palette.
func (m *Model) cycleSetting(step int) {
	switch m.settings.field {
	case settingsFieldTool:
		count := len(m.settings.toolNames)
		m.settings.toolIndex = (m.settings.toolIndex + step + count) % count
	case settingsFieldTheme:
		m.settings.themeIndex = (m.settings.themeIndex + step + len(themes)) % len(themes)
		applyTheme(themes[m.settings.themeIndex])
		SyncTerminalBackground()
	case settingsFieldLayout:
		m.settings.layoutSplit = !m.settings.layoutSplit
	case settingsFieldQuickClose:
		m.settings.quickCloseSend = !m.settings.quickCloseSend
	case settingsFieldFocusKey:
		m.settings.enterFocuses = !m.settings.enterFocuses
	}
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
		if !m.moveGroupCursor(-1) && len(m.form.groups) > 0 {
			m.form.groupIndex = len(m.form.groups) - 1
		}
		return m, nil
	case "down":
		if !m.moveGroupCursor(1) && len(m.form.groups) > 0 {
			m.form.groupIndex = 0
		}
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
