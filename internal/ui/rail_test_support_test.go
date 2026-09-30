package ui

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/launch"
	"github.com/YoanWai/agent-manager/internal/store"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	rowMenuGlyph = uirail.MenuGlyph
	reorderGrip  = uirail.ReorderGlyph
	rootGroup    = ""
)

func railRows(m *Model) []treeRow {
	rows := m.rail.Rows()
	resolved := make([]treeRow, 0, len(rows))
	for _, row := range rows {
		if row.Kind == uirail.GroupRow {
			resolved = append(resolved, treeRow{isGroup: true, group: row.Group, depth: row.Depth})
			continue
		}
		session, ok := m.sessionByID(row.SessionID)
		if !ok {
			session = store.Session{ID: row.SessionID, Name: row.Name, Tool: row.Tool, Group: row.Group, Status: row.Status}
		}
		resolved = append(resolved, treeRow{group: session.Group, depth: row.Depth, sess: session})
	}
	return resolved
}

func railSelectedSession(m *Model) store.Session {
	session, _ := m.selected()
	return session
}

func (m *Model) visibleSessions() []store.Session {
	visible := make([]store.Session, 0, len(m.workspace.sessions))
	for _, session := range m.workspace.sessions {
		if session.Archived == m.rail.ShowArchived() {
			visible = append(visible, session)
		}
	}
	return visible
}

func setRailCursor(m *Model, index int) bool {
	rows := m.rail.Rows()
	if index < 0 || index >= len(rows) {
		return false
	}
	return m.rail.Focus(rows[index].Selection)
}

func groupRow(t testingT, m *Model, path string) treeRow {
	t.Helper()
	for _, row := range railRows(m) {
		if row.isGroup && row.group == path {
			return row
		}
	}
	t.Fatalf("no group row %q", path)
	return treeRow{}
}

type testingT interface {
	Helper()
	Fatalf(string, ...any)
}

func railHitRows(m *Model) []int {
	rows := m.rail.Rows()
	hits := make([]int, len(m.displayedRail.Lines))
	for lineIndex, line := range m.displayedRail.Lines {
		hits[lineIndex] = -1
		if !line.HitOK {
			continue
		}
		for rowIndex, row := range rows {
			if row.Selection == line.Hit {
				hits[lineIndex] = rowIndex
				break
			}
		}
	}
	return hits
}

func selectionKeyForTest(selection uirail.Selection) string {
	if selection.Kind == uirail.GroupRow {
		return "g:" + selection.Group
	}
	return "s:" + selection.SessionID
}

func clearRailInventory(m *Model) {
	m.workspace.sessions = nil
	m.workspace.groups = nil
	m.rebuildRows()
}

func resetRailModel(m *Model) {
	m.rail = uirail.New(nil)
	m.displayedRail = uirail.Frame{}
}

func railModelFromRows(rows []treeRow, cursor int) uirail.Model {
	model := uirail.New(nil)
	if len(rows) == 0 {
		return model
	}
	snapshot := uirail.Snapshot{ArchivedGroups: map[string]bool{}}
	selections := make([]uirail.Selection, 0, len(rows))
	for index, row := range rows {
		if row.isGroup {
			if row.group != "" {
				snapshot.Groups = append(snapshot.Groups, row.group)
			}
			selections = append(selections, uirail.Selection{Kind: uirail.GroupRow, Group: row.group})
			continue
		}
		session := row.sess
		if session.ID == "" {
			session.ID = "fixture-" + string(rune('a'+index))
		}
		snapshot.Sessions = append(snapshot.Sessions, uirail.Session{
			ID: session.ID, Name: session.Name, DisplayName: session.Name, Tool: session.Tool,
			Cwd: session.Cwd, Group: session.Group, Status: session.Status,
			Archived: session.Archived, Acked: session.Acked, CreatedAt: session.CreatedAt,
			LastStatusAt: session.LastStatusAt, LaunchAt: session.LaunchTime(),
			AgentSessionID: session.AgentSessionID, ParentID: session.ParentID,
		})
		selections = append(selections, uirail.Selection{Kind: uirail.SessionRow, SessionID: session.ID, Group: session.Group})
	}
	model.Reconcile(snapshot)
	if cursor >= 0 && cursor < len(selections) {
		model.Focus(selections[cursor])
	}
	return model
}

func railModelSearching(rows []treeRow, cursor int, query string) uirail.Model {
	model := railModelFromRows(rows, cursor)
	model.SetSearch(query, true)
	return model
}

func railModelCount(count, cursor int, searching bool) uirail.Model {
	rows := make([]treeRow, max(count, 0))
	if count > 0 {
		rows[0] = treeRow{isGroup: true}
	}
	for index := 1; index < count; index++ {
		rows[index] = treeRow{sess: store.Session{ID: "fixture-" + string(rune('a'+index)), Name: "fixture"}}
	}
	model := railModelFromRows(rows, cursor)
	if searching {
		model.SetSearch("", true)
	}
	return model
}

func (m *Model) searchFieldLine(width int) string {
	frame := m.rail.Render(uirail.RenderContext{
		Width: width, Height: 8, TerminalWidth: max(m.width, width), TerminalHeight: max(m.height, 8),
		Theme: m.railTheme(), ListKeys: m.services.listKeys, CursorMarker: cursorAnchorMarker,
	}, uirail.Frame{})
	for _, line := range frame.Lines {
		if strings.Contains(line.Text, "⌕") {
			return line.Text
		}
	}
	return ""
}

