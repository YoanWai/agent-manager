package ui

import (
	"errors"
	"fmt"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
)

type renameKind int

const (
	renameGroup renameKind = iota
	renameSession
)

// renameRequest carries everything the rename worker needs so it never
// reads Model: the target, the requested values and a deep copy of the
// session row for the worktree-branch stage.
type renameRequest struct {
	kind            renameKind
	oldGroup        string
	newGroup        string
	dir             string
	rawDir          string
	dirFallbacks    []string
	draftDir        string
	worktree        string
	base            string
	sessID          string
	sess            store.Session
	name            string
	tool            string
	checkNoChildren bool
	gen             uint64
}

func (renameRequest) effectRequest() {}

// renameEffectResult reports which durable stages committed, in order, so
// the apply side can reconcile partial failures before surfacing the error.
type renameEffectResult struct {
	groupRenamed bool
	pathSet      bool
	worktreeSet  bool
	baseSet      bool
	nameRenamed  bool
	toolChanged  bool
	branch       string
	sessions     []store.Session
	warning      error
	dir          string
}

func (renameEffectResult) effectResult() {}

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

func (moveDialogClose) effectRequest() {}

type moveDialogCloseResult struct{}

func (moveDialogCloseResult) effectResult() {}

func (s effectServices) runRename(request renameRequest) (effectResult, error) {
	result := renameEffectResult{}
	if request.kind == renameGroup {
		if request.rawDir != "" || len(request.dirFallbacks) > 0 {
			resolved, ok := (systemDirectoryPreflight{git: s.gitDrv}).resolve(request.rawDir, request.dirFallbacks)
			result.dir = resolved
			if !ok {
				return result, fmt.Errorf("default path does not exist: %s", resolved)
			}
			request.dir = resolved
		} else {
			result.dir = request.dir
		}
		var err error
		if err = s.store.RenameGroup(request.oldGroup, request.newGroup); err != nil {
			return result, err
		}
		result.groupRenamed = true
		// CreateGroup upserts, so it doubles as the default-path setter.
		if err = s.store.CreateGroup(request.newGroup, request.dir); err != nil {
			return result, err
		}
		result.pathSet = true
		if err = s.store.SetGroupWorktree(request.newGroup, request.worktree); err != nil {
			return result, err
		}
		result.worktreeSet = true
		if err = s.store.SetGroupBase(request.newGroup, request.base); err != nil {
			return result, err
		}
		result.baseSet = true
		sessions, err := s.store.SessionsInSubtree(request.newGroup)
		if err != nil {
			return result, err
		}
		result.sessions = sessions
		result.warning = s.relabelSessions(sessions)
		return result, nil
	}
	if request.checkNoChildren {
		children, err := s.store.Children(request.sessID)
		if err != nil {
			return result, err
		}
		if len(children) > 0 {
			return result, errors.New("move its terminals first")
		}
	}
	sess := request.sess
	// The branch changes before the name is stored, so a name git cannot
	// give it leaves the rename card open instead of splitting them apart.
	if sess.WorktreeRepo != "" && sess.WorktreeBranch != "" {
		if err := sessioncmd.RenameWorktreeBranch(s.gitDrv, s.store, &sess, request.name); err != nil {
			return result, err
		}
		if sess.WorktreeBranch != request.sess.WorktreeBranch {
			result.branch = sess.WorktreeBranch
		}
	}
	var err error
	if err = s.store.RenameSession(request.sessID, request.name); err != nil {
		return result, err
	}
	result.nameRenamed = true
	if request.tool != "" && request.tool != request.sess.Tool {
		if err = s.store.UpdateTool(request.sessID, request.tool); err != nil {
			return result, err
		}
		result.toolChanged = true
	}
	updated, err := s.store.Get(request.sessID)
	if err != nil {
		return result, err
	}
	result.sessions = []store.Session{updated}
	result.warning = s.relabelSessions(result.sessions)
	return result, nil
}

