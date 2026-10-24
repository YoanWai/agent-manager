package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/YoanWai/agent-manager/internal/store"
	uilink "github.com/YoanWai/agent-manager/internal/ui/connections/link"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uiapply "github.com/YoanWai/agent-manager/internal/ui/dialogs/apply"
	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The small dialogs keep their state, keys and view in internal/ui/dialogs.
// These adapters seed them from the workspace, translate their outcomes
// into mode changes and effect requests, and draw the card chrome.

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
	remote *uilink.Call
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
		return m.queueRemote(*target.remote)
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
	tone := lipgloss.NewStyle().Foreground(uitheme.ColorAccent()).Bold(true)
	if destructive {
		tone = uitheme.ErrStyle()
	}
	body := uidialogs.ConfirmBody(question, consequence, uiframe.CardInnerWidth(width), tone, uitheme.MutedStyle())
	return m.CardSized(width, title, body, uidialogs.ConfirmHints(answer))
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
	var picker spawn.Picker
	if row.isGroup {
		move = uidialogs.MoveGroup(row.group)
		picker = m.groupPickerFor(uirail.ParentGroup(row.group))
		pruneMoveTargets(&picker, row.group)
	} else {
		move = uidialogs.MoveSession(row.sess.ID)
		picker = m.groupPickerFor(row.sess.Group)
		if m.isShell(row.sess.Tool) {
			m.appendAgentMoveTargets(&picker)
		}
	}
	m.gens.dialog++
	m.push(uidialogs.NewMoveSurface(moveRoot{m}, move, picker))
	m.clearErr()
}

func (m *Model) appendAgentMoveTargets(p *spawn.Picker) {
	selected := p.GroupPath()
	var options []spawn.GroupOption
	for _, opt := range p.Groups {
		options = append(options, opt)
		for _, sess := range m.workspace.sessions {
			if sess.Archived || m.isShell(sess.Tool) || sess.Group != opt.Path {
				continue
			}
			options = append(options, spawn.GroupOption{
				Path:      sess.Group,
				Depth:     opt.Depth + 1,
				SessionID: sess.ID,
				Name:      sess.Name,
			})
		}
	}
	p.Groups = options
	p.Reselect(selected, func(opt spawn.GroupOption) bool { return opt.SessionID == "" })
}

// pruneMoveTargets drops the moved group and its descendants from the
// picker: a group cannot land inside its own subtree.
func pruneMoveTargets(p *spawn.Picker, subtree string) {
	selected := p.GroupPath()
	options := make([]spawn.GroupOption, 0, len(p.Groups))
	for _, opt := range p.Groups {
		if opt.Path == subtree || strings.HasPrefix(opt.Path, subtree+"/") {
			continue
		}
		options = append(options, opt)
	}
	p.Groups = options
	p.Reselect(selected, func(spawn.GroupOption) bool { return true })
}

// placeMove runs the placement the picked row asks for on the effect lane;
// the card closes from the fenced follow-up. A move the dialog refuses
// closes it now.
func (m *Model) placeMove(move uidialogs.Move, p *spawn.Picker) tea.Cmd {
	pick := uidialogs.MovePick{Group: p.GroupPath()}
	if !move.IsGroup() {
		opt := p.Picked()
		pick = uidialogs.MovePick{Group: opt.Path, ParentID: opt.SessionID}
	}
	request, ok := move.Place(pick)
	if !ok {
		m.Pop()
		return nil
	}
	if request.IsGroup() {
		return m.enqueueMove(uirail.Mutation{Kind: uirail.MoveGroup, Path: request.GroupPath, Group: request.Group}, uiapply.MoveClose{IsGroup: true, Group: request.GroupPath, Parent: request.Group})
	}
	return m.enqueueMove(uirail.Mutation{Kind: uirail.PlaceSession, SessionID: request.SessionID, Group: request.Group, ParentID: request.ParentID}, uiapply.MoveClose{SessID: request.SessionID, OptPath: request.Group, OptSessID: request.ParentID})
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
	tool, ok := m.services.cfg.Tools[entry.sess.Tool]
	if !ok {
		m.reportErr(fmt.Sprintf("tool %s is no longer configured", entry.sess.Tool))
		return
	}
	if err := uiapply.ValidateForkSource(entry.sess.Tool, tool, entry.sess); err != nil {
		m.reportErr(err.Error())
		return
	}
	m.gens.dialog++
	m.push(uidialogs.NewForkSurface(forkRoot{m, entry.sess}, entry.sess.Name, entry.sess.Group, int(m.gens.dialog)))
	m.clearErr()
}

