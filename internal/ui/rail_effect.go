package ui

import (
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	uirailpersist "github.com/YoanWai/agent-manager/internal/ui/rail/persist"
	tea "github.com/charmbracelet/bubbletea"
	"slices"
)

type railRequest struct {
	mutation  uirail.Mutation
	dir       string
	worktree  string
	base      string
	placement uirailpersist.Precondition
}

func (railRequest) EffectRequest() {}

func (railRequest) effectName() string { return "rail change" }

func (value railRequest) copied() effectRequest {
	value.mutation.Collapsed = slices.Clone(value.mutation.Collapsed)
	value.mutation.HostCollapsed = slices.Clone(value.mutation.HostCollapsed)
	value.mutation.GroupSiblings = slices.Clone(value.mutation.GroupSiblings)
	return value
}

func (request railRequest) run(services effectServices) (uieffects.Result, error) {
	result, err := uirailpersist.NewWriter(services.store, services.driver).Apply(request.mutation, request.placement)
	return railEffectResult{result: result}, err
}

type railEffectResult struct {
	result uirailpersist.Result
}

func (railEffectResult) EffectResult() {}

func (result railEffectResult) apply(m *Model, job *uieffects.Job, err error) tea.Cmd {
	return m.applyRailEffect(job, result.result, err)
}
func (m *Model) queueRail(mutations []uirail.Mutation, chain uint64, first bool) {
	if len(mutations) == 0 {
		return
	}
	if first {
		// Prioritized snapshots describe current UI state. Older pending saves
		// must not overwrite them after their reveal, rename or move completes.
		for _, mutation := range mutations {
			if mutation.Kind != uirail.SaveCollapsed {
				continue
			}
			pending := m.effects.Main.Pending[:0]
			for _, job := range m.effects.Main.Pending {
				if request, ok := job.Request.(railRequest); ok && request.mutation.Kind == uirail.SaveCollapsed {
					continue
				}
				pending = append(pending, job)
			}
			m.effects.Main.Pending = pending
			break
		}
	}
	if chain == 0 {
		m.effects.NextChain++
		chain = m.effects.NextChain
	}
	for i := 0; i < len(mutations); i++ {
		index := i
		if first {
			index = len(mutations) - 1 - i
		}
		mutation := mutations[index]
		mutation.Collapsed = append([]string(nil), mutation.Collapsed...)
		mutation.HostCollapsed = append([]uirail.HostFold(nil), mutation.HostCollapsed...)
		mutation.GroupSiblings = append([]string(nil), mutation.GroupSiblings...)
		m.enqueueEffect(railRequest{mutation: mutation, dir: m.workspace.groupPaths[mutation.Path], worktree: m.workspace.groupWorktrees[mutation.Path], base: m.workspace.groupBases[mutation.Path]}, chain, first)
	}
}
func (m *Model) applyRailEffect(job *uieffects.Job, result uirailpersist.Result, err error) tea.Cmd {
	request := job.Request.(railRequest)
	mutation := request.mutation
	if err == nil {
		switch mutation.Kind {
		case uirail.SwapSession:
			m.swapSessionInventory(mutation.SessionID, mutation.TargetID)
		case uirail.SwapGroup:
			m.materializeGroupsLocal(mutation.GroupSiblings)
			m.swapGroupInventory(mutation.Path, mutation.TargetPath)
		case uirail.MoveGroup:
			m.renameGroupLocally(mutation.Path, result.NewPath, request.dir, request.worktree, request.base)
		}
		for _, sess := range result.Sessions {
			for i := range m.workspace.sessions {
				if m.workspace.sessions[i].ID == sess.ID {
					m.workspace.sessions[i].Group = sess.Group
					m.workspace.sessions[i].ParentID = sess.ParentID
				}
			}
		}
	} else {
		pending := m.effects.Main.Pending[:0]
		for _, each := range m.effects.Main.Pending {
			if each.Chain != job.Chain {
				pending = append(pending, each)
			}
		}
		m.effects.Main.Pending = pending
	}
	follow := m.rail.ApplyMutation(mutation, err)
	m.queueRail(follow.Mutations, job.Chain, true)
	follow.Mutations = nil
	_, cmd := m.applyRailDecision(follow)
	if result.Warning != nil {
		m.reportErr(result.Warning.Error())
	}
	m.rebuildRows()
	return cmd
}

func (m *Model) railMutationPending() bool {
	jobs := append([]*uieffects.Job(nil), m.effects.Main.Pending...)
	if m.effects.Main.Active != nil {
		jobs = append(jobs, m.effects.Main.Active)
	}
	for _, job := range jobs {
		if request, ok := job.Request.(railRequest); ok && request.mutation.Kind != uirail.SaveCollapsed {
			return true
		}
	}
	return false
}
