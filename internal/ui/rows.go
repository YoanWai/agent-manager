package ui

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/YoanWai/agent-manager/internal/store"
)

const collapsedSetting = "collapsed_groups"

// loadCollapsed restores the set of folded group paths persisted from a
// previous run so the tree opens in the same shape the user left it.
func loadCollapsed(st *store.Store) map[string]bool {
	collapsed := map[string]bool{}
	raw, err := st.Setting(collapsedSetting)
	if err != nil || raw == "" {
		return collapsed
	}
	var paths []string
	if err := json.Unmarshal([]byte(raw), &paths); err != nil {
		return collapsed
	}
	for _, path := range paths {
		collapsed[path] = true
	}
	return collapsed
}

// persistCollapsed saves the currently folded group paths so the state
// survives across launches.
func (m *Model) persistCollapsed() {
	paths := make([]string, 0, len(m.collapsed))
	for path, folded := range m.collapsed {
		if folded {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	raw, err := json.Marshal(paths)
	if err != nil {
		m.errBar.text = err.Error()
		return
	}
	if err := m.store.SetSetting(collapsedSetting, string(raw)); err != nil {
		m.errBar.text = err.Error()
	}
}

// visibleSessions filters to the sessions the current view scope shows:
// active ones normally, archived ones in the archived view. It also
// covers the frames between a scope toggle and the next refresh, when
// m.sessions still carries the other scope's list. Status filters apply
// later via listedSessions.
func (m *Model) visibleSessions() []store.Session {
	visible := make([]store.Session, 0, len(m.sessions))
	for _, sess := range m.sessions {
		if sess.Archived == m.showArchived {
			visible = append(visible, sess)
		}
	}
	return visible
}

// listedSessions is the archived scope narrowed by the status filter.
// Header counts, group rollups, and the tree all share this set so the
// numbers always match what the list can show.
//
// The selected session stays listed when its status leaves the filter
// (finished → idle on enter/ack) so rebuild cannot eject the cursor mid-work.
func (m *Model) listedSessions() []store.Session {
	visible := m.visibleSessions()
	if !m.statusFilter.active() {
		return visible
	}
	heldID := ""
	if sess, ok := m.selected(); ok {
		heldID = sess.ID
	}
	listed := make([]store.Session, 0, len(visible))
	for _, sess := range visible {
		if m.statusFilter.matches(sess.Status) || sess.ID == heldID {
			listed = append(listed, sess)
		}
	}
	return listed
}

// listedAgents is listedSessions without the shells, for the rollups that
// describe what a group is working on.
func (m *Model) listedAgents() []store.Session {
	listed := m.listedSessions()
	agents := make([]store.Session, 0, len(listed))
	for _, sess := range listed {
		if !m.isShell(sess.Tool) {
			agents = append(agents, sess)
		}
	}
	return agents
}

func (m *Model) selected() (store.Session, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) || m.rows[m.cursor].isGroup {
		return store.Session{}, false
	}
	return m.rows[m.cursor].sess, true
}

func (m *Model) selectedRow() (treeRow, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return treeRow{}, false
	}
	return m.rows[m.cursor], true
}

// focusSession puts the cursor on a session's row, for the keys that make
// one and leave the user on it, and reports whether it found the row. A
// session filtered out of the current view has none, and the cursor stays
// where it was.
func (m *Model) focusSession(id string) bool {
	for i, row := range m.rows {
		if !row.isGroup && row.sess.ID == id {
			m.cursor = i
			return true
		}
	}
	return false
}

// rootGroup is the path every ungrouped session already carries.
const rootGroup = ""

// isRoot marks the pinned top-level row, which is not a stored group.
func (e treeRow) isRoot() bool { return e.isGroup && e.group == rootGroup }

// rowsBelowRoot is the tree without its pinned row: empty means the rail
// has nothing to list, however many rows it paints.
func rowsBelowRoot(rows []treeRow) []treeRow {
	if len(rows) > 0 && rows[0].isRoot() {
		return rows[1:]
	}
	return rows
}

