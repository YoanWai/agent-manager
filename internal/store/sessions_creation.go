package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// CreateActiveSession keeps a new live pane out of archived placements.
func (s *Store) CreateActiveSession(sess Session) error {
	return s.createSession(sess, "", true)
}

// CreateActiveSessionBeside validates the anchor's resolved placement in the write transaction.
func (s *Store) CreateActiveSessionBeside(sess Session, anchorID string) error {
	return s.createSession(sess, anchorID, true)
}

// ConversationFree refuses a conversation another row of the same CLI
// already holds, so two rows never resume it side by side.
func (s *Store) ConversationFree(tool, agentSessionID string) error {
	return conversationFree(s.db, tool, agentSessionID)
}

func conversationFree(db queryRower, tool, agentSessionID string) error {
	if agentSessionID == "" {
		return nil
	}
	var id, name string
	err := db.QueryRow(`SELECT id, name FROM sessions WHERE tool = ? AND agent_session_id = ?`, tool, agentSessionID).Scan(&id, &name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("conversation %s is already session %s (%s)", agentSessionID, id, name)
}

func validateActivePlacement(tx *sql.Tx, sess Session, anchorID string) error {
	if sess.Archived {
		return fmt.Errorf("session %q is archived; restore it in Agent Manager first", sess.Name)
	}
	for _, id := range []string{anchorID, sess.ParentID} {
		if id == "" {
			continue
		}
		var archived bool
		var name string
		if err := tx.QueryRow(`SELECT name, archived FROM sessions WHERE id = ?`, id).Scan(&name, &archived); err != nil {
			return err
		}
		if archived {
			return fmt.Errorf("session %q is archived; restore it in Agent Manager first", name)
		}
	}
	rows, err := tx.Query(`SELECT name FROM groups WHERE archived = 1`)
	if err != nil {
		return err
	}
	defer rows.Close()
	archived := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		archived[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if EffectivelyArchived(archived, sess.Group) {
		return fmt.Errorf("group %q is archived; restore it in Agent Manager first", sess.Group)
	}
	return nil
}
