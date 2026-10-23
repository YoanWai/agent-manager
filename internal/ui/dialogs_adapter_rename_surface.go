package ui

import (
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	"github.com/YoanWai/agent-manager/internal/ui/pathcomplete"
)

// previewFeeds is whether the preview has a pane to feed: the bare list, a
// rename typed in a row, or focus. Any other card hides it.
func (m *Model) previewFeeds() bool {
	if m.focused() {
		return true
	}
	_, renaming := m.renaming()
	return m.listClear() || renaming
}

// dialogPaths hands the group form the root's path completion.
type dialogPaths struct{ *pathcomplete.State }

func (p dialogPaths) Active() bool        { return p.State.Active() }
func (p dialogPaths) Chosen() bool        { return p.State.Chosen }
func (p dialogPaths) Move(delta int) bool { return p.State.Move(delta) }
func (p dialogPaths) Reset()              { p.State.Reset() }

func newRenameSurface(m *Model, rename uidialogs.Rename, base string) *uidialogs.RenameSurface {
	return uidialogs.NewRenameSurface(renameRoot{m}, rename, base)
}

// renaming is the open rename, if one is on the stack.
func (m *Model) renaming() (*uidialogs.RenameSurface, bool) {
	return surfaceOf[*uidialogs.RenameSurface](m)
}
