package store

import (
	"database/sql"
	"errors"
)

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
