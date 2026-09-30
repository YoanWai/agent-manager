package ui

import (
	"sort"
	"strings"

	"github.com/YoanWai/agent-manager/internal/store"
)

func groupClosure(groups []string, sessions []store.Session) map[string]bool {
	paths := map[string]bool{}
	add := func(path string) {
		for path != "" {
			paths[path] = true
			index := strings.LastIndex(path, "/")
			if index < 0 {
				break
			}
			path = path[:index]
		}
	}
	for _, group := range groups {
		add(group)
	}
	for _, session := range sessions {
		add(session.Group)
	}
	return paths
}

func childIndex(paths map[string]bool, ordered []string) map[string][]string {
	rank := make(map[string]int, len(ordered))
	for index, path := range ordered {
		rank[path] = index
	}
	children := map[string][]string{}
	for path := range paths {
		parent := parentGroup(path)
		children[parent] = append(children[parent], path)
	}
	for _, siblings := range children {
		sort.SliceStable(siblings, func(i, j int) bool {
			left, leftKnown := rank[siblings[i]]
			right, rightKnown := rank[siblings[j]]
			if leftKnown && rightKnown {
				return left < right
			}
			if leftKnown != rightKnown {
				return leftKnown
			}
			return siblings[i] < siblings[j]
		})
	}
	return children
}

func (m *Model) groupEffectivelyArchived(path string) bool {
	return store.EffectivelyArchived(m.workspace.archivedGroups, path)
}

func (m *Model) materializeGroupsLocal(paths []string) {
	known := make(map[string]bool, len(m.workspace.groups))
	for _, path := range m.workspace.groups {
		known[path] = true
	}
	for _, path := range paths {
		if !known[path] {
			m.workspace.groups = append(m.workspace.groups, path)
			known[path] = true
		}
	}
}
