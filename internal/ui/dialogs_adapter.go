package ui

import (
	"sort"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	uilink "github.com/YoanWai/agent-manager/internal/ui/connections/link"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uiapply "github.com/YoanWai/agent-manager/internal/ui/dialogs/apply"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	uigeometry "github.com/YoanWai/agent-manager/internal/ui/geometry"
	uiquick "github.com/YoanWai/agent-manager/internal/ui/quick"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	uirailpersist "github.com/YoanWai/agent-manager/internal/ui/rail/persist"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	uilaunch "github.com/YoanWai/agent-manager/internal/ui/spawn/launch"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The small dialogs keep their state, keys and view in internal/ui/dialogs.
// This adapter seeds them from the workspace, translates their outcomes into
// effect requests and mirrors the results back; the hosts in
// dialogs_host.go answer the reads and run the request a card holds.

func (m *Model) openConfirm(target confirmTarget) {
	m.push(uidialogs.NewConfirmSurface(confirmRoot{m, target}, target.prompt, m.prefs.glyphMode))
}

// listClear is the list with no card over it: what "mode is the list" meant
// while the cards were modes. The quick bar and a resize sit on the list
// without covering it.
func (m *Model) listClear() bool {
	if m.base != baseList {
		return false
	}
	for _, s := range m.surfaces {
		switch s.(type) {
		case *uiquick.Surface, *uigeometry.Surface:
		default:
			return false
		}
	}
	return true
}

// previewFeeds is whether the preview has a pane to feed: the bare list, a
// rename typed in a row, or focus. Any other card hides it.
func (m *Model) previewFeeds() bool {
	if m.focused() {
		return true
	}
	_, renaming := surfaceOf[*uidialogs.RenameSurface](m)
	return m.listClear() || renaming
}

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
		uidialogs.PruneTargets(&picker, row.group)
	} else {
		move = uidialogs.MoveSession(row.sess.ID)
		picker = m.groupPickerFor(row.sess.Group)
		if m.isShell(row.sess.Tool) {
			uidialogs.NestAgents(&picker, m.agentMoveTargets())
		}
	}
	m.gens.dialog++
	m.push(uidialogs.NewMoveSurface(moveRoot{m}, move, picker))
	m.clearErr()
}

// agentMoveTargets are the running agents a shell may be nested under.
func (m *Model) agentMoveTargets() []uidialogs.Agent {
	var agents []uidialogs.Agent
	for _, sess := range m.workspace.sessions {
		if sess.Archived || m.isShell(sess.Tool) {
			continue
		}
		agents = append(agents, uidialogs.Agent{ID: sess.ID, Group: sess.Group, Name: sess.Name})
	}
	return agents
}

