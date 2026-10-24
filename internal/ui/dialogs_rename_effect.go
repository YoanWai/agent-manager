package ui

import (
	"strings"

	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uiapply "github.com/YoanWai/agent-manager/internal/ui/dialogs/apply"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	uirailpersist "github.com/YoanWai/agent-manager/internal/ui/rail/persist"
	tea "github.com/charmbracelet/bubbletea"
)

// renameRequest carries what the rename worker needs; uiapply.Request holds
// the values and the package worker runs them.
type renameRequest struct{ uiapply.Request }

func (request renameRequest) EffectRequest() {}

func (request renameRequest) effectName() string { return "rename" }

func (value renameRequest) copied() effectRequest {
	value.Request = value.Copy()
	return value
}

func (request renameRequest) run(services effectServices) (uieffects.Result, error) {
	return services.runRename(request)
}

// renameEffectResult reports which durable stages committed, in order.
type renameEffectResult struct{ uiapply.Outcome }

func (renameEffectResult) EffectResult() {}

func (result renameEffectResult) apply(m *Model, job *uieffects.Job, err error) tea.Cmd {
	m.applyRenameEffect(job, result, err)
	return nil
}

// moveDialogClose is the chained follow-up that closes a move dialog only
// if the dialog it captured is still the open one.
type moveDialogClose struct {
	isGroup   bool
	gen       uint64
	sessID    string
	group     string
	parent    string
	optPath   string
	optSessID string
}

func (moveDialogClose) EffectRequest() {}

func (request moveDialogClose) copied() effectRequest { return request }

func (moveDialogClose) effectName() string { return "move" }

func (moveDialogClose) run(effectServices) (uieffects.Result, error) {
	return moveDialogCloseResult{}, nil
}

type moveDialogCloseResult struct{}

func (moveDialogCloseResult) EffectResult() {}

func (moveDialogCloseResult) apply(m *Model, job *uieffects.Job, err error) tea.Cmd {
	m.applyMoveDialogClose(job.Request.(moveDialogClose))
	return nil
}

// queueRename completes the rename dialog's request with the inventory it
// was captured against (the group's default-path fallbacks, or the session
// row and whether a tool change must first find it childless), fences it to
// a new dialog generation and runs it on the effect lane.
func (m *Model) queueRename(request renameRequest) tea.Cmd {
	if request.Kind == uiapply.Group {
		m.gens.dialog++
		request.DirFallbacks = m.groupDirCandidates(parentGroup(request.OldGroup))
		request.Gen = m.gens.dialog
		m.enqueueEffect(request, 0, false)
		return m.nextEffectCmd()
	}
	index := -1
	for i := range m.workspace.sessions {
		if m.workspace.sessions[i].ID == request.SessID {
			index = i
			break
		}
	}
	prevTool := ""
	if index >= 0 {
		prevTool = m.workspace.sessions[index].Tool
		request.Sess = m.workspace.sessions[index]
	}
	toolChanged := request.Tool != "" && request.Tool != prevTool
	m.gens.dialog++
	request.CheckNoChildren = toolChanged && m.isShell(request.Tool)
	request.Gen = m.gens.dialog
	m.enqueueEffect(request, 0, false)
	return m.nextEffectCmd()
}

func (s effectServices) runRename(request renameRequest) (uieffects.Result, error) {
	result, err := uiapply.Rename(s.dialogSources(), s.dialogPathProbe(), request.Request)
	return renameEffectResult{result}, err
}

func (m *Model) applyRenameEffect(job *uieffects.Job, result renameEffectResult, err error) {
	request := job.Request.(renameRequest)
	if request.Kind == uiapply.Group {
		dir := request.Dir
		if result.Dir != "" {
			dir = result.Dir
		}
		if result.GroupRenamed {
			if result.PathSet && result.WorktreeSet && result.BaseSet {
				m.renameGroupLocally(request.OldGroup, request.NewGroup, dir, request.Worktree, request.Base)
			} else {
				m.mirrorGroupRenamePartial(request, dir, result)
			}
		}
	} else {
		for i := range m.workspace.sessions {
			if m.workspace.sessions[i].ID != request.SessID {
				continue
			}
			if result.NameRenamed {
				m.workspace.sessions[i].Name = request.Name
			}
			if result.Branch != "" {
				m.workspace.sessions[i].WorktreeBranch = result.Branch
			}
			if result.ToolChanged {
				m.workspace.sessions[i].Tool = request.Tool
				m.workspace.sessions[i].AgentSessionID = ""
			}
		}
	}
	if result.Warning != nil && err == nil {
		m.reportErr(result.Warning.Error())
	}
	if err != nil {
		m.reportErr(err.Error())
	} else if s, ok := m.renaming(); ok && m.gens.dialog == request.Gen && s.Name() == request.Name &&
		(request.Kind != uiapply.Group || s.Dir() == request.DraftDir) {
		// Only the dialog that submitted this job closes; a reopened or
		// resubmitted card keeps its edits.
		m.Pop()
	}
	m.rebuildRows()
}

