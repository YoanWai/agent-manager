package sessioncmd

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

// FocusRequest is what the focus endpoint reads from a connection, and
// FocusAnswer what it writes back on the same one once the manager acted.
type FocusRequest struct {
	ID    string `json:"id"`
	Enter bool   `json:"enter"`
}

type FocusAnswer struct {
	Error string `json:"error,omitempty"`
}

// FocusAnswerWait bounds how long the manager has to act on a request. The
// endpoint drops a request it could not hand over in that time, so a caller
// told it timed out never sees the list move later.
const FocusAnswerWait = 5 * time.Second

var errNoManager = errors.New("no Agent Manager is running to bring the session forward")

// Focus brings a session forward in the running manager: the groups above
// it unfold and the cursor lands on its row, and with enter the session
// opens the way the enter key opens it. The manager answers with what it
// did. It acts as no session, so a script outside Agent Manager can run it.
func (s *Sessions) Focus(targetID string, enter bool) (Session, error) {
	runtime, err := s.open()
	if err != nil {
		return Session{}, err
	}
	defer runtime.store.Close()
	id := strings.TrimSpace(targetID)
	if id == "" {
		return Session{}, fmt.Errorf("session_id is empty; call %s to get one", runtime.words.ListSessions)
	}
	target, err := runtime.store.Get(id)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, fmt.Errorf("session %s does not exist; call %s for current ids", id, runtime.words.ListSessions)
	}
	if err != nil {
		return Session{}, err
	}
	if target.Archived {
		return Session{}, fmt.Errorf("session %s is archived, so the list has no row for it; restore it with %s first", id, runtime.words.Restore)
	}
	// Nothing listening, or a socket a manager left behind when it exited,
	// both mean no manager is there to answer.
	conn, err := net.Dial("unix", runtime.driver.FocusEndpoint())
	if err != nil {
		return Session{}, errNoManager
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(FocusAnswerWait + time.Second)); err != nil {
		return Session{}, err
	}
	if err := json.NewEncoder(conn).Encode(FocusRequest{ID: target.ID, Enter: enter}); err != nil {
		return Session{}, fmt.Errorf("send the request to Agent Manager: %w", err)
	}
	var answer FocusAnswer
	if err := json.NewDecoder(conn).Decode(&answer); err != nil {
		return Session{}, fmt.Errorf("Agent Manager did not answer: %w", err)
	}
	if answer.Error != "" {
		return Session{}, errors.New(answer.Error)
	}
	return runtime.sessionInfo(target, runtime.driver.Exists(target.ID), false), nil
}
