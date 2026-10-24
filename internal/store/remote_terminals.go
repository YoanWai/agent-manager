package store

import (
	"database/sql"
	"errors"
)

// SetRemoteTerminalOpener records which local session opened a terminal on
// an SSH connection. Every SSH call reaches the host as the same caller with
// no session, so only this side can tell one session's terminal from
// another's.
func (s *Store) SetRemoteTerminalOpener(host, terminalID, sessionID string) error {
	_, err := s.db.Exec(
		`INSERT INTO remote_terminals (host, terminal_id, session_id) VALUES (?, ?, ?)
		 ON CONFLICT(host, terminal_id) DO UPDATE SET session_id = excluded.session_id`,
		host, terminalID, sessionID,
	)
	return err
}

// RemoteTerminalOpener is empty for a terminal no local session opened.
func (s *Store) RemoteTerminalOpener(host, terminalID string) (string, error) {
	var sessionID string
	err := s.db.QueryRow(
		`SELECT session_id FROM remote_terminals WHERE host = ? AND terminal_id = ?`,
		host, terminalID).Scan(&sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return sessionID, nil
}

func (s *Store) DeleteRemoteTerminal(host, terminalID string) error {
	_, err := s.db.Exec(`DELETE FROM remote_terminals WHERE host = ? AND terminal_id = ?`, host, terminalID)
	return err
}
