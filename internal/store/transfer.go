package store

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/YoanWai/agent-manager/internal/keybind"
	remotevalue "github.com/YoanWai/agent-manager/internal/remote/value"
)

// Configuration carries portable preferences without session or runtime state.
type Configuration struct {
	Version     int                  `json:"version"`
	Settings    map[string]string    `json:"settings,omitempty"`
	Presets     []SessionPreset      `json:"presets,omitempty"`
	Groups      []ConfigurationGroup `json:"groups,omitempty"`
	Connections []Connection         `json:"connections,omitempty"`
}

type ConfigurationGroup struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Worktree    string `json:"worktree"`
	Base        string `json:"base"`
	Environment string `json:"environment"`
}

type ConfigurationSections struct {
	Settings    bool
	Presets     bool
	Groups      bool
	Connections bool
}

func (s *Store) ExportConfiguration(sections ConfigurationSections, specs []SettingSpec) (Configuration, error) {
	result := Configuration{Version: 1}
	if sections.Settings {
		stored, err := s.Settings()
		if err != nil {
			return result, err
		}
		result.Settings = make(map[string]string)
		for _, spec := range specs {
			if value, ok := stored[spec.Key]; ok {
				result.Settings[spec.Key] = value
			}
		}
		for _, scope := range []string{"session", "list"} {
			if value, ok := stored[keysSetting(scope)]; ok {
				result.Settings[keysSetting(scope)] = value
			}
		}
	}
	if sections.Presets {
		presets, err := s.SessionPresets()
		if err != nil {
			return result, err
		}
		result.Presets = presets
	}
	if sections.Groups {
		groups, err := s.Groups()
		if err != nil {
			return result, err
		}
		for _, group := range groups {
			result.Groups = append(result.Groups, ConfigurationGroup{
				Name: group.Name, Path: group.Path, Worktree: group.Worktree, Base: group.Base, Environment: group.Environment,
			})
		}
	}
	if sections.Connections {
		connections, err := s.Connections()
		if err != nil {
			return result, err
		}
		result.Connections = connections
	}
	return result, nil
}

// ImportConfiguration merges matching keys and names in one transaction.
func (s *Store) ImportConfiguration(data Configuration, specs []SettingSpec) error {
	if err := data.validate(specs); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for key, value := range data.Settings {
		if _, err := tx.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)
   ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value); err != nil {
			return err
		}
	}
	for _, preset := range data.Presets {
		if _, err := tx.Exec(`INSERT INTO session_presets (name, instructions) VALUES (?, ?)
   ON CONFLICT(name) DO UPDATE SET instructions = excluded.instructions`, preset.Name, preset.Instructions); err != nil {
			return err
		}
	}
	for position, group := range data.Groups {
		if _, err := tx.Exec(`INSERT INTO groups (name, path, worktree, base, environment, sort_order) VALUES (?, ?, ?, ?, ?, ?)
   ON CONFLICT(name) DO UPDATE SET path = excluded.path, worktree = excluded.worktree,
   base = excluded.base, environment = excluded.environment, sort_order = excluded.sort_order`,
			group.Name, group.Path, group.Worktree, group.Base, group.Environment, position); err != nil {
			return err
		}
	}
	for position, connection := range data.Connections {
		if _, err := tx.Exec(`INSERT INTO connections (name, destination, binary, position, created_at) VALUES (?, ?, ?, ?, ?)
   ON CONFLICT(name) DO UPDATE SET destination = excluded.destination, binary = excluded.binary, position = excluded.position`,
			connection.Name, connection.Destination, connection.Binary, position, encodeTime(time.Now())); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (data Configuration) validate(specs []SettingSpec) error {
	if data.Version != 1 {
		return fmt.Errorf("unsupported configuration version %d", data.Version)
	}
	known := make(map[string]SettingSpec, len(specs))
	for _, spec := range specs {
		known[spec.Key] = spec
	}
	for key, value := range data.Settings {
		if key == keysSetting("session") || key == keysSetting("list") {
			if err := checkConfigurationKeys(strings.TrimPrefix(key, "keybindings."), value); err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
			continue
		}
		spec, ok := known[key]
		if !ok {
			return fmt.Errorf("unknown configuration setting %q", key)
		}
		if value != "" {
			if err := spec.Check(value); err != nil {
				return err
			}
		}
	}
	names := make(map[string]bool)
	for _, preset := range data.Presets {
		if err := checkSessionPreset(preset); err != nil {
			return fmt.Errorf("preset %q: %w", preset.Name, err)
		}
		if preset.Name != strings.TrimSpace(preset.Name) {
			return fmt.Errorf("preset name %q has surrounding whitespace", preset.Name)
		}
		if names[preset.Name] {
			return fmt.Errorf("duplicate preset %q", preset.Name)
		}
		names[preset.Name] = true
	}
	names = make(map[string]bool)
	for _, group := range data.Groups {
		if strings.TrimSpace(group.Name) == "" || strings.ContainsFunc(group.Name, unicode.IsControl) {
			return fmt.Errorf("invalid group name %q", group.Name)
		}
		if group.Worktree != "" && group.Worktree != "on" && group.Worktree != "off" {
			return fmt.Errorf("invalid worktree choice for group %q", group.Name)
		}
		if names[group.Name] {
			return fmt.Errorf("duplicate group %q", group.Name)
		}
		names[group.Name] = true
	}
	var connections []remotevalue.Connection
	for _, connection := range data.Connections {
		next := remotevalue.Connection(connection)
		if err := remotevalue.ValidateConnection(next, connections); err != nil {
			return err
		}
		connections = append(connections, next)
	}
	return nil
}

func checkConfigurationKeys(scope, raw string) error {
	if raw == "" {
		return nil
	}
	var stored map[string][]string
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return err
	}
	written := make(map[string]keybind.Binding, len(stored))
	for name, specs := range stored {
		var keys []keybind.Key
		for _, spec := range specs {
			key, err := keybind.Parse(spec)
			if err != nil {
				return err
			}
			keys = append(keys, key)
		}
		written[name] = keybind.Keys(keys...)
	}
	if scope == "session" {
		_, err := keybind.SessionTable(written)
		return err
	}
	_, err := keybind.ListTable(written)
	return err
}