// mirrorGroupRenamePartial mirrors only the stages that committed, so a
// failed default-dir or worktree write does not leak the requested value
// into the local inventory.
func (m *Model) mirrorGroupRenamePartial(request renameRequest, dir string, result renameEffectResult) {
	old, newPath := request.OldGroup, request.NewGroup
	m.renameGroupInventory(old, newPath)
	if result.PathSet {
		m.workspace.groupPaths[newPath] = dir
	}
	if result.WorktreeSet {
		m.workspace.groupWorktrees = setGroupChoice(m.workspace.groupWorktrees, newPath, request.Worktree)
	}
	m.applyRailStateDecision(m.rail.RenameGroup(old, newPath))
}

// enqueueMove sends a move dialog's decision through the existing effect
// lane as a typed Rail mutation, with a chained follow-up that closes the
// dialog only if the generation it was submitted with is still open.
func (m *Model) enqueueMove(mut uirail.Mutation, close moveDialogClose) tea.Cmd {
	m.gens.dialog++
	close.gen = m.gens.dialog
	m.effects.NextChain++
	chain := m.effects.NextChain
	request := railRequest{
		mutation: mut,
		dir:      m.workspace.groupPaths[mut.Path],
		worktree: m.workspace.groupWorktrees[mut.Path],
		base:     m.workspace.groupBases[mut.Path],
	}
	if mut.Kind == uirail.PlaceSession {
		request.placement = m.capturePlacementPrecondition(mut)
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
		request, ok := job.Request.(railRequest)
		if !ok || request.mutation.Kind != uirail.PlaceSession || request.mutation.SessionID != mutation.SessionID {
			continue
		}
		precondition.SourceGroup = request.mutation.Group
		precondition.SourceParentID = request.mutation.ParentID
	}
	return precondition
}

func (m *Model) applyMoveDialogClose(request moveDialogClose) {
	if _, open := surfaceOf[*uidialogs.MoveSurface](m); !open || m.gens.dialog != request.gen {
		return
	}
	m.Pop()
	m.rebuildRows()
}

// renameGroupLocally rewrites the in-memory tree right away, so the
// frames between saving and the poller's next refresh already show the
// new name and path instead of flashing the stale ones.
func (m *Model) renameGroupLocally(old, newPath, dir, worktree, base string) {
	m.renameGroupInventory(old, newPath)
	m.workspace.groupPaths[newPath] = dir
	m.workspace.groupWorktrees = setGroupChoice(m.workspace.groupWorktrees, newPath, worktree)
	m.workspace.groupBases = setGroupChoice(m.workspace.groupBases, newPath, base)
	m.applyRailStateDecision(m.rail.RenameGroup(old, newPath))
}

// renameGroupInventory moves the in-memory groups and sessions under a
// renamed path; metadata maps are mirrored by the caller, which decides
// which stages committed.
func (m *Model) renameGroupInventory(old, newPath string) {
	moved := func(group string) (string, bool) {
		if group == old || strings.HasPrefix(group, old+"/") {
			return newPath + group[len(old):], true
		}
		return group, false
	}
	for i := range m.workspace.groups {
		m.workspace.groups[i], _ = moved(m.workspace.groups[i])
	}
	for i := range m.workspace.sessions {
		m.workspace.sessions[i].Group, _ = moved(m.workspace.sessions[i].Group)
	}
	groupPaths := make(map[string]string, len(m.workspace.groupPaths))
	for group, path := range m.workspace.groupPaths {
		group, _ = moved(group)
		groupPaths[group] = path
	}
	m.workspace.groupPaths = groupPaths
	m.workspace.groupWorktrees = movedChoices(m.workspace.groupWorktrees, moved)
	m.workspace.groupBases = movedChoices(m.workspace.groupBases, moved)
}

// movedChoices rekeys a per-group choice map for a renamed subtree.
func movedChoices(choices map[string]string, moved func(string) (string, bool)) map[string]string {
	out := make(map[string]string, len(choices))
	for group, choice := range choices {
		group, _ = moved(group)
		out[group] = choice
	}
	return out
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
