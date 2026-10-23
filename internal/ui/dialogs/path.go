package dialogs

import "strings"

// Group paths are slash-separated, the way the Rail spells them.

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