// placeMove runs the placement the picked row asks for on the effect lane;
// the card closes from the fenced follow-up. A move the dialog refuses
// closes it now.
func (m *Model) placeMove(move uidialogs.Move, p *spawn.Picker) tea.Cmd {
	placement, ok := uidialogs.PlacementOf(move, p)
	if !ok {
		m.Pop()
		return nil
	}
	if placement.IsGroup() {
		return m.enqueueMove(
			uirail.Mutation{Kind: uirail.MoveGroup, Path: placement.GroupPath, Group: placement.Group},
			uiapply.MoveClose{IsGroup: true, Group: placement.GroupPath, Parent: placement.Group})
	}
	return m.enqueueMove(
		uirail.Mutation{Kind: uirail.PlaceSession, SessionID: placement.SessionID, Group: placement.Group, ParentID: placement.ParentID},
		uiapply.MoveClose{SessID: placement.SessionID, OptPath: placement.Group, OptSessID: placement.ParentID})
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
		m.reportErr("tool " + entry.sess.Tool + " is no longer configured")
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

// viewFork draws the fork card whose form the package holds.
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

// queueFork admits the fork dialog's request onto the effect lane once per
// dialog generation, minting the child id and the pane geometry it launches
// at.
func (m *Model) queueFork(request uiapply.ForkEffect) {
	for _, job := range append([]*uieffects.Job{m.effects.Main.Active}, m.effects.Main.Pending...) {
		if job == nil {
			continue
		}
		if pending, ok := job.Request.(uiapply.ForkEffect); ok && pending.Gen == request.Gen {
			m.reportErr("this fork is already in progress")
			return
		}
	}
	w, h := m.paneTargetSize()
	request.MintChildID()
	request.Pane = sessioncmd.PaneSize{Width: w, Height: h}
	m.enqueueEffect(request, 0, false)
}

// applyFork records the durable fork row the worker committed, then answers
// the card that asked: an open dialog closes, a stale one keeps its edits.
func (m *Model) applyFork(request uiapply.ForkEffect, result uiapply.ForkOutcome, err error) tea.Cmd {
	launched := result.Launched
	if launched.ID != "" {
		m.markFreshPane(launched.ID)
		m.ledger.lifecycle.RecordLaunch(launched.ID, time.Now())
		m.workspace.sessions = append(m.workspace.sessions, launched)
		// Forks start as starting, which attention excludes; clear so the
		// row the fork just created is on screen.
		m.rail.ClearStatusFilter()
		m.rebuildRows()
	}
	// A completion must not replace a newer dialog or hand its keyboard
	// outcome to a dialog the user has since opened, including one reopened
	// on the same source.
	card, stillOpen := surfaceOf[*uidialogs.ForkSurface](m)
	stillOpen = stillOpen && card.Gen() == request.Gen && card.Name() == request.Name
	if err == nil {
		if stillOpen {
			m.Pop()
			m.clearErr()
			m.focusSession(launched.ID)
		}
		return m.refreshCmd()
	}
	if !stillOpen {
		m.reportErr(err.Error())
		return nil
	}
	request.ChildID = result.ChildID
	if result.RecordedForkID != "" {
		request.KnownForkID = result.RecordedForkID
	}
	m.reportLaunchError(err)
	if fix, ok := m.launchHintFix(); ok {
		fix.Retry = request
	}
	return nil
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
		surface = uidialogs.NewRenameSurface(renameRoot{m}, uidialogs.RenameGroup(entry.group, dir, uidialogs.WorktreeIndex(m.workspace.groupWorktrees[entry.group])), m.workspace.groupBases[entry.group])
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
	// Each opening's reads outrank the last one's, so a scan a cancelled
	// rename asked for cannot replace this one's pick.
	surface.Paths().Generation = m.gens.dialog << 32
	m.push(surface)
	m.clearErr()
}

// queueRename completes the rename dialog's request with the inventory it
// was captured against (the group's default-path fallbacks, or the session
// row and whether a tool change must first find it childless), fences it to
// a new dialog generation and runs it on the effect lane.
func (m *Model) queueRename(request uiapply.RenameEffect) tea.Cmd {
	if request.Kind == uiapply.Group {
		m.gens.dialog++
		request.DirFallbacks = m.groupDirCandidates(uirail.ParentGroup(request.OldGroup))
		request.Gen = m.gens.dialog
		m.enqueueEffect(request, 0, false)
		return m.nextEffectCmd()
	}
	prevTool := ""
	for i := range m.workspace.sessions {
		if m.workspace.sessions[i].ID != request.SessID {
			continue
		}
		prevTool = m.workspace.sessions[i].Tool
		request.Sess = m.workspace.sessions[i]
		break
	}
	toolChanged := request.Tool != "" && request.Tool != prevTool
	m.gens.dialog++
	request.CheckNoChildren = toolChanged && m.isShell(request.Tool)
	request.Gen = m.gens.dialog
	m.enqueueEffect(request, 0, false)
	return m.nextEffectCmd()
}

// applyRenameOutcome mirrors the stages the worker committed into the loaded
// workspace, then closes the dialog that submitted this job, and only that
// one: a reopened or resubmitted card keeps its edits.
func (m *Model) applyRenameOutcome(job *uieffects.Job, result uiapply.Outcome, err error) {
	request := job.Request.(uiapply.RenameEffect)
	stages := result.Stages()
	inventory := uiapply.Inventory{
		Groups:         m.workspace.groups,
		Sessions:       m.workspace.sessions,
		GroupPaths:     m.workspace.groupPaths,
		GroupWorktrees: m.workspace.groupWorktrees,
		GroupBases:     m.workspace.groupBases,
	}
	if request.Kind == uiapply.Group {
		dir := request.Dir
		if result.Dir != "" {
			dir = result.Dir
		}
		if stages.GroupRenamed {
			// Rewriting the in-memory tree right away means the frames
			// between saving and the poller's refresh already show the new
			// name and path instead of flashing the stale ones.
			renamed := inventory.RenamedGroup(request.OldGroup, request.NewGroup, dir, request.Worktree, request.Base, stages)
			m.workspace.groups, m.workspace.sessions = renamed.Groups, renamed.Sessions
			m.workspace.groupPaths, m.workspace.groupWorktrees = renamed.GroupPaths, renamed.GroupWorktrees
			m.workspace.groupBases = renamed.GroupBases
			m.queueRail(m.rail.RenameGroup(request.OldGroup, request.NewGroup).Mutations, 0, true)
		}
	} else {
		m.workspace.sessions = inventory.RenamedSession(request.SessID, stages, request.Name, result.Branch, request.Tool)
	}
	if result.Warning != nil && err == nil {
		m.reportErr(result.Warning.Error())
	}
	if err != nil {
		m.reportErr(err.Error())
	} else if s, ok := surfaceOf[*uidialogs.RenameSurface](m); ok && m.gens.dialog == request.Gen && s.Name() == request.Name &&
		(request.Kind != uiapply.Group || s.Dir() == request.DraftDir) {
		m.Pop()
	}
	m.rebuildRows()
}

// renameGroupLocally rewrites the in-memory tree right away, so the frames
// between saving and the poller's next refresh already show the new name and
// path instead of flashing the stale ones.
func (m *Model) renameGroupLocally(old, newPath, dir, worktree, base string) {
	renamed := uiapply.Inventory{
		Groups:         m.workspace.groups,
		Sessions:       m.workspace.sessions,
		GroupPaths:     m.workspace.groupPaths,
		GroupWorktrees: m.workspace.groupWorktrees,
		GroupBases:     m.workspace.groupBases,
	}.RenamedGroup(old, newPath, dir, worktree, base,
		uiapply.Stages{GroupRenamed: true, PathSet: true, WorktreeSet: true, BaseSet: true})
	m.workspace.groups, m.workspace.sessions = renamed.Groups, renamed.Sessions
	m.workspace.groupPaths, m.workspace.groupWorktrees = renamed.GroupPaths, renamed.GroupWorktrees
	m.workspace.groupBases = renamed.GroupBases
	m.queueRail(m.rail.RenameGroup(old, newPath).Mutations, 0, true)
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
		Worktrees: uidialogs.WorktreeOptions,
		Base:      uidialogs.BaseChoice(uitheme.SubtleStyle(), uitheme.ValueStyle(), s.Base, m.groupBase(uirail.ParentGroup(group))),
	}
	if s.Paths().Active() {
		view.Suggestions = func() string { return s.Paths().View(pathCompleteStyles(m.prefs.glyphMode)) }
	}
	return s.GroupFields(view)
}

