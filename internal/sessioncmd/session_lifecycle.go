package sessioncmd

import (
	"errors"
	"fmt"
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
)

// Kill stops a session's pane and leaves its row dead, keeping the last
// screen so the manager can still show it and a revive can resume the
// conversation it held.
func (s *Sessions) Kill(sessionID, targetID string) (Session, error) {
	runtime, err := s.open()
	if err != nil {
		return Session{}, err
	}
	defer runtime.Close()
	if _, err := runtime.caller(sessionID); err != nil {
		return Session{}, err
	}
	target, err := runtime.agent(targetID)
	if err != nil {
		return Session{}, err
	}
	if target.ID == sessionID {
		return Session{}, fmt.Errorf("a session kills itself with %s, which waits for this turn to end", runtime.words.KillSelf)
	}
	lifecycle, err := s.lifecycle(runtime, nil)
	if err != nil {
		return Session{}, err
	}
	killed, err := lifecycle.Kill(target)
	if err != nil {
		return Session{}, err
	}
	return runtime.sessionInfo(killed, false, false), nil
}

// Revive relaunches a dead session under its old id, keeping its name,
// group and history, and resuming the conversation it held where the tool
// can.
func (s *Sessions) Revive(sessionID, targetID string) (Session, error) {
	runtime, err := s.open()
	if err != nil {
		return Session{}, err
	}
	defer runtime.Close()
	if _, err := runtime.caller(sessionID); err != nil {
		return Session{}, err
	}
	target, err := runtime.agent(targetID)
	if err != nil {
		return Session{}, err
	}
	lifecycle, err := s.lifecycle(runtime, nil)
	if err != nil {
		return Session{}, err
	}
	revived, err := lifecycle.Revive(target, PaneSize{})
	if err != nil {
		return Session{}, err
	}
	return runtime.sessionInfo(revived.Session, true, false), nil
}

// Archive parks a session out of the active list, or restores it. A live
// pane keeps running; archiving only changes where the row is filed, and
// the last screen is captured first so an archived row still shows one.
func (s *Sessions) Archive(sessionID, targetID string, archived bool) (Session, error) {
	request := ArchiveRequest{CallerID: sessionID, TargetID: targetID, Archived: archived, Words: s.words}
	if s.archiveOwner != nil {
		return s.archiveOwner.Archive(request)
	}
	runtime, err := s.open()
	if err != nil {
		return Session{}, err
	}
	defer runtime.Close()
	lifecycle, err := s.lifecycle(runtime, nil)
	if err != nil {
		return Session{}, err
	}
	return lifecycle.SetArchivedForSession(request.CallerID, request.TargetID, request.Archived, request.Words)
}

// AfterTurn is what the calling session left for the manager to do to it
// once its turn ends.
type AfterTurn struct {
	Pending      string `json:"pending,omitempty" jsonschema:"archive or kill, which the manager runs once this turn ends; empty when nothing is pending"`
	Replaced     string `json:"replaced,omitempty" jsonschema:"the action this call replaced"`
	Canceled     string `json:"canceled,omitempty" jsonschema:"the action this call withdrew"`
	ManagerAwake bool   `json:"manager_awake" jsonschema:"whether Agent Manager is running; it carries the action out, so while it is closed the action waits for it to open"`
}

// EndAfterTurn asks the manager to archive or kill the calling session once
// the turn making this call ends. Ending it now would cut off the reply the
// agent is still writing, and the agent cannot outlive its own kill to
// finish the bookkeeping, so the manager does it.
func (s *Sessions) EndAfterTurn(sessionID, action string) (AfterTurn, error) {
	runtime, err := s.open()
	if err != nil {
		return AfterTurn{}, err
	}
	defer runtime.Close()
	self, err := runtime.agentCaller(sessionID)
	if err != nil {
		return AfterTurn{}, err
	}
	now := time.Now()
	if err := runtime.store.RequestAfterTurn(self.ID, action, now); err != nil {
		return AfterTurn{}, err
	}
	awake, err := runtime.managerAwake(now)
	if err != nil {
		return AfterTurn{}, err
	}
	result := AfterTurn{Pending: action, ManagerAwake: awake}
	if self.AfterTurn != action {
		result.Replaced = self.AfterTurn
	}
	return result, nil
}

// CancelAfterTurn withdraws whichever end the calling session has pending.
func (s *Sessions) CancelAfterTurn(sessionID string) (AfterTurn, error) {
	runtime, err := s.open()
	if err != nil {
		return AfterTurn{}, err
	}
	defer runtime.Close()
	self, err := runtime.agentCaller(sessionID)
	if err != nil {
		return AfterTurn{}, err
	}
	awake, err := runtime.managerAwake(time.Now())
	if err != nil {
		return AfterTurn{}, err
	}
	if self.AfterTurn == "" {
		return AfterTurn{ManagerAwake: awake}, nil
	}
	if err := runtime.store.ClearAfterTurn(self.ID); err != nil {
		return AfterTurn{}, err
	}
	return AfterTurn{Canceled: self.AfterTurn, ManagerAwake: awake}, nil
}

// agentCaller is the calling session when it is an agent, the only kind
// with turns to end.
func (r *runtime) agentCaller(sessionID string) (store.Session, error) {
	caller, err := r.caller(sessionID)
	if err != nil {
		return store.Session{}, err
	}
	if r.cfg.Tools[caller.Tool].Shell {
		return store.Session{}, errors.New("this session is a terminal, which has no turns to end")
	}
	return caller, nil
}
