package dialogs

// WorktreeOptions are the picker states for a group's spawn-in-worktree
// choice; index 0 stores as "", so the group keeps inheriting.
var WorktreeOptions = []string{"inherit", "on", "off"}

func WorktreeValue(index int) string {
	if index > 0 && index < len(WorktreeOptions) {
		return WorktreeOptions[index]
	}
	return ""
}

func WorktreeIndex(value string) int {
	for index, option := range WorktreeOptions {
		if option == value {
			return index
		}
	}
	return 0
}
