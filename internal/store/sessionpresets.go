package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

type SessionPreset struct {
	Name         string
	Instructions string
}

func (s *Store) SessionPresets() ([]SessionPreset, error) {
	rows, err := s.db.Query(`SELECT name, instructions FROM session_presets ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	presets := make([]SessionPreset, 0)
	for rows.Next() {
		var preset SessionPreset
		if err := rows.Scan(&preset.Name, &preset.Instructions); err != nil {
			return nil, err
		}
		presets = append(presets, preset)
	}
	return presets, rows.Err()
}

func (s *Store) SessionPreset(name string) (SessionPreset, bool, error) {
	preset := SessionPreset{Name: name}
	err := s.db.QueryRow(`SELECT instructions FROM session_presets WHERE name = ?`, name).Scan(&preset.Instructions)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionPreset{}, false, nil
	}
	if err != nil {
		return SessionPreset{}, false, err
	}
	return preset, true, nil
}

func (s *Store) SaveSessionPreset(previousName string, preset SessionPreset) error {
	if !utf8.ValidString(preset.Name) || strings.ContainsFunc(preset.Name, unicode.IsControl) {
		return errors.New("session preset name must be valid UTF-8 without control characters")
	}
	preset.Name = strings.TrimSpace(preset.Name)
	if preset.Name == "" || utf8.RuneCountInString(preset.Name) > 60 {
		return errors.New("session preset name must contain 1 to 60 characters")
	}
	if !utf8.ValidString(preset.Instructions) || len(preset.Instructions) > 64*1024 || strings.TrimSpace(preset.Instructions) == "" {
		return errors.New("session preset instructions must contain non-whitespace text in valid UTF-8, at most 64 KiB")
	}
	if previousName == "" {
		_, err := s.db.Exec(`INSERT INTO session_presets (name, instructions) VALUES (?, ?)`, preset.Name, preset.Instructions)
		return err
	}
	result, err := s.db.Exec(`UPDATE session_presets SET name = ?, instructions = ? WHERE name = ?`, preset.Name, preset.Instructions, previousName)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("session preset %q no longer exists", previousName)
	}
	return nil
}

func (s *Store) DeleteSessionPreset(name string) (bool, error) {
	result, err := s.db.Exec(`DELETE FROM session_presets WHERE name = ?`, name)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected > 0, err
}
