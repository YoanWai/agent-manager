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

// confirmDialog holds the lifecycle request a confirm asks about. The
// lifecycle adapters fill it and open the dialog; the question, its keys
// and its wording are uidialogs.Confirm.
type confirmDialog struct{ confirmTarget }

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

func (d *confirmDialog) prompt() uidialogs.Confirm {
	return uidialogs.Confirm{Action: d.action, Group: d.isGroup, Label: d.label}
}

func (m *Model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.confirm.prompt().Key(msg) {
	case uidialogs.ConfirmQuit:
		return m.requestQuit()
	case uidialogs.ConfirmCancel:
		m.confirm.confirmTarget = confirmTarget{}
		m.mode = modeList
	case uidialogs.ConfirmAccept:
		target := m.confirm.confirmTarget
		target.sessions = append(target.sessions[:0:0], target.sessions...)
		m.confirm.confirmTarget = confirmTarget{}
		m.mode = modeList
		switch {
		case target.remote != nil:
			return m, m.runRemote(*target.remote)
		case target.connection != "":
			m.removeConnection(target.connection)
		default:
			m.queueLifecycle(target, false, "")
		}
		return m, m.nextEffectCmd()
	}
	return m, nil
}

func (m *Model) viewConfirm() string {
	prompt := m.confirm.prompt()
	question, consequence := uidialogs.SplitLabel(prompt.Label)
	return m.confirmCard(prompt.Title(), question, consequence, prompt.Destructive(), prompt.Answer())
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

// moveDialog borrows the spawn form's group picker for its targets, which
// the root rebuilds and shapes when the dialog opens; the root runs the
// placement on the effect lane and closes the dialog from its fenced
// follow-up.
type moveDialog struct{ uidialogs.Move }

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
	if row.isGroup {
		m.move.Move = uidialogs.MoveGroup(row.group)
		m.rebuildGroupOptions(parentGroup(row.group))
		m.pruneMoveTargets(row.group)
	} else {
		m.move.Move = uidialogs.MoveSession(row.sess.ID)
		m.rebuildGroupOptions(row.sess.Group)
		if m.isShell(row.sess.Tool) {
			m.appendAgentMoveTargets()
		}
	}
	m.gens.dialog++
	m.mode = modeMove
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

func (m *Model) handleMoveKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := m.move.Key(msg)
	switch key.Action {
	case uidialogs.MoveCancel:
		m.mode = modeList
	case uidialogs.MoveCursor:
		return m, m.moveGroupCursor(key.Delta)
	case uidialogs.MoveSubmit:
		pick := uidialogs.MovePick{Group: m.selectedGroupPath()}
		if !m.move.IsGroup() {
			opt := m.pickedGroupOption()
			pick = uidialogs.MovePick{Group: opt.path, ParentID: opt.sessID}
		}
		request, ok := m.move.Place(pick)
		if !ok {
			m.mode = modeList
			return m, nil
		}
		if request.IsGroup() {
			return m, m.enqueueMove(uirail.Mutation{Kind: uirail.MoveGroup, Path: request.GroupPath, Group: request.Group}, moveDialogClose{isGroup: true, group: request.GroupPath, parent: request.Group})
		}
		return m, m.enqueueMove(uirail.Mutation{Kind: uirail.PlaceSession, SessionID: request.SessionID, Group: request.Group, ParentID: request.ParentID}, moveDialogClose{sessID: request.SessionID, optPath: request.Group, optSessID: request.ParentID})
	}
	return m, nil
}

func (m *Model) viewMove() string {
	return m.card(uidialogs.MoveTitle, m.viewGroupPicker(), uidialogs.MoveHints())
}

// forkDialog keeps the captured source row the fork request carries; the
// name field and the dialog generation are uidialogs.Fork.
type forkDialog struct {
	uidialogs.Fork
	source store.Session
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
	m.fork.Open(entry.sess.Name, entry.sess.Group)
	m.fork.source = entry.sess
	m.clearErr()
	m.mode = modeFork
}

func (m *Model) handleForkKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := m.fork.Key(msg)
	switch key.Action {
	case uidialogs.ForkCancel:
		m.mode = modeList
		m.clearErr()
	case uidialogs.ForkInvalid:
		m.reportErr(key.Err)
	case uidialogs.ForkSubmit:
		m.queueFork(forkRequest{source: m.fork.source, name: key.Name, gen: key.Gen})
		return m, m.nextEffectCmd()
	}
	return m, key.Cmd
}

