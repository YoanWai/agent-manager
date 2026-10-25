package store

import (
	"database/sql"
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
