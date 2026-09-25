package store

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

func (s *Store) Setting(key string) (string, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

// paneSizeSetting carries the window size the running manager pins its
// session panes to. The CLI and the MCP server open panes with no manager
// to ask, and a pane born at tmux's own 80x24 default stays there until
// something resizes it, so they read the box from here instead.
const paneSizeSetting = "pane_size"

func (s *Store) SetPaneSize(width, height int) error {
	return s.SetSetting(paneSizeSetting, fmt.Sprintf("%dx%d", width, height))
}

// PaneSize is the box the manager last pinned panes to, zeroes when no
// manager has recorded one yet, which leaves the size to tmux.
func (s *Store) PaneSize() (int, int, error) {
	value, err := s.Setting(paneSizeSetting)
	if err != nil || value == "" {
		return 0, 0, err
	}
	columns, rows, ok := strings.Cut(value, "x")
	width, widthErr := strconv.Atoi(columns)
	height, heightErr := strconv.Atoi(rows)
	if !ok || widthErr != nil || heightErr != nil {
		return 0, 0, fmt.Errorf("pane size %q in settings is not <columns>x<rows>", value)
	}
	return width, height, nil
}

func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}