func (m *Model) applyRenameEffect(job *effectJob, result renameEffectResult, err error) {
	request := job.request.(renameRequest)
	if request.kind == renameGroup {
		dir := request.dir
		if result.dir != "" {
			dir = result.dir
		}
		if result.groupRenamed {
			if result.pathSet && result.worktreeSet && result.baseSet {
				m.renameGroupLocally(request.oldGroup, request.newGroup, dir, request.worktree, request.base)
			} else {
				m.mirrorGroupRenamePartial(request, dir, result)
			}
		}
	} else {
		for i := range m.workspace.sessions {
			if m.workspace.sessions[i].ID != request.sessID {
				continue
			}
			if result.nameRenamed {
				m.workspace.sessions[i].Name = request.name
			}
			if result.branch != "" {
				m.workspace.sessions[i].WorktreeBranch = result.branch
			}
			if result.toolChanged {
				m.workspace.sessions[i].Tool = request.tool
				m.workspace.sessions[i].AgentSessionID = ""
			}
		}
	}
	if result.warning != nil && err == nil {
		m.reportErr(result.warning.Error())
	}
	if err != nil {
		m.reportErr(err.Error())
	} else if m.mode == modeRename && m.gens.dialog == request.gen && strings.ReplaceAll(strings.TrimSpace(m.rename.input.Value()), "/", "-") == request.name &&
		(request.kind != renameGroup || m.rename.dir.Value() == request.draftDir) {
		// Only the dialog that submitted this job closes; a reopened or
		// resubmitted card keeps its edits.
		m.mode = modeList
	}
	m.rebuildRows()
}

// mirrorGroupRenamePartial mirrors only the stages that committed, so a
// failed default-dir or worktree write does not leak the requested value
// into the local inventory.
func (m *Model) mirrorGroupRenamePartial(request renameRequest, dir string, result renameEffectResult) {
	old, newPath := request.oldGroup, request.newGroup
	m.renameGroupInventory(old, newPath)
	if result.pathSet {
		m.workspace.groupPaths[newPath] = dir
	}
	if result.worktreeSet {
		m.workspace.groupWorktrees = setGroupChoice(m.workspace.groupWorktrees, newPath, request.worktree)
	}
	m.applyRailStateDecision(m.rail.RenameGroup(old, newPath))
}

// enqueueMove sends a move dialog's decision through the existing effect
// lane as a typed Rail mutation, with a chained follow-up that closes the
// dialog only if the generation it was submitted with is still open.
func (m *Model) enqueueMove(mut uirail.Mutation, close moveDialogClose) tea.Cmd {
	m.gens.dialog++
	close.gen = m.gens.dialog
	m.effects.nextChain++
	chain := m.effects.nextChain
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

func (m *Model) capturePlacementPrecondition(mutation uirail.Mutation) placementPrecondition {
	precondition := placementPrecondition{verify: true, targetID: mutation.ParentID, targetGroup: mutation.Group}
	for _, session := range m.workspace.sessions {
		switch session.ID {
		case mutation.SessionID:
			precondition.sourceGroup = session.Group
			precondition.sourceParentID = session.ParentID
		case mutation.ParentID:
			precondition.targetGroup = session.Group
		}
	}
	// A newer move can be accepted while an earlier placement for the same
	// row is still serialized ahead of it. Validate against that lane's
	// promised destination, which is the state the worker will actually see.
	jobs := append([]*effectJob{m.effects.active}, m.effects.pending...)
	for _, job := range jobs {
		if job == nil {
			continue
		}
		request, ok := job.request.(railRequest)
		if !ok || request.mutation.Kind != uirail.PlaceSession || request.mutation.SessionID != mutation.SessionID {
			continue
		}
		precondition.sourceGroup = request.mutation.Group
		precondition.sourceParentID = request.mutation.ParentID
	}
	return precondition
}

func (m *Model) applyMoveDialogClose(request moveDialogClose) {
	if m.mode != modeMove || m.gens.dialog != request.gen {
		return
	}
	m.mode = modeList
	m.rebuildRows()
}

// relabelSessions refreshes tmux status-bar labels from durable rows.
// Label failures remain warnings after committed placement.
func (s effectServices) relabelSessions(sessions []store.Session) error {
	var first error
	for _, sess := range sessions {
		if !s.driver.Exists(sess.ID) {
			continue
		}
		if err := s.driver.SetLabel(sess.ID, sessionLabel(sess.Group, sess.Name)); err != nil && first == nil {
			first = err
		}
	}
	return first
}
