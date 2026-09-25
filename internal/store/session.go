package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/status"
	_ "modernc.org/sqlite"
)

type Session struct {
	ID           string
	Name         string
	Tool         string
	Cwd          string
	Group        string
	Status       string
	Archived     bool
	Acked        bool
	CreatedAt    time.Time
	LastStatusAt time.Time
	// AgentSessionID is the agent CLI's own conversation id (claude/grok/
	// gemini/pi session UUID, codex rollout id, opencode/hermes session id).
	// Revive resumes this exact conversation instead of the cwd's most recent one.
	AgentSessionID string
	// AgentLaunchedAt is when the agent process now in the pane started, which
	// restart and revive move forward while CreatedAt keeps marking the row's birth.
	// Zero for sessions that never relaunched, whose launch is CreatedAt.
	AgentLaunchedAt time.Time
	// RetiredAgentSessionID is the conversation a restart left behind, kept so
	// id capture never binds the fresh run back to the context it dropped.
	RetiredAgentSessionID string
	// RelaunchSnapshot is the tool's store state taken right before the
	// relaunch pane started: each conversation the directory held with its
	// last activity time. Recapture binds only conversations whose activity
	// outruns this, which tells the conversation the picker selected apart
	// from ones that merely predate the launch.
	RelaunchSnapshot map[string]int64
	// WorktreeRepo and WorktreeBranch are set for sessions running in a
	// worktree Agent Manager created. Forks share these values so the last
	// session to leave can clean up the worktree and its am/ branch.
	WorktreeRepo        string
	WorktreeBranch      string
	PendingInputs       []string
	PendingInputClaimed bool
	ParentID            string
	// LaunchPrompt is the prompt handed to the agent on its command line.
	// Pending input waits for it to show in the pane, because an agent
	// taking it clears the composer and anything pasted there.
	LaunchPrompt string
	// LastPrompt is the most recent text delivered to the session through
	// the manager: a quick bar send, a launch input the poller typed in, or
	// a queued message from another agent. The full screen row wears it,
	// falling back to LaunchPrompt for sessions nothing was sent to since.
	LastPrompt string
	// TmuxSocket is the tmux server the session's pane runs on. A manager
	// only derives status for the sessions on its own server: a pane it
	// cannot see belongs to another manager, not to a dead agent.
	TmuxSocket string
}

// LaunchTime is when the agent now in the pane started: the last restart
// or revive, or the row's creation for a session that never relaunched.
func (sess Session) LaunchTime() time.Time {
	if sess.AgentLaunchedAt.IsZero() {
		return sess.CreatedAt
	}
	return sess.AgentLaunchedAt
}

func (s *Store) CreateSession(sess Session) error {
	return s.createSession(sess, "")
}

// CreateSessionBeside reads the anchor inside the write transaction, so a
// placement that lands first decides where the new row goes rather than
// leaving it behind.
func (s *Store) CreateSessionBeside(sess Session, anchorID string) error {
	return s.createSession(sess, anchorID)
}

