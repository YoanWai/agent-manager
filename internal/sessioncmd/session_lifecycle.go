package sessioncmd

import (
	"errors"
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
		return Session{}, errors.New("a session cannot kill itself")
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
