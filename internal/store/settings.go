package store

import (
	"database/sql"
	"fmt"
	"slices"
	"strconv"
	"strings"
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

const CoordinationSetting = "coordination"

const coordinationProactive = "proactive"

// ProactiveCoordination reports whether agents delegate and coordinate on
// their own. The default waits for the user to ask.
func (s *Store) ProactiveCoordination() (bool, error) {
	value, err := s.Setting(CoordinationSetting)
	return value == coordinationProactive, err
}

func (s *Store) SetProactiveCoordination(proactive bool) error {
	value := "on-request"
	if proactive {
		value = coordinationProactive
	}
	return s.SetSetting(CoordinationSetting, value)
}

// The New Session form and a spawn with no caller both take their CLI from
// these, so the manager and the CLI read them here.
const (
	DefaultToolSetting = "default_tool"
	HiddenToolsSetting = "hidden_tools"
)

// DefaultTool is the CLI picked in Settings for new sessions, empty when none was.
func (s *Store) DefaultTool() (string, error) {
	return s.Setting(DefaultToolSetting)
}

func (s *Store) SetDefaultTool(name string) error {
	return s.SetSetting(DefaultToolSetting, name)
}

// HiddenTools is the set of CLIs turned off for new sessions in Settings.
func (s *Store) HiddenTools() (map[string]bool, error) {
	raw, err := s.Setting(HiddenToolsSetting)
	if err != nil {
		return nil, err
	}
	hidden := make(map[string]bool)
	for _, part := range strings.Split(raw, ",") {
		if name := strings.TrimSpace(part); name != "" {
			hidden[name] = true
		}
	}
	return hidden, nil
}

func (s *Store) SetHiddenTools(hidden map[string]bool) error {
	names := make([]string, 0, len(hidden))
	for name, on := range hidden {
		if on {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return s.SetSetting(HiddenToolsSetting, strings.Join(names, ","))
}

func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// SetSettingIfChanged writes value and reports whether the stored one
// differed, in one statement, so of several processes writing the same
// value exactly one learns it was new.
func (s *Store) SetSettingIfChanged(key, value string) (changed bool, err error) {
	res, err := s.db.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value
		 WHERE value IS NOT excluded.value`, key, value)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// Settings is every stored row, keyed by name.
func (s *Store) Settings() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	settings := map[string]string{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		settings[key] = value
	}
	return settings, rows.Err()
}