// openGroupForm opens the New Group card, seeded with the parent picker and
// what each parent lends a child.
func (m *Model) openGroupForm() {
	opts := uidialogs.FormOptions{
		Gen:       m.nextComposerGen(),
		Cols:      m.layout.width,
		Worktrees: uidialogs.WorktreeOptions,
		Styles:    m.spawnStyles(),
	}
	if target, ok := m.remoteTarget(); ok {
		opts.Remote = uidialogs.Remote{Host: target.host, Group: target.group}
		m.push(uidialogs.NewGroupFormSurface(groupRoot{m}, opts))
		m.clearErr()
		return
	}
	parents, at := m.groupOptions(m.contextGroup())
	lends := make(map[string]uidialogs.Lend, len(parents))
	for _, opt := range parents {
		lends[opt.Path] = uidialogs.Lend{Dir: m.capturedAncestorGroupDir(opt.Path), Base: m.groupBase(opt.Path)}
	}
	opts.Dir = m.capturedGroupDefaultDir(parents[at].Path)
	opts.Parents, opts.Parent, opts.Lends = parents, at, lends
	m.push(uidialogs.NewGroupFormSurface(groupRoot{m}, opts))
	m.clearErr()
}

// handleGroupFormSubmit runs what the form's submit asked: a remote call
// for a connection's group, otherwise the store write on the effect lane.
func (m *Model) handleGroupFormSubmit(msg uidialogs.GroupFormSubmitMsg, reader directoryPreflight) tea.Cmd {
	request := msg.Request
	m.clearErr()
	if target := msg.Remote; target.Host != "" {
		return m.queueRemote(uilink.Call{Op: uilink.CreateGroup, Host: target.Host, Name: request.Path,
			GroupDir: strings.TrimSpace(request.Dir), ComposerGen: request.Gen})
	}
	m.enqueueEffect(uilaunch.GroupRequest{
		Path: request.Path, Worktree: uidialogs.WorktreeValue(request.Worktree), Base: msg.Base, Gen: request.Gen,
		RawDir: request.Dir, Fallbacks: m.groupDirCandidates(request.Parent), DirReader: crossPreflight(reader),
		DraftName: request.Name, DraftDir: request.Dir,
	}, 0, false)
	return m.nextEffectCmd()
}

