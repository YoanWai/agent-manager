package spawn

import "github.com/YoanWai/agent-manager/internal/store"

// Options builds the picker's rows from the group tree: the root first, then
// every live group depth-first, with selectPath highlighted when it is
// there.
func Options(groups, sessionGroups []string, archived map[string]bool, selectPath string) ([]GroupOption, int) {
	paths := GroupClosure(groups, sessionGroups)
	for path := range paths {
		if store.EffectivelyArchived(archived, path) {
			delete(paths, path)
		}
	}
	children := ChildIndex(paths, groups)

	options := []GroupOption{{Path: "", Depth: 0}}
	var walk func(path string, depth int)
	walk = func(path string, depth int) {
		options = append(options, GroupOption{Path: path, Depth: depth})
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
