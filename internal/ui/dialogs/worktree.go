package dialogs

// WorktreeOptions are the picker states for a group's spawn-in-worktree
// choice; index 0 stores as "", so the group keeps inheriting.
var WorktreeOptions = []string{"inherit", "on", "off"}

// WorktreeValue is the value a picker index stores for a group.
func WorktreeValue(index int) string {
	if index > 0 && index < len(WorktreeOptions) {
		return WorktreeOptions[index]
	}
	return ""
}

// WorktreeIndex is the picker index of a stored value.
func WorktreeIndex(value string) int {
	for index, option := range WorktreeOptions {
		if option == value {
			return index
		}
	}
	return 0
}
