package ui

import (
	"strings"

	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uiapply "github.com/YoanWai/agent-manager/internal/ui/dialogs/apply"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	tea "github.com/charmbracelet/bubbletea"
)

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

// applyRenameOutcome mirrors the stages the worker committed into the loaded
// workspace, then closes the dialog that submitted this job, and only that
// one: a reopened or resubmitted card keeps its edits.
func (m *Model) applyRenameOutcome(job *uieffects.Job, result uiapply.RenameEffectResult, err error) {
	request := job.Request.(uiapply.RenameEffect)
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
	} else if s, ok := surfaceOf[*uidialogs.RenameSurface](m); ok && m.gens.dialog == request.Gen && s.Name() == request.Name &&
		(request.Kind != uiapply.Group || s.Dir() == request.DraftDir) {
		m.Pop()
	}
	m.rebuildRows()
}

// mirrorGroupRenamePartial mirrors only the stages that committed, so a
// failed default-dir or worktree write does not leak the requested value
// into the local inventory.
func (m *Model) mirrorGroupRenamePartial(request uiapply.RenameEffect, dir string, result uiapply.RenameEffectResult) {
	old, newPath := request.OldGroup, request.NewGroup
	m.renameGroupInventory(old, newPath)
	if result.PathSet {
		m.workspace.groupPaths[newPath] = dir
	}
	if result.WorktreeSet {
		m.workspace.groupWorktrees = setGroupChoice(m.workspace.groupWorktrees, newPath, request.Worktree)
	}
	m.queueRail(m.rail.RenameGroup(old, newPath), 0, true)
}

// renameGroupLocally rewrites the in-memory tree right away, so the frames
// between saving and the poller's next refresh already show the new name and
// path instead of flashing the stale ones.
func (m *Model) renameGroupLocally(old, newPath, dir, worktree, base string) {
	m.renameGroupInventory(old, newPath)
	m.workspace.groupPaths[newPath] = dir
	m.workspace.groupWorktrees = setGroupChoice(m.workspace.groupWorktrees, newPath, worktree)
	m.workspace.groupBases = setGroupChoice(m.workspace.groupBases, newPath, base)
	m.queueRail(m.rail.RenameGroup(old, newPath), 0, true)
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
