package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/store"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The small dialogs keep their state, keys and view in internal/ui/dialogs.
// These adapters seed them from the workspace, translate their outcomes
// into mode changes and effect requests, and draw the card chrome.

// confirmTarget.action values; the zero value means delete.
const (
	actionDelete  = uidialogs.ConfirmDelete
	actionArchive = uidialogs.ConfirmArchive
	actionRestore = uidialogs.ConfirmRestore
	actionKill    = uidialogs.ConfirmKill
	actionRestart = uidialogs.ConfirmRestart
	actionRevive  = uidialogs.ConfirmRevive
	// actionRemoveConnection takes an SSH connection off the list; the
	// target names it and holds no sessions.
	actionRemoveConnection = uidialogs.ConfirmRemoveConnection
)

type confirmTarget struct {
	isGroup bool
	// archivedOnly marks a group delete issued from the archived view,
	// which clears the group's archive instead of the group itself.
	archivedOnly bool
	path         string
	label        string
	sessions     []store.Session
	action       uidialogs.ConfirmAction
	batch        bool
	selection    lifecycleSelection
	connection   string
	// remote is a call on an SSH row, run on the host once confirmed.
	remote *remoteRequest
}

type lifecycleSelectionKind uint8

const (
	lifecycleSelectionNone lifecycleSelectionKind = iota
	lifecycleSelectionSession
	lifecycleSelectionGroup
)

type lifecycleSelection struct {
	kind         lifecycleSelectionKind
	rootID       string
	archivedOnly bool
}

// runConfirmed is the answered yes: the remote call, the connection removal
// or the lifecycle request the card held.
func (m *Model) runConfirmed(target confirmTarget) tea.Cmd {
	switch {
	case target.remote != nil:
		return m.runRemote(*target.remote)
	case target.connection != "":
		m.removeConnection(target.connection)
	default:
		m.queueLifecycle(target, false, "")
	}
	return m.nextEffectCmd()
}

// confirmCard is the shared yes/no card, also drawn by Settings' reset.
func (m *Model) confirmCard(title, question, consequence string, destructive bool, answer string) string {
	width := m.cardWidth()
	tone := lipgloss.NewStyle().Foreground(colorAccent()).Bold(true)
	if destructive {
		tone = errStyle()
	}
	body := uidialogs.ConfirmBody(question, consequence, cardInnerWidth(width), tone, mutedStyle())
	return m.cardSized(width, title, body, uidialogs.ConfirmHints(answer))
}

func (m *Model) drawCard(card uidialogs.Card) string {
	return m.card(card.Title, card.Body, card.Hints)
}

// pickedGroupOption is the group picker's highlighted row.
func (m *Model) pickedGroupOption() groupOption {
	return m.form.groups[m.form.groupIndex]
}

// openMove captures the selected row and shapes the spawn form's group
// picker into the targets it may land in.
func (m *Model) openMove() {
	row, ok := m.selectedRow()
	if !ok {
		return
	}
	if row.isRoot() {
		m.reportErr("root is the top level, not a group to move")
		return
	}
	var move uidialogs.Move
	if row.isGroup {
		move = uidialogs.MoveGroup(row.group)
		m.rebuildGroupOptions(parentGroup(row.group))
		m.pruneMoveTargets(row.group)
	} else {
		move = uidialogs.MoveSession(row.sess.ID)
		m.rebuildGroupOptions(row.sess.Group)
		if m.isShell(row.sess.Tool) {
			m.appendAgentMoveTargets()
		}
	}
	m.gens.dialog++
	m.push(newMoveSurface(move))
	m.clearErr()
}

func (m *Model) appendAgentMoveTargets() {
	selected := m.form.groups[m.form.groupIndex].path
	var options []groupOption
	for _, opt := range m.form.groups {
		options = append(options, opt)
		for _, sess := range m.workspace.sessions {
			if sess.Archived || m.isShell(sess.Tool) || sess.Group != opt.path {
				continue
			}
			options = append(options, groupOption{
				path:   sess.Group,
				depth:  opt.depth + 1,
				sessID: sess.ID,
				name:   sess.Name,
			})
		}
	}
	m.form.groups = options
	m.form.groupIndex = 0
	for i, opt := range options {
		if opt.path == selected && opt.sessID == "" {
			m.form.groupIndex = i
			return
		}
	}
}

// pruneMoveTargets drops the moved group and its descendants from the
// picker: a group cannot land inside its own subtree.
func (m *Model) pruneMoveTargets(subtree string) {
	selected := m.form.groups[m.form.groupIndex].path
	options := make([]groupOption, 0, len(m.form.groups))
	for _, opt := range m.form.groups {
		if opt.path == subtree || strings.HasPrefix(opt.path, subtree+"/") {
			continue
		}
		options = append(options, opt)
	}
	m.form.groups = options
	m.form.groupIndex = 0
	for i, opt := range options {
		if opt.path == selected {
			m.form.groupIndex = i
			return
		}
	}
}