func (s *Store) createSession(sess Session, anchorID string) error {
	if sess.CreatedAt.IsZero() {
		sess.CreatedAt = time.Now()
	}
	if sess.LastStatusAt.IsZero() {
		sess.LastStatusAt = sess.CreatedAt
	}
	pendingInputs, err := encodePendingInputs(sess.PendingInputs)
	if err != nil {
		return err
	}
	sess.ParentID = strings.TrimSpace(sess.ParentID)
	anchorID = strings.TrimSpace(anchorID)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if anchorID != "" {
		var anchorGroup, anchorParent string
		err := tx.QueryRow(`SELECT group_name, parent_id FROM sessions WHERE id = ?`, anchorID).Scan(&anchorGroup, &anchorParent)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("session %s: %w", anchorID, err)
		}
		if err != nil {
			return err
		}
		sess.ParentID = anchorParent
		sess.Group = anchorGroup
	}
	if sess.ParentID != "" {
		parentGroup, err := validParent(tx, sess.ID, sess.ParentID)
		if err != nil {
			return err
		}
		sess.Group = parentGroup
	}
	_, err = tx.Exec(
		`INSERT INTO sessions (id, name, tool, cwd, group_name, status, archived, created_at, last_status_at, agent_session_id, worktree_repo, worktree_branch, pending_inputs, parent_id, launch_prompt, tmux_socket, sort_order)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
		         (SELECT COALESCE(MAX(sort_order)+1, 0) FROM sessions WHERE group_name = ? AND parent_id = ?))`,
		sess.ID, sess.Name, sess.Tool, sess.Cwd, sess.Group, sess.Status,
		boolToInt(sess.Archived), encodeTime(sess.CreatedAt), encodeTime(sess.LastStatusAt), sess.AgentSessionID,
		sess.WorktreeRepo, sess.WorktreeBranch, pendingInputs, sess.ParentID, sess.LaunchPrompt, sess.TmuxSocket,
		sess.Group, sess.ParentID,
	)
	if err != nil {
		return err
	}
	if sess.Group != "" {
		if _, err := tx.Exec(
			`INSERT INTO groups (name, sort_order)
			 VALUES (?, (SELECT COALESCE(MAX(sort_order)+1, 0) FROM groups))
			 ON CONFLICT(name) DO NOTHING`, sess.Group); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListSessions(includeArchived bool) ([]Session, error) {
	query := `SELECT id, name, tool, cwd, group_name, status, archived, acked, created_at, last_status_at, agent_session_id, worktree_repo, worktree_branch, agent_launched_at, retired_agent_session_id, relaunch_snapshot, pending_inputs, pending_claimed, parent_id, launch_prompt, last_prompt, tmux_socket
	          FROM sessions`
	if !includeArchived {
		query += ` WHERE archived = 0`
	}
	query += ` ORDER BY group_name, sort_order, created_at`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var sess Session
		var archived, acked, pendingClaimed int
		var created, lastStatus, agentLaunched int64
		var pendingInputs, relaunchSnapshot string
		if err := rows.Scan(&sess.ID, &sess.Name, &sess.Tool, &sess.Cwd,
			&sess.Group, &sess.Status, &archived, &acked, &created, &lastStatus,
			&sess.AgentSessionID, &sess.WorktreeRepo, &sess.WorktreeBranch,
			&agentLaunched, &sess.RetiredAgentSessionID, &relaunchSnapshot, &pendingInputs, &pendingClaimed, &sess.ParentID, &sess.LaunchPrompt, &sess.LastPrompt, &sess.TmuxSocket); err != nil {
			return nil, err
		}
		if err := decodeRelaunchSnapshot(relaunchSnapshot, &sess); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(pendingInputs), &sess.PendingInputs); err != nil {
			return nil, fmt.Errorf("decode pending inputs for session %s: %w", sess.ID, err)
		}
		sess.Archived = archived != 0
		sess.Acked = acked != 0
		sess.PendingInputClaimed = pendingClaimed != 0
		sess.CreatedAt = decodeTime(created)
		sess.LastStatusAt = decodeTime(lastStatus)
		sess.AgentLaunchedAt = decodeTime(agentLaunched)
		sessions = append(sessions, sess)
	}
	return sessions, rows.Err()
}

func (s *Store) Get(id string) (Session, error) {
	var sess Session
	var archived, acked, pendingClaimed int
	var created, lastStatus, agentLaunched int64
	var pendingInputs, relaunchSnapshot string
	err := s.db.QueryRow(
		`SELECT id, name, tool, cwd, group_name, status, archived, acked, created_at, last_status_at, agent_session_id, worktree_repo, worktree_branch, agent_launched_at, retired_agent_session_id, relaunch_snapshot, pending_inputs, pending_claimed, parent_id, launch_prompt, last_prompt, tmux_socket
		 FROM sessions WHERE id = ?`, id,
	).Scan(&sess.ID, &sess.Name, &sess.Tool, &sess.Cwd, &sess.Group,
		&sess.Status, &archived, &acked, &created, &lastStatus, &sess.AgentSessionID,
		&sess.WorktreeRepo, &sess.WorktreeBranch, &agentLaunched, &sess.RetiredAgentSessionID, &relaunchSnapshot, &pendingInputs, &pendingClaimed, &sess.ParentID, &sess.LaunchPrompt, &sess.LastPrompt, &sess.TmuxSocket)
	if err != nil {
		return Session{}, err
	}
	if err := decodeRelaunchSnapshot(relaunchSnapshot, &sess); err != nil {
		return Session{}, err
	}
	if err := json.Unmarshal([]byte(pendingInputs), &sess.PendingInputs); err != nil {
		return Session{}, fmt.Errorf("decode pending inputs for session %s: %w", sess.ID, err)
	}
	sess.Archived = archived != 0
	sess.Acked = acked != 0
	sess.PendingInputClaimed = pendingClaimed != 0
	sess.CreatedAt = decodeTime(created)
	sess.LastStatusAt = decodeTime(lastStatus)
	sess.AgentLaunchedAt = decodeTime(agentLaunched)
	return sess, nil
}

