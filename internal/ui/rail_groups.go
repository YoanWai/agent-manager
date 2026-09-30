package ui

import (
	"encoding/json"
	"github.com/YoanWai/agent-manager/internal/store"
	"sort"
	"strings"
)

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
	m.rail.collapsed[path] = !m.rail.collapsed[path]
	m.persistCollapsed()
	m.rebuildRows()
}

// toggleCollapseAll folds every group when any is open, and unfolds all
// when they are already collapsed, so one key flips the whole tree.
func (m *Model) toggleCollapseAll() {
	groups := groupClosure(m.workspace.groups, m.workspace.sessions)
	collapse := !m.allGroupsCollapsed()
	for group := range groups {
		m.rail.collapsed[group] = collapse
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
	for group := range groupClosure(m.workspace.groups, m.workspace.sessions) {
		if !m.rail.collapsed[group] {
			return false
		}
		any = true
	}
	return any
}

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
	paths := make([]string, 0, len(m.rail.collapsed))
	for path, folded := range m.rail.collapsed {
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
	if err := m.services.store.SetSetting(collapsedSetting, string(raw)); err != nil {
		m.errBar.text = err.Error()
	}
}

// inGroupSubtree reports whether a session's group sits at or below the
// given group in the tree.
func inGroupSubtree(sessGroup, group string) bool {
	return sessGroup == group || strings.HasPrefix(sessGroup, group+"/")
}

func (m *Model) selectedGroup() (string, bool) {
	if entry, ok := m.selectedRow(); ok && entry.isGroup {
		return entry.group, true
	}
	return "", false
}

func parentGroup(group string) string {
	if idx := strings.LastIndex(group, "/"); idx >= 0 {
		return group[:idx]
	}
	return ""
}
