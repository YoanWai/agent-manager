package ui

import (
	"fmt"
	"github.com/YoanWai/agent-manager/internal/store"
	"strings"
)

// removeSessionLocally takes a deleted row off the loaded list right away,
// so it leaves the screen on this frame instead of waiting for the next
// poll to confirm what the store already knows.
func (m *Model) removeSessionLocally(id string) {
	for i := range m.workspace.sessions {
		if m.workspace.sessions[i].ID == id {
			m.workspace.sessions = append(m.workspace.sessions[:i], m.workspace.sessions[i+1:]...)
			break
		}
	}
	m.markSession(id, goneMark{deleted: true})
	m.rebuildRows()
}

// pruneGroupsLocally drops the removed group paths from the loaded tree,
// so a deleted group's header goes with its sessions instead of hanging
// around empty until the next poll. Each path is recorded for the stale
// listing filter, which is what keeps an in-flight poll from restoring it.
func (m *Model) pruneGroupsLocally(removed []string) {
	gone := make(map[string]bool, len(removed))
	for _, path := range removed {
		gone[path] = true
		m.markGroup(path, goneMark{deleted: true})
	}
	groups := make([]string, 0, len(m.workspace.groups))
	for _, group := range m.workspace.groups {
		if !gone[group] {
			groups = append(groups, group)
		}
	}
	m.workspace.groups = groups
	for _, path := range removed {
		delete(m.workspace.groupPaths, path)
		delete(m.workspace.groupWorktrees, path)
		delete(m.workspace.archivedGroups, path)
	}
	m.rebuildRows()
}

func (m *Model) prepareDelete() {
	entry, ok := m.selectedRow()
	if !ok {
		return
	}
	if entry.isRoot() {
		m.errBar.text = "root is the top level; delete the sessions under it instead"
		return
	}
	if !entry.isGroup {
		sessions, err := m.sessionAndChildren(entry.sess)
		if err != nil {
			m.errBar.text = err.Error()
			return
		}
		m.confirm = confirmTarget{
			label: followConfirmLabel("delete", entry.sess.Name, len(sessions)-1,
				"kills its tmux session.",
				"kills their tmux sessions."),
			sessions: sessions,
		}
		m.mode = modeConfirmDelete
		return
	}
	subtree, err := m.services.store.SessionsInSubtree(entry.group)
	if err != nil {
		m.errBar.text = err.Error()
		return
	}
	if m.rail.ShowArchived() {
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
	for _, g := range m.workspace.groups {
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
	archived := archivedSessions(subtree)
	return confirmTarget{
		isGroup:      true,
		archivedOnly: true,
		path:         path,
		sessions:     archived,
		label: fmt.Sprintf("delete %s from the archive (%d archived sessions)? kills their tmux sessions, live ones stay.",
			path, len(archived)),
	}
}
