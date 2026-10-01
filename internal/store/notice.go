package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

const dismissedNoticesSetting = "dismissed_notices"

// DismissNotice adds id to the dismissed-notice setting without losing a
// dismissal written by another manager. The immediate transaction takes the
// write lock before the read, so every writer merges against its predecessor's
// committed JSON array.
func (s *Store) DismissNotice(id string) error {
	if id == "" {
		return errors.New("dismiss notice: id is empty")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("dismiss notice: begin: %w", err)
	}
	defer tx.Rollback()

	var raw string
	err = tx.QueryRow(`SELECT value FROM settings WHERE key = ?`, dismissedNoticesSetting).Scan(&raw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("dismiss notice: read setting: %w", err)
	}
	var ids []string
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &ids); err != nil {
			return fmt.Errorf("dismiss notice: decode setting: %w", err)
		}
	}
	seen := make(map[string]struct{}, len(ids)+1)
	for _, existing := range ids {
		seen[existing] = struct{}{}
	}
	seen[id] = struct{}{}
	ids = ids[:0]
	for existing := range seen {
		ids = append(ids, existing)
	}
	slices.Sort(ids)
	encoded, err := json.Marshal(ids)
	if err != nil {
		return fmt.Errorf("dismiss notice: encode setting: %w", err)
	}
	if _, err := tx.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		dismissedNoticesSetting, string(encoded),
	); err != nil {
		return fmt.Errorf("dismiss notice: write setting: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("dismiss notice: commit: %w", err)
	}
	return nil
}
