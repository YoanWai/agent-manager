package ui

import (
	"encoding/json"
	"fmt"
	"github.com/YoanWai/agent-manager/internal/store"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	tea "github.com/charmbracelet/bubbletea"
	"sort"
)

type railRequest struct {
	mutation  uirail.Mutation
	dir       string
	worktree  string
	base      string
	placement placementPrecondition
}

type placementPrecondition struct {
	verify         bool
	sourceGroup    string
	sourceParentID string
	targetID       string
	targetGroup    string
}

func (railRequest) effectRequest() {}

type railEffectResult struct {
	sessions []store.Session
	warning  error
	newPath  string
}

func (railEffectResult) effectResult() {}
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
			pending := m.effects.pending[:0]
			for _, job := range m.effects.pending {
				if request, ok := job.request.(railRequest); ok && request.mutation.Kind == uirail.SaveCollapsed {
					continue
				}
				pending = append(pending, job)
			}
			m.effects.pending = pending
			break
		}
	}
	if chain == 0 {
		m.effects.nextChain++
		chain = m.effects.nextChain
	}
	for i := 0; i < len(mutations); i++ {
		index := i
		if first {
			index = len(mutations) - 1 - i
		}
		mutation := mutations[index]
		mutation.Collapsed = append([]string(nil), mutation.Collapsed...)
		mutation.GroupSiblings = append([]string(nil), mutation.GroupSiblings...)
		m.enqueueEffect(railRequest{mutation: mutation, dir: m.workspace.groupPaths[mutation.Path], worktree: m.workspace.groupWorktrees[mutation.Path], base: m.workspace.groupBases[mutation.Path]}, chain, first)
	}
}
func (s effectServices) runRail(request railRequest) (effectResult, error) {
	mutation := request.mutation
	result := railEffectResult{}
	if request.placement.verify && mutation.Kind == uirail.PlaceSession {
		source, err := s.store.Get(mutation.SessionID)
		if err != nil {
			return result, err
		}
		if source.Group != request.placement.sourceGroup || source.ParentID != request.placement.sourceParentID {
			return result, fmt.Errorf("move source changed before placement")
		}
		if request.placement.targetID != "" {
			target, err := s.store.Get(request.placement.targetID)
			if err != nil {
				return result, err
			}
			if target.Group != request.placement.targetGroup {
				return result, fmt.Errorf("move target changed before placement")
			}
		}
		if source.ParentID == mutation.ParentID && source.Group == mutation.Group {
			result.sessions = []store.Session{source}
			return result, nil
		}
	}
	var err error
	switch mutation.Kind {
	case uirail.SaveCollapsed:
		sort.Strings(mutation.Collapsed)
		var raw []byte
		raw, err = json.Marshal(mutation.Collapsed)
		if err == nil {
			err = s.store.SetSetting(collapsedSetting, string(raw))
		}
	case uirail.SwapSession:
		err = s.store.SwapSessionOrder(mutation.SessionID, mutation.TargetID)
	case uirail.SwapGroup:
		err = s.store.SwapGroupOrder(mutation.Path, mutation.TargetPath, mutation.GroupSiblings...)
	case uirail.PlaceSession:
		err = s.store.PlaceSession(mutation.SessionID, mutation.Group, mutation.ParentID)
	case uirail.PlaceSessionBefore:
		err = s.store.PlaceSessionBefore(mutation.SessionID, mutation.TargetID)
	case uirail.MoveGroup:
		err = s.store.MoveGroup(mutation.Path, mutation.Group)
		result.newPath = baseName(mutation.Path)
		if mutation.Group != "" {
			result.newPath = mutation.Group + "/" + result.newPath
		}
	}
	if err != nil {
		return result, err
	}
	switch mutation.Kind {
	case uirail.PlaceSession, uirail.PlaceSessionBefore:
		var sess store.Session
		sess, err = s.store.Get(mutation.SessionID)
		if err == nil {
			result.sessions = append(result.sessions, sess)
			var children []store.Session
			children, err = s.store.Children(sess.ID)
			result.sessions = append(result.sessions, children...)
		}
	case uirail.MoveGroup:
		result.sessions, err = s.store.SessionsInSubtree(result.newPath)
	}
	// Persistence already succeeded; a read/label failure is a warning.
	result.warning = err
	for _, sess := range result.sessions {
		if s.driver.Exists(sess.ID) {
			if labelErr := s.driver.SetLabel(sess.ID, sessionLabel(sess.Group, sess.Name)); labelErr != nil && result.warning == nil {
				result.warning = labelErr
			}
		}
	}
	return result, nil
}
func (m *Model) applyRailEffect(job *effectJob, result railEffectResult, err error) tea.Cmd {
	request := job.request.(railRequest)
	mutation := request.mutation
	if err == nil {
		switch mutation.Kind {
		case uirail.SwapSession:
			m.swapSessionInventory(mutation.SessionID, mutation.TargetID)
		case uirail.SwapGroup:
			m.materializeGroupsLocal(mutation.GroupSiblings)
			m.swapGroupInventory(mutation.Path, mutation.TargetPath)
		case uirail.MoveGroup:
			m.renameGroupLocally(mutation.Path, result.newPath, request.dir, request.worktree, request.base)
		}
		for _, sess := range result.sessions {
			for i := range m.workspace.sessions {
				if m.workspace.sessions[i].ID == sess.ID {
					m.workspace.sessions[i].Group = sess.Group
					m.workspace.sessions[i].ParentID = sess.ParentID
				}
			}
		}
	} else {
		pending := m.effects.pending[:0]
		for _, each := range m.effects.pending {
			if each.chain != job.chain {
				pending = append(pending, each)
			}
		}
		m.effects.pending = pending
	}
	follow := m.rail.ApplyMutation(mutation, err)
	m.queueRail(follow.Mutations, job.chain, true)
	follow.Mutations = nil
	_, cmd := m.applyRailDecision(follow)
	if result.warning != nil {
		m.errBar.text = result.warning.Error()
	}
	m.rebuildRows()
	return cmd
}

func (m *Model) railMutationPending() bool {
	jobs := append([]*effectJob(nil), m.effects.pending...)
	if m.effects.active != nil {
		jobs = append(jobs, m.effects.active)
	}
	for _, job := range jobs {
		if request, ok := job.request.(railRequest); ok && request.mutation.Kind != uirail.SaveCollapsed {
			return true
		}
	}
	return false
}
