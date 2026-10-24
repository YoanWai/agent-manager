package ui

import (
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
)

// previewFeeds is whether the preview has a pane to feed: the bare list, a
// rename typed in a row, or focus. Any other card hides it.
func (m *Model) previewFeeds() bool {
	if m.focused() {
		return true
	}
	_, renaming := surfaceOf[*uidialogs.RenameSurface](m)
	return m.listClear() || renaming
}