func (s *Store) Children(parentID string) ([]Session, error) {
	if parentID == "" {
		return nil, nil
	}
	sessions, err := s.ListSessions(true)
	if err != nil {
		return nil, err
	}
	var kids []Session
	for _, sess := range sessions {
		if sess.ParentID == parentID {
			kids = append(kids, sess)
		}
	}
	return kids, nil
}

func (s *Store) ClaimPendingInput(id, expected string) (bool, error) {
	encoded, inputs, claimed, err := s.pendingInputState(id)
	if err != nil {
		return false, err
	}
	if claimed || len(inputs) == 0 || inputs[0] != expected {
		return false, nil
	}
	res, err := s.db.Exec(
		`UPDATE sessions SET pending_claimed = 1
		 WHERE id = ? AND pending_inputs = ? AND pending_claimed = 0`, id, encoded)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected == 0 {
		return false, s.requireRowOrNoop(res, id)
	}
	return true, nil
}

func (s *Store) ConsumeClaimedPendingInput(id, expected string) (bool, error) {
	encoded, inputs, claimed, err := s.pendingInputState(id)
	if err != nil {
		return false, err
	}
	if !claimed || len(inputs) == 0 || inputs[0] != expected {
		return false, nil
	}
	remaining, err := encodePendingInputs(inputs[1:])
	if err != nil {
		return false, err
	}
	res, err := s.db.Exec(
		`UPDATE sessions SET pending_inputs = ?, pending_claimed = 0
		 WHERE id = ? AND pending_inputs = ? AND pending_claimed = 1`,
		remaining, id, encoded)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected == 0 {
		return false, s.requireRowOrNoop(res, id)
	}
	return true, nil
}

func (s *Store) pendingInputState(id string) (string, []string, bool, error) {
	var encoded string
	var claimed int
	if err := s.db.QueryRow(
		`SELECT pending_inputs, pending_claimed FROM sessions WHERE id = ?`, id,
	).Scan(&encoded, &claimed); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil, false, fmt.Errorf("session %s: %w", id, ErrSessionGone)
		}
		return "", nil, false, err
	}
	var inputs []string
	if err := json.Unmarshal([]byte(encoded), &inputs); err != nil {
		return "", nil, false, fmt.Errorf("decode pending inputs for session %s: %w", id, err)
	}
	return encoded, inputs, claimed != 0, nil
}

func encodePendingInputs(inputs []string) (string, error) {
	if len(inputs) == 0 {
		return "[]", nil
	}
	encoded, err := json.Marshal(inputs)
	return string(encoded), err
}

// decodeRelaunchSnapshot fills the session's snapshot map from the stored
// JSON. An empty column means the relaunch predates snapshot capture, which
// recapture treats as "nothing known" and refuses to guess from.
func decodeRelaunchSnapshot(encoded string, sess *Session) error {
	if encoded == "" {
		sess.RelaunchSnapshot = nil
		return nil
	}
	if err := json.Unmarshal([]byte(encoded), &sess.RelaunchSnapshot); err != nil {
		return fmt.Errorf("decode relaunch snapshot for session %s: %w", sess.ID, err)
	}
	return nil
}