func (m *Model) renderTreeRow(entry treeRow, selected bool, width, _ int, _ string) string {
	selection := uirail.Selection{Kind: uirail.SessionRow, SessionID: entry.sess.ID, Group: entry.sess.Group}
	snapshot := uirail.Snapshot{ArchivedGroups: map[string]bool{}}
	if entry.isGroup {
		selection = uirail.Selection{Kind: uirail.GroupRow, Group: entry.group}
		snapshot = groupRenderSnapshot(entry.group)
	} else {
		session := entry.sess
		prompt := m.workspace.panePrompts[session.ID]
		if prompt == "" {
			prompt = session.LastPrompt
		}
		if prompt == "" {
			prompt = launch.DeliveredPrompt(session.LaunchPrompt)
		}
		snapshot.Sessions = []uirail.Session{{
			ID: session.ID, Name: session.Name, DisplayName: m.displayName(session), Tool: session.Tool,
			Cwd: session.Cwd, Group: session.Group, Status: session.Status, Archived: session.Archived,
			Acked: session.Acked, CreatedAt: session.CreatedAt, LastStatusAt: session.LastStatusAt,
			LaunchAt: session.LaunchTime(), AgentSessionID: session.AgentSessionID, ParentID: session.ParentID,
			Prompt: prompt, PaneLine: m.workspace.paneLines[session.ID], PanePrompt: m.workspace.panePrompts[session.ID],
			Queued: m.workspace.queuedMessages[session.ID], IsShell: m.isShell(session.Tool),
			Elsewhere: m.elsewhereNote(session) != "",
		}}
		if session.Group != "" {
			snapshot.Groups = []string{session.Group}
		}
	}
	model := uirail.New(m.rail.Collapsed())
	if !entry.isGroup && entry.sess.Archived {
		model.SetArchived(true)
	}
	model.SetSearch(m.rail.Search(), m.rail.Searching())
	model.Reconcile(snapshot)
	if selected {
		model.Focus(selection)
	} else if entry.isRoot() {
		model.Focus(uirail.Selection{Kind: uirail.SessionRow, SessionID: "root-render-fixture"})
	} else {
		model.Focus(uirail.Selection{Kind: uirail.GroupRow})
	}
	frame := model.Render(uirail.RenderContext{
		Width: width, Height: 20, TerminalWidth: max(m.width, width), TerminalHeight: max(m.height, 20),
		Theme: m.railTheme(), ListKeys: m.services.listKeys, ComfortableRows: m.prefs.comfortableRows,
		MouseDisabled: m.prefs.mouseDisabled, Focused: m.mode == modeFocus,
		EnterFocuses: m.enterFocuses(), StartupPhase: m.startup.startupPhase, CursorMarker: cursorAnchorMarker,
	}, uirail.Frame{})
	var lines []string
	for _, line := range frame.Lines {
		if line.HitOK && line.Hit == selection {
			lines = append(lines, line.Text)
		}
	}
	return strings.Join(lines, "\n")
}

func (m *Model) entryHeight(entry treeRow) int {
	if entry.isGroup || !m.prefs.comfortableRows {
		return 1
	}
	if m.isShell(entry.sess.Tool) {
		return 2
	}
	return 3
}

func (m *Model) entryLines(_ []treeRow, _ int, width, height int) []contentLine {
	m.rebuildRows()
	frame := m.rail.Render(uirail.RenderContext{
		Width: width, Height: height, TerminalWidth: max(m.width, width), TerminalHeight: max(m.height, height),
		Theme: m.railTheme(), ListKeys: m.services.listKeys, ComfortableRows: m.prefs.comfortableRows,
		MouseDisabled: m.prefs.mouseDisabled, Focused: m.mode == modeFocus,
		EnterFocuses: m.enterFocuses(), StartupPhase: m.startup.startupPhase, CursorMarker: cursorAnchorMarker,
	}, m.displayedRail)
	m.displayedRail = frame
	return railContentLines(frame.Lines)
}

func (m *Model) reorderSelected(delta int) (tea.Model, tea.Cmd) {
	key := "J"
	if delta < 0 {
		key = "K"
	}
	return m.handleKey(runeKey(key))
}

func (m *Model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	return m.applyRailDecision(m.rail.Key(msg, m.displayedRail, m.railKeyContext()))
}

func setRailSessionTool(m *Model, id, tool string) {
	for index := range m.workspace.sessions {
		if m.workspace.sessions[index].ID == id {
			m.workspace.sessions[index].Tool = tool
			break
		}
	}
	m.rebuildRows()
}

func setRailSessionStatus(m *Model, id, state string) {
	for index := range m.workspace.sessions {
		if m.workspace.sessions[index].ID == id {
			m.workspace.sessions[index].Status = state
			break
		}
	}
	m.rebuildRows()
}

func setRailSessionWorktreeBranch(m *Model, id, branch string) {
	for index := range m.workspace.sessions {
		if m.workspace.sessions[index].ID == id {
			m.workspace.sessions[index].WorktreeBranch = branch
			break
		}
	}
	m.rebuildRows()
}

func setRailSessionCwd(m *Model, id, cwd string) {
	for index := range m.workspace.sessions {
		if m.workspace.sessions[index].ID == id {
			m.workspace.sessions[index].Cwd = cwd
			break
		}
	}
	m.rebuildRows()
}

func groupRenderSnapshot(group string) uirail.Snapshot {
	if group == "" {
		return uirail.Snapshot{Sessions: []uirail.Session{{ID: "root-render-fixture", Name: "fixture", DisplayName: "fixture"}}}
	}
	return uirail.Snapshot{Groups: []string{group}, Sessions: []uirail.Session{{ID: "group-fixture", Name: baseName(group), DisplayName: baseName(group), Group: group}}}
}