func (m *Model) viewFork() string {
	return m.drawCard(m.fork.View(uidialogs.ForkStyles{
		BranchGlyph: m.glyphs().BranchGlyph,
		Value:       valueStyle(),
		Group:       lipgloss.NewStyle().Foreground(colorAccent2()),
		Field:       formField,
		Input:       textInputView,
	}))
}

// renameDialog keeps what the root shares with the rename: the path
// completion under its path field and the group base choice, which the
// root probes. The fields, focus and keys are uidialogs.Rename.
type renameDialog struct {
	uidialogs.Rename
	paths pathComplete
	base  string
}

// dialogPaths hands the rename and the group form the root's path completion.
type dialogPaths struct{ *pathComplete }

func (p dialogPaths) Active() bool        { return p.active() }
func (p dialogPaths) Chosen() bool        { return p.chosen }
func (p dialogPaths) Move(delta int) bool { return p.move(delta) }
func (p dialogPaths) Reset()              { p.reset() }

// stepRenameBase steps the group base choice, probing the default path the
// open group edit would save.
func (m *Model) stepRenameBase(current string, delta int) (string, tea.Cmd) {
	return m.stepGroupBase(groupBaseRename, m.gens.dialog, m.renameGroupDir(), current, delta)
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
	if entry.isGroup {
		dir := m.workspace.groupPaths[entry.group]
		if dir == "" {
			dir = m.capturedGroupDefaultDir(entry.group)
		}
		m.rename = renameDialog{
			Rename: uidialogs.RenameGroup(entry.group, dir, groupWorktreeIndex(m.workspace.groupWorktrees[entry.group])),
			paths:  m.rename.paths.fresh(),
			base:   m.workspace.groupBases[entry.group],
		}
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
		m.rename = renameDialog{
			Rename: uidialogs.RenameSession(entry.sess.ID, entry.sess.Name, entry.sess.Tool, tools),
			paths:  m.rename.paths.fresh(),
		}
	}
	m.mode = modeRename
	m.clearErr()
}

func (m *Model) handleRenameKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := m.rename.Key(msg, dialogPaths{&m.rename.paths}, len(groupWorktreeOptions))
	switch key.Action {
	case uidialogs.RenameCancel:
		m.mode = modeList
		return m, nil
	case uidialogs.RenameComplete:
		return m, m.applyPathSuggestion()
	case uidialogs.RenameStepBase:
		var cmd tea.Cmd
		m.rename.base, cmd = m.stepRenameBase(m.rename.base, key.Delta)
		return m, cmd
	case uidialogs.RenameInvalid:
		m.reportErr(key.Err)
		return m, nil
	case uidialogs.RenameSubmit:
		request := key.Request
		if request.Group {
			return m, m.queueRename(renameRequest{
				kind:     renameGroup,
				oldGroup: request.OldGroup,
				newGroup: request.NewGroup,
				rawDir:   request.Dir,
				draftDir: request.Dir,
				worktree: groupWorktreeValue(request.Worktree),
				base:     m.rename.base,
				name:     request.Name,
			})
		}
		return m, m.queueRename(renameRequest{
			kind:   renameSession,
			sessID: request.SessionID,
			name:   request.Name,
			tool:   request.Tool,
		})
	}
	if key.Suggest {
		return m, tea.Batch(key.Cmd, m.rename.paths.request(pathSuggestionRename, m.rename.Dir(), systemPathSuggestionReader{}))
	}
	return m, key.Cmd
}

// renameGroupDir is the default path the group edit would save, resolved
// the way the rename worker resolves it.
func (m *Model) renameGroupDir() string {
	return m.capturedAbsolutePath(m.rename.Dir(), m.capturedGroupDefaultDir(parentGroup(m.rename.Path())))
}

// viewRenameGroupFields draws the group edit's path, worktree and base rows
// under the group's head in the detail column.
func (m *Model) viewRenameGroupFields(group string, width int) string {
	view := uidialogs.GroupFieldsView{
		Width:     width,
		Label:     labelStyle(),
		Focused:   lipgloss.NewStyle().Foreground(colorAccent()),
		Subtle:    subtleStyle(),
		Value:     valueStyle(),
		Input:     textInputView,
		Worktrees: groupWorktreeOptions,
		Base:      groupBaseChoice(m.rename.base, m.groupBase(parentGroup(group))),
	}
	if m.rename.paths.active() {
		view.Suggestions = m.viewPathSuggestions
	}
	return m.rename.GroupFields(view)
}