// SetRelaunchSnapshot stores the tool-store state taken right before a
// relaunch, and clears it once the conversation has bound or the launch is
// over. Written before the pane starts, so it never races the launch itself.
func (s *Store) SetRelaunchSnapshot(id string, snapshot map[string]int64) error {
	encoded := ""
	if snapshot != nil {
		// An empty non-nil map is a real pre-launch state ("the store held
		// nothing"), so it persists as {} where nil clears the column.
		data, err := json.Marshal(snapshot)
		if err != nil {
			return err
		}
		encoded = string(data)
	}
	res, err := s.db.Exec(
		`UPDATE sessions SET relaunch_snapshot = ? WHERE id = ?`, encoded, id)
	if err != nil {
		return err
	}
	return requireRow(res, id)
}

func (s *Store) UpdateStatus(id, newStatus string) error {
	res, err := s.db.Exec(
		`UPDATE sessions SET status = ?, last_status_at = ? WHERE id = ?`,
		newStatus, encodeTime(time.Now()), id)
	if err != nil {
		return err
	}
	return requireRow(res, id)
}

// UpdateStatusOnSocket writes a status only while the row is still this
// manager's to speak for: claimed by this tmux server, or claimed by none.
// It reports whether the write landed, so a manager whose listing predates
// another one claiming the row leaves that row's status alone rather than
// announcing what it derived from a pane it cannot see.
func (s *Store) UpdateStatusOnSocket(id, newStatus, socket string) (bool, error) {
	res, err := s.db.Exec(
		`UPDATE sessions SET status = ?, last_status_at = ?
		 WHERE id = ? AND tmux_socket IN ('', ?)`,
		newStatus, encodeTime(time.Now()), id, socket)
	if err != nil {
		return false, err
	}
	changed, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return changed > 0, nil
}

// AcknowledgeFinished atomically marks a session idle and acked if its stored
// status is still finished. A newer status makes the operation a no-op.
func (s *Store) AcknowledgeFinished(id string) error {
	res, err := s.db.Exec(
		`UPDATE sessions SET status = ?, acked = 1, last_status_at = ? WHERE id = ? AND status = ?`,
		status.Idle, encodeTime(time.Now()), id, status.Finished)
	if err != nil {
		return err
	}
	return s.requireRowOrNoop(res, id)
}

// SetAcked marks whether the user has acknowledged the session's last
// finished turn; an acked session renders idle even while its pane still
// shows the finished turn.
func (s *Store) SetAcked(id string, acked bool) error {
	res, err := s.db.Exec(
		`UPDATE sessions SET acked = ? WHERE id = ?`, boolToInt(acked), id)
	if err != nil {
		return err
	}
	return requireRow(res, id)
}

// SetLastPrompt records the text a delivery just put in front of the
// session, which the full screen row shows beside its name.
func (s *Store) SetLastPrompt(id, prompt string) error {
	res, err := s.db.Exec(
		`UPDATE sessions SET last_prompt = ? WHERE id = ?`, prompt, id)
	if err != nil {
		return err
	}
	return requireRow(res, id)
}

// SetAgentSessionID records the agent CLI's own conversation id for a
// session, so a later revive resumes that exact conversation. Used both
// when launching a tool we assign the id to and when capturing the id a
// tool minted itself.
func (s *Store) SetAgentSessionID(id, agentSessionID string) error {
	res, err := s.db.Exec(
		`UPDATE sessions SET agent_session_id = ? WHERE id = ?`, agentSessionID, id)
	if err != nil {
		return err
	}
	return requireRow(res, id)
}

