package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

const (
	dismissedNoticesSetting = "dismissed_notices"
	seenNoticesSetting      = "seen_notices"
)

// DismissNotice adds id to the dismissed-notice setting without losing a
// dismissal written by another manager.
func (s *Store) DismissNotice(id string) error {
	return s.addNoticeID(dismissedNoticesSetting, id, "dismiss notice")
}

// SeeNotice adds id to the seen-notice setting without losing a mark
// written by another manager.
func (s *Store) SeeNotice(id string) error {
	return s.addNoticeID(seenNoticesSetting, id, "see notice")
}

// addNoticeID merges id into the JSON array under key. The immediate
// transaction takes the write lock before the read, so every writer merges
// against its predecessor's committed array.
func (s *Store) addNoticeID(key, id, verb string) error {
	if id == "" {
		return fmt.Errorf("%s: id is empty", verb)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("%s: begin: %w", verb, err)
	}
	defer tx.Rollback()

	var raw string
	err = tx.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&raw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%s: read setting: %w", verb, err)
	}
	var ids []string
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &ids); err != nil {
			return fmt.Errorf("%s: decode setting: %w", verb, err)
		}
	}
	unique := make(map[string]struct{}, len(ids)+1)
	for _, existing := range ids {
		unique[existing] = struct{}{}
	}
	unique[id] = struct{}{}
	ids = ids[:0]
	for existing := range unique {
		ids = append(ids, existing)
	}
	slices.Sort(ids)
	encoded, err := json.Marshal(ids)
	if err != nil {
		return fmt.Errorf("%s: encode setting: %w", verb, err)
	}
	if _, err := tx.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, string(encoded),
	); err != nil {
		return fmt.Errorf("%s: write setting: %w", verb, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%s: commit: %w", verb, err)
	}
	return nil
}
