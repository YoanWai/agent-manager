package rail

import (
	"sort"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/keybind"
	tea "github.com/charmbracelet/bubbletea"
)

type InputMode uint8

const (
	BrowseMode InputMode = iota
	SearchMode
	MenuMode
	ReorderMode
)

func (m Model) InputMode() InputMode {
	switch {
	case m.reorder.active:
		return ReorderMode
	case m.menu.active:
		return MenuMode
	case m.searching:
		return SearchMode
	default:
		return BrowseMode
	}
}

func (m Model) ShowArchived() bool { return m.showArchived }
func (m Model) Searching() bool    { return m.searching }
func (m Model) Search() string     { return m.search }
func (m Model) MenuOpen() bool     { return m.menu.active }
func (m Model) Reordering() bool   { return m.reorder.active }
func (m Model) Cursor() int        { return m.cursor }

type ReorderInfo struct {
	Active    bool
	Dragging  bool
	DropLabel string
}

func (m Model) ReorderInfo() ReorderInfo {
	info := ReorderInfo{Active: m.reorder.active, Dragging: m.reorder.dragging}
	if m.reorder.drop != nil {
		info.DropLabel = m.reorder.drop.label
	}
	return info
}

func (m *Model) CancelClickFocus() { m.clickFocusKey = "" }

func (m *Model) ResetClickHistory() {
	m.listClickAt = time.Time{}
	m.listClickKey = ""
	m.clickFocusKey = ""
}

func (m *Model) SetSearch(query string, editing bool) {
	m.search, m.searching = query, editing
	m.rebuildRows()
}

func (m *Model) SetHideEmptyGroups(hide bool) {
	m.hideEmptyGroups = hide
	m.rebuildRows()
}

func (m Model) HideEmptyGroups() bool { return m.hideEmptyGroups }

func (m Model) FilteringAttention() bool { return m.filter == statusFilterAttention }

func (m *Model) SetFilteringAttention(attention bool) {
	if attention {
		m.filter = statusFilterAttention
	} else {
		m.filter = statusFilterAll
	}
	m.rebuildRows()
}

func (m *Model) SetCollapsed(path string, collapsed bool) {
	if collapsed {
		m.collapsed[path] = true
	} else {
		delete(m.collapsed, path)
	}
	m.rebuildRows()
}

func (m Model) IsCollapsed(path string) bool { return m.collapsed[path] }

