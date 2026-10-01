package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/agentsession"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
)

// forkRequest freezes a fork dialog submission: the source's manager id,
// conversation and worktree ownership, the typed name, and the dialog
// generation it was submitted from. The worker re-reads the source by
// manager id to validate liveness; a source whose captured identity changed
// while the fork was queued is refused, and the fork targets the captured
// identity, not the replacement.
type forkRequest struct {
	childID     string
	source      store.Session
	name        string
	gen         int
	knownForkID string
	pane        sessioncmd.PaneSize
}

func (forkRequest) effectRequest() {}

// forkEffectResult is the durable outcome of a fork. launched is set once
// the child session is committed, even when the launch then fails at
// labeling, so the row applies before the error reports. recordedForkID is
// the fork the source's own store recorded once the fork keys landed; it
// survives a failed child launch so a retry reuses it instead of forking
// the source a second time.
type forkEffectResult struct {
	childID        string
	launched       store.Session
	recordedForkID string
}

func (forkEffectResult) effectResult() {}

func (m *Model) queueFork(source store.Session, name string) {
	for _, job := range append([]*effectJob{m.effects.active}, m.effects.pending...) {
		if job == nil {
			continue
		}
		if pending, ok := job.request.(forkRequest); ok && pending.gen == m.fork.gen {
			m.errBar.text = "this fork is already in progress"
			return
		}
	}
	w, h := m.paneTargetSize()
	m.enqueueEffect(forkRequest{childID: newID(), source: source, name: name, gen: m.fork.gen, pane: sessioncmd.PaneSize{Width: w, Height: h}}, 0, false)
}

// forkInSourceWait bounds how long the tool may take to record the fork once
// the keys land.
const forkInSourceWait = 10 * time.Second

func (s effectServices) runFork(request forkRequest) (effectResult, error) {
	result := forkEffectResult{childID: request.childID}
	if result.childID == "" {
		result.childID = newID()
	}
	source, err := s.store.Get(request.source.ID)
	if err != nil {
		return result, err
	}
	// The re-read validates liveness, not identity: the fork targets the
	// source the dialog confirmed. A restart while queued mints a new
	// conversation, and forking it would open the replacement.
	if source.Tool != request.source.Tool ||
		source.AgentSessionID != request.source.AgentSessionID ||
		source.Cwd != request.source.Cwd ||
		source.WorktreeRepo != request.source.WorktreeRepo ||
		source.WorktreeBranch != request.source.WorktreeBranch {
		return result, fmt.Errorf("%s changed while the fork was in the queue; reopen the fork dialog", source.Name)
	}
	tool, ok := s.cfg.Tools[request.source.Tool]
	if !ok {
		return result, fmt.Errorf("tool %s is no longer configured", request.source.Tool)
	}
	if err := validateForkSource(request.source.Tool, tool, request.source); err != nil {
		return result, err
	}
	if !isDir(request.source.Cwd) {
		return result, fmt.Errorf("working directory no longer exists: %s", request.source.Cwd)
	}
	agentID := ""
	var baseCommand string
	switch {
	case tool.ForkKeys != "":
		if request.knownForkID != "" {
			// A retry after a recorded fork: the keys already landed, and
			// typing them again would make the source fork a second time.
			known, ok := agentsession.Forks(tool.SessionStore, request.source.AgentSessionID)
			if !ok {
				return result, fmt.Errorf("cannot read the forks %s recorded of %s", source.Tool, source.Name)
			}
			if !known[request.knownForkID] {
				return result, fmt.Errorf("the fork %s %s recorded is gone; fork again", request.knownForkID, source.Name)
			}
			agentID = request.knownForkID
			baseCommand = expandForkCommand(tool.ForkCommand, source.AgentSessionID, agentID, request.name, "")
		} else {
			earlier, ok := agentsession.Forks(tool.SessionStore, source.AgentSessionID)
			if !ok {
				return result, fmt.Errorf("cannot read the forks %s recorded of %s", source.Tool, source.Name)
			}
			since := time.Now()
			if err := s.runner.TypeForkKeys(source, tool.ForkKeys); err != nil {
				return result, fmt.Errorf("fork key delivery outcome is uncertain; inspect the source before trying again: %w", err)
			}
			for deadline := since.Add(forkInSourceWait); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
				if id, ok := agentsession.ForkedFrom(tool.SessionStore, source.AgentSessionID, since, earlier); ok {
					agentID = id
					result.recordedForkID = id
					baseCommand = expandForkCommand(tool.ForkCommand, source.AgentSessionID, id, request.name, "")
					break
				}
			}
			if agentID == "" {
				return result, fmt.Errorf("%s did not record a fork of %s; the outcome is uncertain, inspect the source before trying again", source.Tool, source.Name)
			}
		}
	default:
		if strings.Contains(tool.ForkCommand, "{new_id}") {
			agentID = uuid.NewString()
		}
		sessionFile := ""
		if strings.Contains(tool.ForkCommand, "{session_file}") {
			resolved, err := forkSessionFileResolver(tool.SessionStore, source.AgentSessionID)
			if err != nil {
				return result, err
			}
			sessionFile = resolved
		}
		baseCommand = expandForkCommand(tool.ForkCommand, source.AgentSessionID, agentID, request.name, sessionFile)
	}
	forked := store.Session{
		ID:             result.childID,
		Name:           request.name,
		Tool:           request.source.Tool,
		Cwd:            request.source.Cwd,
		Group:          request.source.Group,
		Status:         status.Starting,
		AgentSessionID: agentID,
		WorktreeRepo:   request.source.WorktreeRepo,
		WorktreeBranch: request.source.WorktreeBranch,
	}
	launched, err := s.lifecycle.Launch(sessioncmd.LaunchRequest{
		Session:     forked,
		Tool:        tool,
		BaseCommand: baseCommand,
		Pane:        request.pane,
	})
	if err != nil {
		return result, err
	}
	result.launched = launched.Session
	return result, launched.LabelError
}

func (m *Model) applyForkEffect(request forkRequest, result forkEffectResult, err error) tea.Cmd {
	launched := result.launched
	if launched.ID != "" {
		m.markFreshPane(launched.ID)
		if m.ledger.launched == nil {
			m.ledger.launched = map[string]time.Time{}
		}
		m.ledger.launched[launched.ID] = time.Now()
		m.workspace.sessions = append(m.workspace.sessions, launched)
		// Forks start as starting, which attention excludes; clear so the
		// row the fork just created is on screen.
		m.rail.ClearStatusFilter()
		m.rebuildRows()
	}
	// A completion must not replace a newer dialog or hand its keyboard
	// outcome to a dialog the user has since opened, including one reopened
	// on the same source.
	stillOpen := m.mode == modeFork && m.fork.gen == request.gen && strings.ReplaceAll(strings.TrimSpace(m.fork.name.Value()), "/", "-") == request.name
	if err == nil {
		if stillOpen {
			m.mode = modeList
			m.errBar.text = ""
			m.focusSession(launched.ID)
		}
		return m.refreshCmd()
	}
	if !stillOpen {
		m.errBar.text = err.Error()
		return nil
	}
	request.childID = result.childID
	if result.recordedForkID != "" {
		request.knownForkID = result.recordedForkID
	}
	m.reportLaunchError(err, nil)
	if m.mode == modeLaunchHint {
		m.launchFix.effectRetry = request
	}
	return nil
}