// placeMove runs the placement the picked row asks for on the effect lane;
// the card closes from the fenced follow-up. A move the dialog refuses
// closes it now.
func (m *Model) placeMove(move uidialogs.Move) tea.Cmd {
	pick := uidialogs.MovePick{Group: m.selectedGroupPath()}
	if !move.IsGroup() {
		opt := m.pickedGroupOption()
		pick = uidialogs.MovePick{Group: opt.path, ParentID: opt.sessID}
	}
	request, ok := move.Place(pick)
	if !ok {
		m.pop()
		return nil
	}
	if request.IsGroup() {
		return m.enqueueMove(uirail.Mutation{Kind: uirail.MoveGroup, Path: request.GroupPath, Group: request.Group}, moveDialogClose{isGroup: true, group: request.GroupPath, parent: request.Group})
	}
	return m.enqueueMove(uirail.Mutation{Kind: uirail.PlaceSession, SessionID: request.SessionID, Group: request.Group, ParentID: request.ParentID}, moveDialogClose{sessID: request.SessionID, optPath: request.Group, optSessID: request.ParentID})
}

// movePicker is the group picker's row count and cursor, for clicks.
func (m *Model) movePicker() (rows, cursor int) { return len(m.form.groups), m.form.groupIndex }

func (m *Model) viewMove() string {
	return m.card(uidialogs.MoveTitle(m.prefs.glyphMode), m.viewGroupPicker(), uidialogs.MoveHints())
}

// configuredTool looks a tool up in the loaded configuration.
func (m *Model) configuredTool(name string) (config.Tool, bool) {
	tool, ok := m.services.cfg.Tools[name]
	return tool, ok
}

func (m *Model) openFork() {
	entry, ok := m.selectedRow()
	if !ok {
		return
	}
	if entry.isGroup {
		m.reportErr("select a session to fork")
		return
	}
	tool, ok := m.configuredTool(entry.sess.Tool)
	if !ok {
		m.reportErr(fmt.Sprintf("tool %s is no longer configured", entry.sess.Tool))
		return
	}
	if err := validateForkSource(entry.sess.Tool, tool, entry.sess); err != nil {
		m.reportErr(err.Error())
		return
	}
	m.gens.dialog++
	m.push(newForkSurface(entry.sess, int(m.gens.dialog)))
	m.clearErr()
}

func (m *Model) viewFork(s *forkSurface) string {
	return m.drawCard(s.Fork.View(uidialogs.ForkStyles{
		BranchGlyph: m.glyphs().BranchGlyph,
		Value:       valueStyle(),
		Group:       lipgloss.NewStyle().Foreground(colorAccent2()),
		Field:       m.formField,
		Input:       textInputView,
	}))
}

// stepRenameBase steps the group base choice, probing the default path the
// open group edit would save.
func (m *Model) stepRenameBase(s *renameSurface, delta int) (string, tea.Cmd) {
	return m.stepGroupBase(groupBaseRename, m.gens.dialog, m.renameGroupDir(s), s.base, delta)
}

// openRename reads the selected row, the workspace's group choices and the
// configured tools to seed the dialog.
func (m *Model) openRename() {
	m.gens.dialog++
	entry, ok := m.selectedRow()
	if !ok {
		return
	}
	if entry.isRoot() {
		m.reportErr("root is the top level, not a group to rename")
		return
	}
	var surface *renameSurface
	if entry.isGroup {
		dir := m.workspace.groupPaths[entry.group]
		if dir == "" {
			dir = m.capturedGroupDefaultDir(entry.group)
		}
		surface = newRenameSurface(uidialogs.RenameGroup(entry.group, dir, groupWorktreeIndex(m.workspace.groupWorktrees[entry.group])), m.workspace.groupBases[entry.group])
	} else {
		tools := m.services.cfg.AgentToolNames()
		shells := []string{}
		for _, name := range m.services.cfg.ToolNames() {
			if m.services.cfg.Tools[name].Shell {
				shells = append(shells, name)
			}
		}
		sort.Strings(shells)
		tools = append(tools, shells...)
		surface = newRenameSurface(uidialogs.RenameSession(entry.sess.ID, entry.sess.Name, entry.sess.Tool, tools), "")
	}
	m.push(surface)
	m.clearErr()
}

// renameGroupDir is the default path the group edit would save, resolved
// the way the rename worker resolves it.
func (m *Model) renameGroupDir(s *renameSurface) string {
	return m.capturedAbsolutePath(s.Dir(), m.capturedGroupDefaultDir(parentGroup(s.Path())))
}

// viewRenameGroupFields draws the group edit's path, worktree and base rows
// under the group's head in the detail column.
func (m *Model) viewRenameGroupFields(s *renameSurface, group string, width int) string {
	view := uidialogs.GroupFieldsView{
		Width:     width,
		Label:     labelStyle(),
		Focused:   lipgloss.NewStyle().Foreground(colorAccent()),
		Subtle:    subtleStyle(),
		Value:     valueStyle(),
		Input:     textInputView,
		Worktrees: groupWorktreeOptions,
		Base:      groupBaseChoice(s.base, m.groupBase(parentGroup(group))),
	}
	if s.complete.active() {
		view.Suggestions = func() string { return s.complete.view(m.prefs.glyphMode) }
	}
	return s.GroupFields(view)
}