func (m Model) Collapsed() []string {
	paths := make([]string, 0, len(m.collapsed))
	for path, collapsed := range m.collapsed {
		if collapsed {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	return paths
}

func (m Model) ListedSessionIDs() []string {
	sessions := m.listedSessions()
	ids := make([]string, len(sessions))
	for i, session := range sessions {
		ids[i] = session.ID
	}
	return ids
}

func (m Model) ListedAgentIDs() []string {
	var ids []string
	for _, session := range m.listedSessions() {
		if !session.IsShell {
			ids = append(ids, session.ID)
		}
	}
	return ids
}

func (m Model) HasStatus(state string) bool {
	for _, row := range m.rows {
		if row.kind == SessionRow && row.sess.Status == state {
			return true
		}
	}
	return false
}

func (m Model) HasWorkingLoader() bool {
	for _, row := range m.rows {
		if row.kind == SessionRow && row.sess.Status == "working" && row.sess.PaneLine == "" {
			return true
		}
	}
	return false
}

func (m *Model) Key(msg tea.KeyMsg, _ Frame, ctx KeyContext) Decision {
	switch m.InputMode() {
	case ReorderMode:
		return m.reorderKey(msg, ctx)
	case MenuMode:
		return m.menuKey(msg, ctx)
	case SearchMode:
		return m.searchKey(msg)
	}
	key := keybind.Normalize(msg.String())
	if key == "ctrl+c" {
		return m.intent(Quit)
	}
	if key == "esc" {
		return m.clearSearch()
	}
	action, _ := ctx.ListKeys.ActionFor(key)
	return m.action(action, ctx)
}

func (m *Model) action(action string, ctx KeyContext) Decision {
	switch action {
	case keybind.Quit:
		return m.intent(Quit)
	case keybind.Up:
		return m.Move(-1, true)
	case keybind.Down:
		return m.Move(1, true)
	case keybind.ReorderUp:
		return m.requestReorder(-1)
	case keybind.ReorderDown:
		return m.requestReorder(1)
	case keybind.Open:
		if row, ok := m.row(); ok && row.folds() {
			return m.toggleCollapse()
		}
		if ctx.EnterFocuses {
			return m.intent(Focus)
		}
		return m.intent(Attach)
	case keybind.StepIn:
		if !ctx.ArrowStep {
			return Decision{Consumed: true}
		}
		if row, ok := m.row(); ok && row.folds() {
			if m.collapsed[row.foldKey()] {
				return m.toggleCollapse()
			}
			return Decision{Consumed: true}
		}
		return m.intent(Focus)
	case keybind.StepOut:
		if !ctx.ArrowStep {
			return Decision{Consumed: true}
		}
		if row, ok := m.row(); ok && row.folds() && !m.collapsed[row.foldKey()] {
			return m.toggleCollapse()
		}
		return Decision{Consumed: true}
	case keybind.Attach:
		if ctx.EnterFocuses {
			return m.intent(Attach)
		}
		return m.intent(Focus)
	case keybind.NewSession:
		return m.intent(NewSession)
	case keybind.NewGroup:
		return m.intent(NewGroup)
	case keybind.NewConnection:
		return m.intent(NewConnection)
	case keybind.Fork:
		return m.intent(Fork)
	case keybind.Revive:
		return m.intent(Revive)
	case keybind.MarkIdle:
		return m.intent(MarkIdle)
	case keybind.ReviveAll:
		return m.intent(ReviveAll)
	case keybind.Restart:
		return m.intent(Restart)
	case keybind.Kill:
		return m.intent(Kill)
	case keybind.KillAll:
		return m.intent(KillAll)
	case keybind.Archive:
		return m.intent(Archive)
	case keybind.Restore:
		return m.intent(Restore)
	case keybind.Delete:
		return m.intent(Delete)
	case keybind.Prompt:
		return m.intent(Prompt)
	case keybind.CopyReply:
		return m.intent(CopyReply)
	case keybind.CancelEnd:
		return m.intent(CancelEnd)
	case keybind.FoldAll:
		return m.toggleCollapseAll()
	case keybind.Filter:
		return m.cycleFilter()
	case keybind.Settings:
		return m.intent(OpenSettings)
	case keybind.Resize:
		return m.intent(Resize)
	case keybind.Archived:
		m.showArchived = !m.showArchived
		return Decision{Consumed: true, Refresh: true, Selection: selectedOf(m)}
	case keybind.Terminal:
		return m.intent(NewTerminal)
	case keybind.Editor:
		return m.intent(OpenEditor)
	case keybind.EmptyGroups:
		return m.toggleEmptyGroups()
	case keybind.Search:
		m.searching = true
		return Decision{Consumed: true}
	case keybind.Rename:
		return m.intent(RenameAction)
	case keybind.Move:
		return m.intent(MoveToGroup)
	case keybind.Messages:
		return m.intent(OpenMessages)
	case keybind.Help:
		return m.intent(OpenHelp)
	case keybind.Review:
		return m.intent(OpenReview)
	default:
		return Decision{}
	}
}

func (m *Model) intent(kind ActionKind) Decision {
	selected, _ := m.Selected()
	return Decision{Consumed: true, Selection: selected, Intent: Intent{Kind: kind, Target: selected}}
}

func selectedOf(m *Model) Selection {
	selected, _ := m.Selected()
	return selected
}

func (m *Model) searchKey(msg tea.KeyMsg) Decision {
	previous, previousOK := m.Selected()
	switch msg.String() {
	case "enter":
		m.searching = false
	case "esc":
		m.searching = false
		return m.clearSearch()
	case "backspace":
		runes := []rune(m.search)
		if len(runes) > 0 {
			m.search = string(runes[:len(runes)-1])
		}
		m.rebuildRows()
	default:
		switch msg.Type {
		case tea.KeyRunes:
			m.search += string(msg.Runes)
			m.rebuildRows()
		case tea.KeySpace:
			m.search += " "
			m.rebuildRows()
		}
	}
	current, currentOK := m.Selected()
	return Decision{Consumed: true, SelectionChanged: previousOK != currentOK || previous != current, Selection: current}
}

func (m *Model) clearSearch() Decision {
	if m.search == "" {
		return Decision{Consumed: true}
	}
	previous, previousOK := m.Selected()
	m.search = ""
	m.rebuildRows()
	current, currentOK := m.Selected()
	return filterSelectionDecision(previous, previousOK, current, currentOK)
}

func (m *Model) cycleFilter() Decision {
	previous, previousOK := m.Selected()
	if m.filter == statusFilterAll {
		m.filter = statusFilterAttention
	} else {
		m.filter = statusFilterAll
	}
	m.rebuildRows()
	current, currentOK := m.Selected()
	return filterSelectionDecision(previous, previousOK, current, currentOK)
}

func (m *Model) toggleEmptyGroups() Decision {
	if m.showArchived {
		return Decision{Consumed: true}
	}
	previous, previousOK := m.Selected()
	m.hideEmptyGroups = !m.hideEmptyGroups
	m.rebuildRows()
	current, currentOK := m.Selected()
	return filterSelectionDecision(previous, previousOK, current, currentOK)
}

func filterSelectionDecision(previous Selection, previousOK bool, current Selection, currentOK bool) Decision {
	decision := Decision{
		Consumed:         true,
		SelectionChanged: previousOK != currentOK || previous != current,
		Selection:        current,
	}
	if decision.SelectionChanged {
		decision.SelectionEffect = SelectionEffectFilter
	}
	return decision
}

func (m *Model) toggleCollapse() Decision {
	row, ok := m.row()
	if !ok {
		return Decision{Consumed: true}
	}
	key := row.foldKey()
	if key == "" {
		return Decision{Consumed: true}
	}
	m.collapsed[key] = !m.collapsed[key]
	m.rebuildRows()
	return m.collapsedDecision()
}

func (m *Model) toggleCollapseAll() Decision {
	collapse := !m.AllGroupsCollapsed()
	for key := range m.foldable() {
		m.collapsed[key] = collapse
	}
	m.rebuildRows()
	return m.collapsedDecision()
}

func (m Model) AllGroupsCollapsed() bool {
	any := false
	for key := range m.foldable() {
		if !m.collapsed[key] {
			return false
		}
		any = true
	}
	return any
}

func (m Model) collapsedDecision() Decision {
	return Decision{
		Consumed:  true,
		Selection: selectedOfValue(m),
		Mutations: []Mutation{{Kind: SaveCollapsed, Collapsed: m.Collapsed()}},
	}
}

func selectedOfValue(m Model) Selection {
	selected, _ := m.Selected()
	return selected
}

func (m *Model) ClearStatusFilter() {
	if m.filter == statusFilterAll {
		return
	}
	m.filter = statusFilterAll
	m.rebuildRows()
}

func (m *Model) SetArchived(show bool) {
	if m.showArchived == show {
		return
	}
	m.showArchived = show
	m.rebuildRows()
}

func (m *Model) ForgetGroups(paths []string) Decision {
	changed := false
	for _, removed := range paths {
		for path := range m.collapsed {
			if path == removed || strings.HasPrefix(path, removed+"/") {
				delete(m.collapsed, path)
				changed = true
			}
		}
	}
	if !changed {
		return Decision{Consumed: true}
	}
	m.rebuildRows()
	return m.collapsedDecision()
}

func (m *Model) RenameGroup(oldPath, newPath string) Decision {
	for path, collapsed := range m.collapsed {
		if path != oldPath && !strings.HasPrefix(path, oldPath+"/") {
			continue
		}
		delete(m.collapsed, path)
		renamed := newPath + strings.TrimPrefix(path, oldPath)
		m.collapsed[renamed] = collapsed
	}
	m.rebuildRows()
	return m.collapsedDecision()
}

func (m *Model) OpenGroup(path string) Decision {
	if !m.collapsed[path] {
		return Decision{Consumed: true}
	}
	m.collapsed[path] = false
	m.rebuildRows()
	return m.collapsedDecision()
}

// RevealGroup resets list narrowing after a successful group creation, opens
// its ancestors, and selects the new group if it is present in the current
// copied inventory.
func (m *Model) RevealGroup(path string) Decision {
	previous, previousOK := m.Selected()
	m.showArchived = false
	m.hideEmptyGroups = false
	m.filter = statusFilterAll
	m.search = ""
	m.searching = false
	collapsedChanged := false
	for ancestor := parentGroup(path); ancestor != ""; ancestor = parentGroup(ancestor) {
		if m.collapsed[ancestor] {
			delete(m.collapsed, ancestor)
			collapsedChanged = true
		}
	}
	m.rebuildRows()
	m.Focus(Selection{Kind: GroupRow, Group: path})
	current, currentOK := m.Selected()
	decision := Decision{
		Consumed:         true,
		SelectionChanged: previousOK != currentOK || previous != current,
		Selection:        current,
	}
	if collapsedChanged {
		decision.Mutations = []Mutation{{Kind: SaveCollapsed, Collapsed: m.Collapsed()}}
	}
	return decision
}