func (m *Model) viewFork(form *uidialogs.Fork) string {
	card := form.View(uidialogs.ForkStyles{
		BranchGlyph: m.glyphs().BranchGlyph,
		Value:       uitheme.ValueStyle(),
		Group:       lipgloss.NewStyle().Foreground(uitheme.ColorAccent2()),
		Field:       m.formField,
		Input:       uiframe.TextInputView,
	})
	return m.CardSized(m.cardWidth(), card.Title, card.Body, card.Hints)
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
	var surface *uidialogs.RenameSurface
	if entry.isGroup {
		dir := m.workspace.groupPaths[entry.group]
		if dir == "" {
			dir = m.capturedGroupDefaultDir(entry.group)
		}
		surface = uidialogs.NewRenameSurface(renameRoot{m}, uidialogs.RenameGroup(entry.group, dir, groupWorktreeIndex(m.workspace.groupWorktrees[entry.group])), m.workspace.groupBases[entry.group])
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
		surface = uidialogs.NewRenameSurface(renameRoot{m}, uidialogs.RenameSession(entry.sess.ID, entry.sess.Name, entry.sess.Tool, tools), "")
	}
	m.push(surface)
	m.clearErr()
}

// viewRenameGroupFields draws the group edit's path, worktree and base rows
// under the group's head in the detail column.
func (m *Model) viewRenameGroupFields(s *uidialogs.RenameSurface, group string, width int) string {
	view := uidialogs.GroupFieldsView{
		Width:     width,
		Label:     uitheme.LabelStyle(),
		Focused:   lipgloss.NewStyle().Foreground(uitheme.ColorAccent()),
		Subtle:    uitheme.SubtleStyle(),
		Value:     uitheme.ValueStyle(),
		Input:     uiframe.TextInputView,
		Worktrees: groupWorktreeOptions,
		Base:      uidialogs.BaseChoice(uitheme.SubtleStyle(), uitheme.ValueStyle(), s.Base, m.groupBase(uirail.ParentGroup(group))),
	}
	if s.Paths().Active() {
		view.Suggestions = func() string { return s.Paths().View(pathCompleteStyles(m.prefs.glyphMode)) }
	}
	return s.GroupFields(view)
}

// enqueueMove sends a move dialog's decision through the existing effect
// lane as a typed Rail mutation, with a chained follow-up that closes the
// dialog only if the generation it was submitted with is still open.
func (m *Model) enqueueMove(mut uirail.Mutation, close uiapply.MoveClose) tea.Cmd {
	m.gens.dialog++
	close.Gen = m.gens.dialog
	m.effects.NextChain++
	chain := m.effects.NextChain
	request := uirailpersist.Request{
		Mutation: mut,
		Dir:      m.workspace.groupPaths[mut.Path],
		Worktree: m.workspace.groupWorktrees[mut.Path],
		Base:     m.workspace.groupBases[mut.Path],
	}
	if mut.Kind == uirail.PlaceSession {
		request.Placement = m.capturePlacementPrecondition(mut)
	}
	m.enqueueEffect(request, chain, false)
	m.enqueueEffect(close, chain, false)
	return m.nextEffectCmd()
}

func (m *Model) capturePlacementPrecondition(mutation uirail.Mutation) uirailpersist.Precondition {
	precondition := uirailpersist.Precondition{Verify: true, TargetID: mutation.ParentID, TargetGroup: mutation.Group}
	for _, session := range m.workspace.sessions {
		switch session.ID {
		case mutation.SessionID:
			precondition.SourceGroup = session.Group
			precondition.SourceParentID = session.ParentID
		case mutation.ParentID:
			precondition.TargetGroup = session.Group
		}
	}
	// A newer move can be accepted while an earlier placement for the same
	// row is still serialized ahead of it. Validate against that lane's
	// promised destination, which is the state the worker will actually see.
	jobs := append([]*uieffects.Job{m.effects.Main.Active}, m.effects.Main.Pending...)
	for _, job := range jobs {
		if job == nil {
			continue
		}
		request, ok := job.Request.(uirailpersist.Request)
		if !ok || request.Mutation.Kind != uirail.PlaceSession || request.Mutation.SessionID != mutation.SessionID {
			continue
		}
		precondition.SourceGroup = request.Mutation.Group
		precondition.SourceParentID = request.Mutation.ParentID
	}
	return precondition
}

// applyMoveClose pops the move card the follow-up was chained to, when it is
// still the one on screen.
func (m *Model) applyMoveClose(request uiapply.MoveClose) {
	if _, open := surfaceOf[*uidialogs.MoveSurface](m); !open || m.gens.dialog != request.Gen {
		return
	}
	m.Pop()
	m.rebuildRows()
}
