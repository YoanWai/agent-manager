package store

import "fmt"

// SetLastPromptForLaunch records a typed draft only while the captured pane
// incarnation still owns this session row. The comparison and write are one
// SQLite statement, so a concurrent relaunch cannot inherit an older draft.
func (s *Store) SetLastPromptForLaunch(expected Session, prompt string) error {
	result, err := s.db.Exec(`UPDATE sessions SET last_prompt = ?
 WHERE id = ?
 AND (created_at = ? OR (created_at > 0 AND created_at < ? AND created_at = ?))
 AND (agent_launched_at = ? OR (agent_launched_at > 0 AND agent_launched_at < ? AND agent_launched_at = ?))
 AND tmux_socket = ?`,
		prompt, expected.ID,
		encodeTime(expected.CreatedAt), secondsCeiling, expected.CreatedAt.Unix(),
		encodeTime(expected.AgentLaunchedAt), secondsCeiling, expected.AgentLaunchedAt.Unix(), expected.TmuxSocket)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("session %s changed before its typed prompt was recorded", expected.ID)
	}
	return nil
}

// RecordQuickSendForLaunch records the metadata for a quick prompt only while
// the pane incarnation that accepted the send still owns the session row. The
// alert acknowledgement and prompt move together, so observers cannot see a
// cleared acknowledgement without its corresponding prompt (or vice versa).
func (s *Store) RecordQuickSendForLaunch(expected Session, prompt string) error {
	result, err := s.db.Exec(`UPDATE sessions SET acked = 0, last_prompt = ?
 WHERE id = ?
 AND (created_at = ? OR (created_at > 0 AND created_at < ? AND created_at = ?))
 AND (agent_launched_at = ? OR (agent_launched_at > 0 AND agent_launched_at < ? AND agent_launched_at = ?))
 AND tmux_socket = ?`,
		prompt, expected.ID,
		encodeTime(expected.CreatedAt), secondsCeiling, expected.CreatedAt.Unix(),
		encodeTime(expected.AgentLaunchedAt), secondsCeiling, expected.AgentLaunchedAt.Unix(), expected.TmuxSocket)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("session %s changed before its quick-send metadata was recorded", expected.ID)
	}
	return nil
}
