package ui

import (
	"fmt"

	"github.com/YoanWai/agent-manager/internal/clipboard"
	"github.com/YoanWai/agent-manager/internal/sysstat"
	"github.com/YoanWai/agent-manager/internal/tmux"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Resize mode owns the keyboard until the drag commits or the user
	// cancels: other bindings would fight the mouse-gated session.
	if m.split.resizeMode {
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
			m.split.resizeMode = false
			m.split.dragging = false
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
	case modeLaunchHint:
		return m.handleLaunchHintKey(msg)
	case modeRename:
		return m.handleRenameKey(msg)
	case modeFork:
		return m.handleForkKey(msg)
	case modeSettings:
		return m.handleSettingsKey(msg)
	case modeMove:
		return m.handleMoveKey(msg)
	case modeRepoPick:
		return m.handleRepoPickKey(msg)
	case modeRunPick:
		return m.handleRunPickKey(msg)
	case modeRunInit:
		return m.handleRunInitKey(msg)
	case modeGroupForm:
		return m.handleGroupFormKey(msg)
	case modeDiff:
		return m.handleDiffKey(msg)
	case modeFocus:
		return m.handleFocusKey(msg)
	case modeNotices:
		return m.handleNoticesKey(msg)
	case modeHelp:
		return m.handleHelpKey(msg)
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
	case "right":
		if !m.arrowStep {
			return m, nil
		}
		if entry, ok := m.selectedRow(); ok && entry.isGroup {
			if m.collapsed[entry.group] {
				m.toggleCollapse()
			}
			return m, nil
		}
		return m.focusSelected()
	case "left":
		if !m.arrowStep {
			return m, nil
		}
		if entry, ok := m.selectedRow(); ok && entry.isGroup && !m.collapsed[entry.group] {
			m.toggleCollapse()
		}
		return m, nil
	case "A", "shift+a":
		if m.enterFocuses() {
			return m.attachSelected()
		}
		return m.focusSelected()
	case "n":
		m.openForm()
	case "g":
		m.openGroupForm()
	case "f":
		m.openFork()
	case "v":
		return m.reviveSelected()
	case ".":
		return m.acknowledgeSelected()
	case "V", "shift+v":
		return m.reviveAllDead()
	case "R", "shift+r":
		return m.restartSelected()
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
	case "w":
		return m, m.cycleStatusFilter()
	case "s":
		m.openSettings()
	case "|":
		return m.enterResizeMode()
	case "t":
		m.showArchived = !m.showArchived
		m.requestRefresh()
	case "T", "shift+t":
		return m.terminalKey()
	case "p":
		return m.runKey()
	case "o":
		return m.openEditor()
	case "e":
		return m, m.toggleEmptyGroups()
	case "/":
		m.searching = true
		m.errBar.text = ""
	case "esc":
		return m, m.clearSearch()
	case "r":
		m.openRename()
	case "m":
		m.openMove()
	case "M", "shift+m":
		m.openNotices("")
	case "?":
		m.openHelp()
	case "ctrl+r":
		return m, m.openDiff()
	}
	return m, nil
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
		m.errBar.text = "root stays at the top of the list"
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
		m.errBar.text = fmt.Sprintf("%s already at the %s of its level", what, edge)
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
		m.errBar.text = err.Error()
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
	m.errBar.text = ""
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
	// The pinned block and the tree are separate orderings, so a reorder
	// never pairs a row in one with a row in the other.
	from, to := 0, len(m.treeRows())
	if m.cursor >= to {
		from, to = to, len(m.rows)
	}
	for i := m.cursor + step; i >= from && i < to; i += step {
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
	collapse := !m.allGroupsCollapsed()
	for group := range groups {
		m.collapsed[group] = collapse
	}
	m.persistCollapsed()
	m.rebuildRows()
}

// allGroupsCollapsed reports whether the whole tree is folded, which is
// what decides the direction F takes and the label the footer offers. A
// tree with no groups is not folded: there is nothing to unfold, and the
// label must not offer it.
func (m *Model) allGroupsCollapsed() bool {
	any := false
	for group := range groupClosure(m.groups, m.sessions) {
		if !m.collapsed[group] {
			return false
		}
		any = true
	}
	return any
}

// pasteFocused is the seam tests swap to observe pastes into the pane.
var pasteFocused = func(driver *tmux.Driver, id, text string) error {
	return driver.Paste(id, text)
}

// cycleStatusFilter advances the list status filter (all → attention → …).
// Modes live in statusFilterCycle so new ones only need a const and a
// matches case; this handler stays the same.
func (m *Model) cycleStatusFilter() tea.Cmd {
	previousKey := ""
	if entry, ok := m.selectedRow(); ok {
		previousKey = rowKey(entry)
	}
	m.statusFilter = m.statusFilter.next()
	m.rebuildRows()
	return m.afterListFilter(previousKey)
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
	return m.afterListFilter(previousKey)
}

// afterListFilter keeps the preview tied to the selection when a filter
// change leaves the cursor on the same row, and refreshes it when not.
func (m *Model) afterListFilter(previousKey string) tea.Cmd {
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

// warn carries a PrepareAttach failure: shown to the user, but the attach
// still proceeds, unlike err which cancels it.
type reattachPreparedMsg struct {
	sessID  string
	diffGen int
	err     error
	warn    string
}

// captureClipboardImage is the seam the quick bar uses to save a pasted
// image to a temp file; tests swap it for a fake.
var captureClipboardImage = clipboard.SaveImage

const diffLayoutSetting = "diff_layout"

const listDensitySetting = "list_density"

const focusKeySetting = "focus_key"

// arrowStepSetting is the beta ←→ pair: "off" turns it off, anything else
// leaves it on.
const arrowStepSetting = "arrow_step_keys"

const quickCloseSetting = "quick_prompt_close"

const worktreeSetting = "worktree_default"

// terminalPlacementSetting is where shells sit: "pinned" gathers them in
// their own block, "inline" leaves them among the agents in their group.
const terminalPlacementSetting = "terminal_placement"

const notificationsSetting = "notifications"

const notifyFinishedSetting = "notify_finished"

// hiddenToolsSetting lists CLI tools omitted from new-session pickers
// (comma-separated names). Empty means every configured tool is shown.
const hiddenToolsSetting = "hidden_tools"

func (m *Model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.searching = false
	case "esc":
		m.searching = false
		return m, m.clearSearch()
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

// clearSearch drops the query and re-lists. A query that outlives its field
// with no way back is what makes filtered-away sessions read as sessions
// that are gone, so esc answers from the list as well as from the field.
func (m *Model) clearSearch() tea.Cmd {
	if m.search == "" {
		return nil
	}
	previousKey := ""
	if entry, ok := m.selectedRow(); ok {
		previousKey = rowKey(entry)
	}
	m.search = ""
	m.rebuildRows()
	return m.afterListFilter(previousKey)
}