func rowKey(entry treeRow) string {
	if entry.isGroup {
		return "g:" + entry.group
	}
	return "s:" + entry.sess.ID
}

// rebuildRows walks the group tree depth-first and emits one row per
// group node and per session, honoring collapse state, search, and the
// status filter. The cursor follows the previously selected row's
// identity, so list changes from the 2s poll never yank the selection.
func (m *Model) rebuildRows() {
	previousKey := ""
	if entry, ok := m.selectedRow(); ok {
		previousKey = rowKey(entry)
	}
	query := strings.ToLower(strings.TrimSpace(m.search))
	prunedView := query != "" || m.statusFilter.active()

	listed := m.listedSessions()
	listedIDs := make(map[string]bool, len(listed))
	for _, sess := range listed {
		listedIDs[sess.ID] = true
	}
	byID := make(map[string]store.Session, len(m.sessions))
	for _, sess := range m.sessions {
		byID[sess.ID] = sess
	}
	// m.sessions arrives ordered by the store (group, sort_order), so
	// per-group slices inherit the user's manual order.
	matched := make(map[string]bool, len(listed))
	for _, sess := range listed {
		if query == "" || matchesSearch(sess, query) {
			matched[sess.ID] = true
		}
	}
	// A parent the search itself missed still comes along to carry its
	// matching children, in the store's order rather than after them.
	carried := map[string]bool{}
	for _, sess := range listed {
		if !matched[sess.ID] || sess.ParentID == "" || !listedIDs[sess.ParentID] {
			continue
		}
		carried[sess.ParentID] = true
	}
	sessionsByGroup := map[string][]store.Session{}
	childrenByParent := map[string][]store.Session{}
	for _, sess := range listed {
		if sess.ParentID != "" {
			if _, ok := byID[sess.ParentID]; ok {
				if matched[sess.ID] {
					childrenByParent[sess.ParentID] = append(childrenByParent[sess.ParentID], sess)
				}
				continue
			}
		}
		if matched[sess.ID] || carried[sess.ID] {
			sessionsByGroup[sess.Group] = append(sessionsByGroup[sess.Group], sess)
		}
	}
	walked := map[string]bool{}
	for _, groupSessions := range sessionsByGroup {
		for _, sess := range groupSessions {
			walked[sess.ID] = true
		}
	}
	orphaned := map[string]bool{}
	// A child whose parent never made it into a group paints un-nested, in
	// the store's order rather than the order the parent map happens to
	// yield.
	for _, sess := range listed {
		if _, nested := childrenByParent[sess.ParentID]; !nested || walked[sess.ParentID] {
			continue
		}
		if !matched[sess.ID] {
			continue
		}
		sessionsByGroup[sess.Group] = append(sessionsByGroup[sess.Group], sess)
		orphaned[sess.ParentID] = true
	}
	for parentID := range orphaned {
		delete(childrenByParent, parentID)
	}

	paths := groupClosure(m.groups, m.sessions)
	if m.showArchived {
		// The archived view keeps groups that hold archived sessions plus any
		// group whose subtree was archived as a whole (even with no sessions),
		// instead of the full tree skeleton.
		kept := pathsWithSessions(paths, sessionsByGroup)
		for path := range paths {
			if m.groupEffectivelyArchived(path) {
				addWithAncestors(kept, path)
			}
		}
		paths = kept
	} else {
		// The active view hides any archived group and its whole subtree.
		for path := range paths {
			if m.groupEffectivelyArchived(path) {
				delete(paths, path)
			}
		}
		if prunedView {
			paths = pathsWithSessions(paths, sessionsByGroup)
		}
	}
	if m.hideEmptyGroups && !m.showArchived {
		// This is a presentation filter only: stored groups remain available
		// to forms and return to the tree as soon as the toggle is switched
		// off. Ancestors of groups with visible sessions stay in the tree.
		paths = pathsWithSessions(paths, sessionsByGroup)
	}
	children := childIndex(paths, m.groups)

	// Folds are a browsing convenience for the active tree; the archived,
	// search, and status-filter views already prune to matching groups, so
	// honoring folds there would hide the very sessions the user came for.
	honorFolds := !prunedView && !m.showArchived

	// Root is a standing move and spawn target; its sessions stay flat.
	rows := make([]treeRow, 0, len(m.sessions)+len(paths)+1)
	appendSession := func(sess store.Session, depth int) {
		rows = append(rows, treeRow{sess: sess, depth: depth})
		for _, child := range childrenByParent[sess.ID] {
			rows = append(rows, treeRow{sess: child, depth: depth + 1})
		}
	}
	rows = append(rows, treeRow{isGroup: true, group: rootGroup})
	for _, sess := range sessionsByGroup[""] {
		appendSession(sess, 0)
	}
	var walk func(path string, depth int)
	walk = func(path string, depth int) {
		rows = append(rows, treeRow{isGroup: true, group: path, depth: depth})
		if honorFolds && m.collapsed[path] {
			return
		}
		for _, sess := range sessionsByGroup[path] {
			appendSession(sess, depth+1)
		}
		for _, child := range children[path] {
			walk(child, depth+1)
		}
	}
	for _, root := range children[""] {
		walk(root, 0)
	}

	m.rows = rows
	if previousKey != "" {
		for i, entry := range rows {
			if rowKey(entry) == previousKey {
				m.cursor = i
				break
			}
		}
	} else if m.cursor == 0 && len(rows) > 1 && rows[0].isRoot() {
		// A launch opens on a session, not on root's rollup.
		m.cursor = 1
	}
	if m.cursor >= len(rows) {
		m.cursor = len(rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// groupClosure unions stored groups with groups referenced by sessions,
// then adds every ancestor so partial paths always render.
func groupClosure(groups []string, sessions []store.Session) map[string]bool {
	paths := map[string]bool{}
	add := func(path string) {
		for path != "" {
			paths[path] = true
			idx := strings.LastIndex(path, "/")
			if idx < 0 {
				break
			}
			path = path[:idx]
		}
	}
	for _, g := range groups {
		add(g)
	}
	for _, sess := range sessions {
		add(sess.Group)
	}
	return paths
}

func (m *Model) groupEffectivelyArchived(path string) bool {
	return store.EffectivelyArchived(m.archivedGroups, path)
}

func addWithAncestors(set map[string]bool, path string) {
	for path != "" {
		set[path] = true
		idx := strings.LastIndex(path, "/")
		if idx < 0 {
			break
		}
		path = path[:idx]
	}
}

func pathsWithSessions(paths map[string]bool, sessionsByGroup map[string][]store.Session) map[string]bool {
	kept := map[string]bool{}
	for path := range paths {
		for group := range sessionsByGroup {
			if inGroupSubtree(group, path) {
				kept[path] = true
				break
			}
		}
	}
	return kept
}

// childIndex maps each group to its ordered children: stored groups in
// the user's manual order, synthesized ancestors alphabetically after.
func childIndex(paths map[string]bool, ordered []string) map[string][]string {
	rank := make(map[string]int, len(ordered))
	for i, name := range ordered {
		rank[name] = i
	}
	children := map[string][]string{}
	for path := range paths {
		parent := parentGroup(path)
		children[parent] = append(children[parent], path)
	}
	for _, siblings := range children {
		sort.SliceStable(siblings, func(i, j int) bool {
			ri, oki := rank[siblings[i]]
			rj, okj := rank[siblings[j]]
			if oki && okj {
				return ri < rj
			}
			if oki != okj {
				return oki
			}
			return siblings[i] < siblings[j]
		})
	}
	return children
}

func baseName(path string) string {
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		return path[idx+1:]
	}
	return path
}

func matchesSearch(sess store.Session, query string) bool {
	return strings.Contains(strings.ToLower(sess.Name), query) ||
		strings.Contains(strings.ToLower(sess.Tool), query) ||
		strings.Contains(strings.ToLower(sess.Group), query) ||
		strings.Contains(strings.ToLower(sess.Status), query)
}
