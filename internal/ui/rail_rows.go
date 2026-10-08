package ui

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/launch"
	"github.com/YoanWai/agent-manager/internal/store"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
)

type treeRow struct {
	isGroup bool
	group   string
	depth   int
	sess    store.Session
}

func (row treeRow) isRoot() bool { return row.isGroup && row.group == "" }

func rowKey(row treeRow) string {
	if row.isGroup {
		return "g:" + row.group
	}
	return "s:" + row.sess.ID
}

func (m *Model) railSnapshot() uirail.Snapshot {
	snapshot := uirail.Snapshot{
		Sessions:       make([]uirail.Session, 0, len(m.workspace.sessions)),
		Groups:         append([]string(nil), m.workspace.groups...),
		ArchivedGroups: make(map[string]bool, len(m.workspace.archivedGroups)),
	}
	for path, archived := range m.workspace.archivedGroups {
		snapshot.ArchivedGroups[path] = archived
	}
	for _, session := range m.workspace.sessions {
		prompt := m.workspace.panePrompts[session.ID]
		if prompt == "" {
			prompt = session.LastPrompt
		}
		if prompt == "" {
			prompt = launch.DeliveredPrompt(session.LaunchPrompt)
		}
		snapshot.Sessions = append(snapshot.Sessions, uirail.Session{
			ID:             session.ID,
			Name:           session.Name,
			DisplayName:    m.displayName(session),
			Tool:           session.Tool,
			Cwd:            session.Cwd,
			Group:          session.Group,
			Status:         session.Status,
			Archived:       session.Archived,
			Acked:          session.Acked,
			CreatedAt:      session.CreatedAt,
			LastStatusAt:   session.LastStatusAt,
			LaunchAt:       session.LaunchTime(),
			AgentSessionID: session.AgentSessionID,
			ParentID:       session.ParentID,
			Prompt:         prompt,
			PaneLine:       m.workspace.paneLines[session.ID],
			PanePrompt:     m.workspace.panePrompts[session.ID],
			Queued:         m.workspace.queuedMessages[session.ID],
			IsShell:        m.isShell(session.Tool),
			Elsewhere:      m.elsewhereNote(session) != "",
			AfterTurn:      session.AfterTurn,
			AfterTurnGlyph: afterTurnGlyphs[session.AfterTurn],
		})
	}
	return snapshot
}

func (m *Model) rebuildRows() uirail.SelectionChange {
	return m.rail.Reconcile(m.railSnapshot())
}

func (m *Model) selected() (store.Session, bool) {
	id, ok := m.rail.SelectedSession()
	if !ok {
		return store.Session{}, false
	}
	return m.sessionByID(id)
}

func (m *Model) selectedRow() (treeRow, bool) {
	selection, ok := m.rail.Selected()
	if !ok {
		return treeRow{}, false
	}
	if selection.Kind == uirail.GroupRow {
		return treeRow{isGroup: true, group: selection.Group}, true
	}
	session, ok := m.sessionByID(selection.SessionID)
	if !ok {
		return treeRow{}, false
	}
	return treeRow{group: session.Group, sess: session}, true
}

func (m *Model) selectedGroup() (string, bool) { return m.rail.SelectedGroup() }

func (m *Model) focusSession(id string) bool { return m.rail.FocusSession(id) }

func (m *Model) sessionByID(id string) (store.Session, bool) {
	for _, session := range m.workspace.sessions {
		if session.ID == id {
			return session, true
		}
	}
	return store.Session{}, false
}

func (m *Model) listedSessions() []store.Session {
	ids := m.rail.ListedSessionIDs()
	listed := make([]store.Session, 0, len(ids))
	for _, id := range ids {
		if session, ok := m.sessionByID(id); ok {
			listed = append(listed, session)
		}
	}
	return listed
}

func (m *Model) listedAgents() []store.Session {
	ids := m.rail.ListedAgentIDs()
	agents := make([]store.Session, 0, len(ids))
	for _, id := range ids {
		if session, ok := m.sessionByID(id); ok {
			agents = append(agents, session)
		}
	}
	return agents
}

func inGroupSubtree(sessionGroup, group string) bool {
	return sessionGroup == group || strings.HasPrefix(sessionGroup, group+"/")
}

func parentGroup(group string) string {
	if index := strings.LastIndex(group, "/"); index >= 0 {
		return group[:index]
	}
	return ""
}

func baseName(path string) string {
	if index := strings.LastIndex(path, "/"); index >= 0 {
		return path[index+1:]
	}
	return path
}