// BindAgentSessionID records a captured conversation id, but only while the
// session is still the launch the capture ran for: unbound, and launched at
// the moment the capturing pass read. It reports whether the write landed.
// Capture reads a tool's store from a snapshot and can take minutes, long
// enough for a restart to clear the id and move the launch on underneath it,
// and that stale answer names the conversation the restart just dropped.
func (s *Store) BindAgentSessionID(id, agentSessionID string, launchedAt time.Time) (bool, error) {
	// The snapshot clears in the same statement as the bind, so a relaunch
	// landing between the two can never have its fresh snapshot wiped by a
	// bind that answered the previous launch.
	res, err := s.db.Exec(
		`UPDATE sessions SET agent_session_id = ?, relaunch_snapshot = ''
		 WHERE id = ? AND agent_session_id = '' AND agent_launched_at = ?`,
		agentSessionID, id, encodeTime(launchedAt))
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

// RestartAgent rebinds a session to a fresh agent run: the conversation it
// was resuming is retired, the new one (empty until capture for tools that
// mint their own id) takes its place, and the launch clock moves to now.
func (s *Store) RestartAgent(id, agentSessionID string, launchedAt time.Time) error {
	res, err := s.db.Exec(
		`UPDATE sessions SET
			retired_agent_session_id = CASE WHEN agent_session_id != '' THEN agent_session_id ELSE retired_agent_session_id END,
			agent_session_id = ?,
			agent_launched_at = ?
		 WHERE id = ?`, agentSessionID, encodeTime(launchedAt), id)
	if err != nil {
		return err
	}
	return requireRow(res, id)
}

// SetAgentLaunchedAt records when the agent now in the pane started, without
// touching the conversation it is resuming.
func (s *Store) SetAgentLaunchedAt(id string, launchedAt time.Time) error {
	res, err := s.db.Exec(
		`UPDATE sessions SET agent_launched_at = ? WHERE id = ?`,
		encodeTime(launchedAt), id)
	if err != nil {
		return err
	}
	return requireRow(res, id)
}

// SetTmuxSocket records the tmux server a session's pane was found on,
// which is how a later manager tells a session it cannot drive from one
// whose agent has died.
func (s *Store) SetTmuxSocket(id, socket string) error {
	res, err := s.db.Exec(`UPDATE sessions SET tmux_socket = ? WHERE id = ?`, socket, id)
	if err != nil {
		return err
	}
	return requireRow(res, id)
}

// SetSnapshot stores the session's final pane capture, kept out of the
// Session struct so list queries never haul the blob.
func (s *Store) SetSnapshot(id, snapshot string) error {
	res, err := s.db.Exec(
		`UPDATE sessions SET snapshot = ? WHERE id = ?`, snapshot, id)
	if err != nil {
		return err
	}
	return requireRow(res, id)
}

func (s *Store) Snapshot(id string) (string, error) {
	var snapshot string
	err := s.db.QueryRow(`SELECT snapshot FROM sessions WHERE id = ?`, id).Scan(&snapshot)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return snapshot, err
}

func (s *Store) SetArchived(id string, archived bool) error {
	res, err := s.db.Exec(
		`UPDATE sessions SET archived = ? WHERE id = ?`, boolToInt(archived), id)
	if err != nil {
		return err
	}
	if err := requireRow(res, id); err != nil {
		return err
	}
	// Restoring a session out of an archived group must leave it with a live
	// home, so un-archive its group and every ancestor.
	if !archived {
		sess, err := s.Get(id)
		if err != nil {
			return err
		}
		return s.unarchiveAncestorGroups(sess.Group)
	}
	return nil
}

// Delete removes a session and the coordination state that only makes
// sense while it exists. One transaction, because a session row that
// outlives its own inbox strands every sender waiting on a receipt.
func (s *Store) Delete(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM review_targets WHERE session_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM review_bases WHERE session_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM review_scopes WHERE session_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM review_states WHERE session_id = ?`, id); err != nil {
		return err
	}
	// Session ids are recycled from a fresh UUID prefix, so a message left
	// pointing at a deleted id could be re-attached to a future session.
	if _, err := tx.Exec(`DELETE FROM session_inbox WHERE session_id = ? OR sender_id = ?`, id, id); err != nil {
		return err
	}
	// A claim outlives its holder as pending work rather than as a task
	// parked forever against a session that no longer exists.
	if err := releaseTasksOwnedBy(tx, id, time.Now()); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM file_reservations WHERE session_id = ?`, id); err != nil {
		return err
	}
	res, err := tx.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if err := requireRow(res, id); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteChild removes a session only while it still hangs under parentID.
// kill runs inside the same write transaction, which holds the database's
// writer lock, so a placement cannot slip between the ownership check and
// the delete, and a failed kill rolls the row back into place.
func (s *Store) DeleteChild(id, parentID string, kill func() error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE sessions SET parent_id = ? WHERE id = ? AND parent_id = ?`, parentID, id, parentID)
	if err != nil {
		return err
	}
	held, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if held == 0 {
		return fmt.Errorf("session %s is no longer nested under %s", id, parentID)
	}
	if err := kill(); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM review_targets WHERE session_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM review_bases WHERE session_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM review_scopes WHERE session_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM review_states WHERE session_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM sessions WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// SessionsInSubtree returns every session (archived included) whose group
// is the given path or any descendant of it.
func (s *Store) SessionsInSubtree(path string) ([]Session, error) {
	sessions, err := s.ListSessions(true)
	if err != nil {
		return nil, err
	}
	var matched []Session
	for _, sess := range sessions {
		if inSubtree(sess.Group, path) {
			matched = append(matched, sess)
		}
	}
	return matched, nil
}

// validParent reads the parent through the caller's transaction, so the row
// it approves cannot be deleted or nested before the write lands.
func validParent(tx *sql.Tx, id, parentID string) (string, error) {
	if parentID == id {
		return "", fmt.Errorf("session %s cannot be its own parent", id)
	}
	var group, grandparent string
	err := tx.QueryRow(`SELECT group_name, parent_id FROM sessions WHERE id = ?`, parentID).Scan(&group, &grandparent)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("parent %s: %w", parentID, err)
	}
	if err != nil {
		return "", err
	}
	if grandparent != "" {
		return "", fmt.Errorf("parent %s already has a parent", parentID)
	}
	return group, nil
}

func (s *Store) PlaceSession(id, group, parentID string) error {
	parentID = strings.TrimSpace(parentID)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if parentID != "" {
		parentGroup, err := validParent(tx, id, parentID)
		if err != nil {
			return err
		}
		group = parentGroup
	}
	if parentID != "" {
		var kids int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM sessions WHERE parent_id = ?`, id).Scan(&kids); err != nil {
			return err
		}
		if kids > 0 {
			return fmt.Errorf("session %s has terminals of its own; move them out first", id)
		}
	}
	res, err := tx.Exec(
		`UPDATE sessions SET group_name = ?, parent_id = ?,
		 sort_order = (SELECT COALESCE(MAX(sort_order)+1, 0) FROM sessions WHERE group_name = ? AND parent_id = ?)
		 WHERE id = ?`,
		group, parentID, group, parentID, id)
	if err != nil {
		return err
	}
	if err := requireRow(res, id); err != nil {
		return err
	}
	if parentID == "" {
		if _, err := tx.Exec(`UPDATE sessions SET group_name = ? WHERE parent_id = ?`, group, id); err != nil {
			return err
		}
	}
	if group != "" {
		if _, err := tx.Exec(
			`INSERT INTO groups (name, sort_order)
			 VALUES (?, (SELECT COALESCE(MAX(sort_order)+1, 0) FROM groups))
			 ON CONFLICT(name) DO NOTHING`, group); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) MoveSession(id, group string) error {
	return s.PlaceSession(id, group, "")
}

func (s *Store) RenameSession(id, name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("session name cannot be empty")
	}
	res, err := s.db.Exec(`UPDATE sessions SET name = ? WHERE id = ?`, name, id)
	if err != nil {
		return err
	}
	return requireRow(res, id)
}

func (s *Store) RenameSessionWorktreeBranch(id, branch string) error {
	res, err := s.db.Exec(`UPDATE sessions SET worktree_branch = ? WHERE id = ?`, branch, id)
	if err != nil {
		return err
	}
	return requireRow(res, id)
}

// UpdateTool changes which tool status rules and revive use for a session.
// Clears the captured agent conversation id: that id only makes sense for
// the tool that minted it, and a manual tool swap means the user swapped
// the process in the pane (e.g. quit opencode, ran grok). A no-op when the
// tool column already matches leaves the conversation id alone.
func (s *Store) UpdateTool(id, tool string) error {
	if strings.TrimSpace(tool) == "" {
		return fmt.Errorf("session tool cannot be empty")
	}
	res, err := s.db.Exec(
		`UPDATE sessions SET tool = ?, agent_session_id = '' WHERE id = ? AND tool != ?`,
		tool, id, tool)
	if err != nil {
		return err
	}
	return s.requireRowOrNoop(res, id)
}