// handleGroupBaseAsk steps the open form's base through the repo's
// branches, probing the directory the group would save.
func (m *Model) handleGroupBaseAsk(msg uidialogs.GroupBaseAskMsg) tea.Cmd {
	s, ok := surfaceOf[*uidialogs.GroupFormSurface](m)
	if !ok || s.Gen() != msg.Gen {
		return nil
	}
	choice, cmd := m.stepGroupBase(uilaunch.BaseForm, uint64(msg.Gen), m.capturedAbsolutePath(s.Dir(), m.capturedGroupDefaultDir(s.Selected())), s.Base(), msg.Delta)
	*s.BaseRef() = choice
	return cmd
}

// dialogSources is what the fork and rename workers reach off the update
// path, captured when their request was queued.
func (s effectServices) dialogSources() uiapply.Sources {
	return uiapply.Sources{
		Store:     s.store,
		Git:       s.gitDrv,
		Config:    s.cfg,
		Panes:     s.driver,
		Lifecycle: s.lifecycle,
		Runner:    s.runner,
	}
}

// dialogPathProbe is the read-only path probe the dialog workers read with:
// the spawn effect's, which resolves a default path the way a group form
// does, plus the plain directory test a fork's source needs.
type dialogPathProbe struct{ systemDirectoryPreflight }

func (p dialogPathProbe) Resolve(raw string, fallbacks []string) (string, bool) {
	return p.resolve(raw, fallbacks)
}

func (p dialogPathProbe) ResolveDir(path string) bool { return uilaunch.IsDir(path) }

func (s effectServices) dialogPathProbe() uiapply.Preflight {
	return dialogPathProbe{systemDirectoryPreflight{git: s.gitDrv}}
}

// link hands the connection runtime the table it writes and the client it
// calls the hosts with.
func (s effectServices) link() uilink.Services {
	return uilink.Services{Store: s.store, Client: s.remote, Openers: s.store}
}

// dialogApply hands the dialog workers what they reach off the update path:
// their sources, and the read-only probe a rename and a fork resolve paths
// with.
func (s effectServices) dialogApply() uiapply.Services {
	return uiapply.Services{Sources: s.dialogSources(), Preflight: s.dialogPathProbe()}
}

// setGroupChoice records a group's own choice, "" dropping it to inherit.
func setGroupChoice(choices map[string]string, group, value string) map[string]string {
	if choices == nil {
		choices = map[string]string{}
	}
	if value == "" {
		delete(choices, group)
	} else {
		choices[group] = value
	}
	return choices
}
