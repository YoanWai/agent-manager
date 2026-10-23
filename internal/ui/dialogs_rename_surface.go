package ui

import (
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	tea "github.com/charmbracelet/bubbletea"
)

// renameSurface is the rename typed into its row. It paints no card: the
// rail and the detail column draw its fields in place, and the list holds
// still under it. It keeps what the root shares with the dialog: the path
// completion under the group's path field and the group base choice, which
// the root probes.
type renameSurface struct {
	uidialogs.Rename
	complete pathComplete
	base     string
}

// renameHost is what the row reaches on the root beyond the stack.
type renameHost interface {
	pop()
	reportErr(text string)
	applyPathSuggestion() tea.Cmd
	stepRenameBase(s *renameSurface, delta int) (string, tea.Cmd)
	queueRename(request renameRequest) tea.Cmd
}

func newRenameSurface(rename uidialogs.Rename, base string) *renameSurface {
	return &renameSurface{Rename: rename, base: base}
}

// renaming is the open rename, if one is on the stack.
func (m *Model) renaming() (*renameSurface, bool) { return surfaceOf[*renameSurface](m) }

// previewFeeds is whether the preview has a pane to feed: the bare list, a
// rename typed in a row, or focus. Any other card hides it.
func (m *Model) previewFeeds() bool {
	if m.mode == modeFocus {
		return true
	}
	_, renaming := m.renaming()
	return m.mode == modeList && (len(m.surfaces) == 0 || renaming)
}

// dialogPaths hands the rename and the group form the root's path completion.
type dialogPaths struct{ *pathComplete }

func (p dialogPaths) Active() bool        { return p.active() }
func (p dialogPaths) Chosen() bool        { return p.chosen }
func (p dialogPaths) Move(delta int) bool { return p.move(delta) }
func (p dialogPaths) Reset()              { p.reset() }

func (s *renameSurface) Key(h surfaceHost, msg tea.KeyMsg) (bool, tea.Cmd) {
	host := h.(renameHost)
	key := s.Rename.Key(msg, dialogPaths{&s.complete}, len(groupWorktreeOptions))
	switch key.Action {
	case uidialogs.RenameCancel:
		h.pop()
		return true, nil
	case uidialogs.RenameComplete:
		return true, host.applyPathSuggestion()
	case uidialogs.RenameStepBase:
		var cmd tea.Cmd
		s.base, cmd = host.stepRenameBase(s, key.Delta)
		return true, cmd
	case uidialogs.RenameInvalid:
		host.reportErr(key.Err)
		return true, nil
	case uidialogs.RenameSubmit:
		request := key.Request
		if request.Group {
			return true, host.queueRename(renameRequest{
				kind:     renameGroup,
				oldGroup: request.OldGroup,
				newGroup: request.NewGroup,
				rawDir:   request.Dir,
				draftDir: request.Dir,
				worktree: groupWorktreeValue(request.Worktree),
				base:     s.base,
				name:     request.Name,
			})
		}
		return true, host.queueRename(renameRequest{
			kind:   renameSession,
			sessID: request.SessionID,
			name:   request.Name,
			tool:   request.Tool,
		})
	}
	if key.Suggest {
		return true, tea.Batch(key.Cmd, s.complete.request(pathSuggestionRename, s.Dir(), systemPathSuggestionReader{}))
	}
	return true, key.Cmd
}

// Click and Wheel do nothing: the name is typed into its row and the list
// holds still.
func (s *renameSurface) Click(surfaceHost, int) tea.Cmd { return nil }

func (s *renameSurface) Wheel(surfaceHost, tea.KeyMsg) tea.Cmd { return nil }

// View leaves the frame as it is: the row and the detail column draw the
// fields themselves.
func (s *renameSurface) View(_ surfaceHost, below string) string { return below }

func (s *renameSurface) Footer() [][2]string { return s.Hints() }

func (s *renameSurface) Owner() inputOwner { return ownerManager }

// Dismissible: the name and path are a draft, so a stray click or ctrl+c
// keeps them; esc cancels. The row also holds the list still, so the mouse
// treats it as no card at all.
func (s *renameSurface) Dismissible() bool { return false }

func (s *renameSurface) Leave(surfaceHost) {}

func (s *renameSurface) pathTarget() pathSuggestionTarget { return pathSuggestionRename }

// dir is the path field of a group edit; a session rename has none.
func (s *renameSurface) dir() (string, bool) { return s.Dir(), s.IsGroup() }

func (s *renameSurface) takePath(path string) { s.SetDir(path) }

func (s *renameSurface) paths() *pathComplete { return &s.complete }
