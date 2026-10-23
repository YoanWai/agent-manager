package ui

import (
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
)

// The group picker is spawn's, under the names the root's surfaces and
// their tests spell it.
type (
	groupOption = spawn.GroupOption
	groupPicker = spawn.Picker
)

// groupPickerFor is the picker rows of the group tree, highlighting
// selectPath. Index 0 is always the root.
func (m *Model) groupPickerFor(selectPath string) groupPicker {
	groups, index := m.groupOptions(selectPath)
	return groupPicker{Groups: groups, GroupIndex: index}
}

// groupOptions is the picker's rows and the index of selectPath in them.
func (m *Model) groupOptions(selectPath string) ([]groupOption, int) {
	sessionGroups := make([]string, 0, len(m.workspace.sessions))
	for _, session := range m.workspace.sessions {
		sessionGroups = append(sessionGroups, session.Group)
	}
	paths := spawn.GroupClosure(m.workspace.groups, sessionGroups)
	for path := range paths {
		if m.groupEffectivelyArchived(path) {
			delete(paths, path)
		}
	}
	children := spawn.ChildIndex(paths, m.workspace.groups)

	options := []groupOption{{Path: "", Depth: 0}}
	var walk func(path string, depth int)
	walk = func(path string, depth int) {
		options = append(options, groupOption{Path: path, Depth: depth})
		for _, child := range children[path] {
			walk(child, depth+1)
		}
	}
	for _, root := range children[""] {
		walk(root, 1)
	}

	for i, opt := range options {
		if selectPath != "" && opt.Path == selectPath {
			return options, i
		}
	}
	return options, 0
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

func (m *Model) viewGroupPicker(p *groupPicker) string {
	return spawn.ViewGroupOptions(p.Groups, p.GroupIndex, m.spawnStyles())
}
