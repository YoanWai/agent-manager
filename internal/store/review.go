package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

type ReviewState struct {
	Reviewed map[string]uint64 `json:"reviewed,omitempty"`
	Comments []ReviewComment   `json:"comments,omitempty"`
	Round    ReviewRound       `json:"round,omitempty"`
}

type ReviewComment struct {
	ID          string `json:"id,omitempty"`
	File        string `json:"file"`
	Line        int    `json:"line"`
	Deleted     bool   `json:"deleted,omitempty"`
	Excerpt     string `json:"excerpt,omitempty"`
	Text        string `json:"text"`
	ContentHash uint64 `json:"content_hash,omitempty"`
	Round       int    `json:"round,omitempty"`
	Scope       string `json:"scope,omitempty"`
	Point       int    `json:"point,omitempty"`
	Resolved    bool   `json:"resolved,omitempty"`
	Outdated    bool   `json:"outdated,omitempty"`
}

type ReviewRound struct {
	Number      int    `json:"number,omitempty"`
	Scope       string `json:"scope,omitempty"`
	Fingerprint uint64 `json:"fingerprint,omitempty"`
}

func (s *Store) ReviewState(sessionID, repoRoot string) (ReviewState, error) {
	var encoded string
	err := s.db.QueryRow(`SELECT state FROM review_states WHERE session_id = ? AND repo_root = ?`,
		sessionID, repoRoot).Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return ReviewState{}, nil
	}
	if err != nil {
		return ReviewState{}, err
	}
	var state ReviewState
	if err := json.Unmarshal([]byte(encoded), &state); err != nil {
		return ReviewState{}, err
	}
	return state, nil
}

func (s *Store) SetReviewState(sessionID, repoRoot string, state ReviewState) error {
	if len(state.Reviewed) == 0 && len(state.Comments) == 0 && state.Round.Number == 0 {
		_, err := s.db.Exec(`DELETE FROM review_states WHERE session_id = ? AND repo_root = ?`,
			sessionID, repoRoot)
		return err
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO review_states (session_id, repo_root, state) VALUES (?, ?, ?)
		ON CONFLICT(session_id, repo_root) DO UPDATE SET state = excluded.state`,
		sessionID, repoRoot, string(encoded))
	return err
}

// Comment IDs stay stable when edits move their line.
func (s *Store) SetReviewCommentHandled(sessionID, commentID string, handled bool) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	rows, err := tx.Query(`SELECT repo_root, state FROM review_states WHERE session_id = ?`, sessionID)
	if err != nil {
		return false, err
	}
	type changedState struct {
		repoRoot string
		state    ReviewState
	}
	var changed []changedState
	matches := 0
	for rows.Next() {
		var repoRoot, encoded string
		if err := rows.Scan(&repoRoot, &encoded); err != nil {
			rows.Close()
			return false, err
		}
		var state ReviewState
		if err := json.Unmarshal([]byte(encoded), &state); err != nil {
			rows.Close()
			return false, err
		}
		updated := false
		for i := range state.Comments {
			if state.Comments[i].ID == commentID && state.Comments[i].Round > 0 {
				state.Comments[i].Resolved = handled
				updated = true
				matches++
			}
		}
		if updated {
			changed = append(changed, changedState{repoRoot: repoRoot, state: state})
		}
	}
	if err := rows.Close(); err != nil {
		return false, err
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	if matches > 1 {
		return false, fmt.Errorf("review comment %s is ambiguous", commentID)
	}
	if len(changed) == 0 {
		return false, nil
	}
	for _, row := range changed {
		encoded, err := json.Marshal(row.state)
		if err != nil {
			return false, err
		}
		if _, err := tx.Exec(`UPDATE review_states SET state = ? WHERE session_id = ? AND repo_root = ?`,
			string(encoded), sessionID, row.repoRoot); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// Preserve statuses an agent may have changed after the UI's last snapshot.
func (s *Store) MergeReviewState(sessionID, repoRoot string, state ReviewState) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var existingJSON string
	err = tx.QueryRow(`SELECT state FROM review_states WHERE session_id = ? AND repo_root = ?`,
		sessionID, repoRoot).Scan(&existingJSON)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		var existing ReviewState
		if err := json.Unmarshal([]byte(existingJSON), &existing); err != nil {
			return err
		}
		handled := make(map[string]bool, len(existing.Comments))
		for _, comment := range existing.Comments {
			if comment.ID != "" {
				handled[comment.ID] = comment.Resolved
			}
		}
		for i := range state.Comments {
			if resolved, found := handled[state.Comments[i].ID]; found {
				state.Comments[i].Resolved = resolved
			}
		}
	}

	if len(state.Reviewed) == 0 && len(state.Comments) == 0 && state.Round.Number == 0 {
		if _, err := tx.Exec(`DELETE FROM review_states WHERE session_id = ? AND repo_root = ?`, sessionID, repoRoot); err != nil {
			return err
		}
	} else {
		encoded, err := json.Marshal(state)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO review_states (session_id, repo_root, state) VALUES (?, ?, ?)
			ON CONFLICT(session_id, repo_root) DO UPDATE SET state = excluded.state`,
			sessionID, repoRoot, string(encoded)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) SetReviewRepo(sessionID, repoRoot string) error {
	if repoRoot == "" {
		_, err := s.db.Exec(`DELETE FROM review_targets WHERE session_id = ?`, sessionID)
		return err
	}
	_, err := s.db.Exec(
		`INSERT INTO review_targets (session_id, repo_root) VALUES (?, ?)
		 ON CONFLICT(session_id) DO UPDATE SET repo_root = excluded.repo_root`,
		sessionID, repoRoot,
	)
	return err
}

func (s *Store) ReviewRepo(sessionID string) (string, error) {
	var root string
	err := s.db.QueryRow(`SELECT repo_root FROM review_targets WHERE session_id = ?`, sessionID).Scan(&root)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return root, nil
}

func (s *Store) SetReviewBase(sessionID, repoRoot, baseRef string) error {
	if baseRef == "" {
		_, err := s.db.Exec(
			`DELETE FROM review_bases WHERE session_id = ? AND repo_root = ?`,
			sessionID, repoRoot)
		return err
	}
	_, err := s.db.Exec(
		`INSERT INTO review_bases (session_id, repo_root, base_ref) VALUES (?, ?, ?)
		 ON CONFLICT(session_id, repo_root) DO UPDATE SET base_ref = excluded.base_ref`,
		sessionID, repoRoot, baseRef,
	)
	return err
}

func (s *Store) ReviewBase(sessionID, repoRoot string) (string, error) {
	var ref string
	err := s.db.QueryRow(
		`SELECT base_ref FROM review_bases WHERE session_id = ? AND repo_root = ?`,
		sessionID, repoRoot).Scan(&ref)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return ref, nil
}

func (s *Store) SetReviewScope(sessionID, scope string) error {
	if scope == "" {
		_, err := s.db.Exec(`DELETE FROM review_scopes WHERE session_id = ?`, sessionID)
		return err
	}
	_, err := s.db.Exec(
		`INSERT INTO review_scopes (session_id, scope) VALUES (?, ?)
		 ON CONFLICT(session_id) DO UPDATE SET scope = excluded.scope`,
		sessionID, scope,
	)
	return err
}

func (s *Store) ReviewScope(sessionID string) (string, error) {
	var scope string
	err := s.db.QueryRow(`SELECT scope FROM review_scopes WHERE session_id = ?`, sessionID).Scan(&scope)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return scope, nil
}
