package store

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/keybind"
)

const (
	editorSetting = "editor"
	// configImportSetting marks that dir's config.toml was looked at, so
	// the file is read once and a key changed since is never overwritten.
	configImportSetting      = "config_import"
	configImportErrorSetting = "config_import_error"
)

func keysSetting(scope string) string {
	return "keybindings." + scope
}

// SessionKeys is the table the manager, the CLI and the MCP server all
// install on the tmux server, so they read it from the one place.
func (s *Store) SessionKeys() (keybind.Table, error) {
	written, err := s.writtenKeys(keybind.DefaultSession())
	if err != nil {
		return keybind.Table{}, err
	}
	return keybind.SessionTable(written)
}

func (s *Store) ListKeys() (keybind.Table, error) {
	written, err := s.writtenKeys(keybind.DefaultList())
	if err != nil {
		return keybind.Table{}, err
	}
	return keybind.ListTable(written)
}

// SetKeys stores the actions that differ from their shipped default, so an
// action nobody moved follows the default a later release gives it. A name
// it does not hold was stored by a release with more actions, and stays
// for that release.
func (s *Store) SetKeys(keys keybind.Table) error {
	if err := keys.Validate(); err != nil {
		return err
	}
	stored, err := s.storedKeys(keys.Scope())
	if err != nil {
		return err
	}
	defaults := keys.Defaults()
	for _, action := range keys.Actions() {
		binding := keys.Binding(action.Name)
		if sameKeys(binding, defaults.Binding(action.Name)) {
			delete(stored, action.Name)
			continue
		}
		stored[action.Name] = keySpecs(binding)
	}
	encoded, err := json.Marshal(stored)
	if err != nil {
		return err
	}
	return s.SetSetting(keysSetting(keys.Scope()), string(encoded))
}

// writtenKeys is what the store names for the table's actions. An action
// it leaves out keeps its default, the way one left out of a file did.
func (s *Store) writtenKeys(table keybind.Table) (map[string]keybind.Binding, error) {
	stored, err := s.storedKeys(table.Scope())
	if err != nil {
		return nil, err
	}
	written := make(map[string]keybind.Binding, len(stored))
	for _, action := range table.Actions() {
		specs, named := stored[action.Name]
		if !named {
			continue
		}
		keys := make([]keybind.Key, 0, len(specs))
		for _, spec := range specs {
			key, err := keybind.Parse(spec)
			if err != nil {
				return nil, fmt.Errorf("stored %s key for %s: %w", table.Scope(), action.Name, err)
			}
			keys = append(keys, key)
		}
		written[action.Name] = keybind.Keys(keys...)
	}
	return written, nil
}

func (s *Store) storedKeys(scope string) (map[string][]string, error) {
	stored := map[string][]string{}
	raw, err := s.Setting(keysSetting(scope))
	if err != nil || raw == "" {
		return stored, err
	}
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return nil, fmt.Errorf("stored %s keys: %w", scope, err)
	}
	return stored, nil
}

func sameKeys(binding, other keybind.Binding) bool {
	return slices.Equal(keySpecs(binding), keySpecs(other))
}

func keySpecs(binding keybind.Binding) []string {
	specs := make([]string, 0, len(binding.Keys()))
	for _, key := range binding.Keys() {
		specs = append(specs, key.Tea())
	}
	return specs
}

// Editor is the command the editor key opens a directory in. Empty leaves
// the choice to the environment and what is on PATH.
func (s *Store) Editor() (string, error) {
	return s.Setting(editorSetting)
}

func (s *Store) SetEditor(line string) error {
	return s.SetSetting(editorSetting, line)
}

// ConfigImportError is why the config.toml beside the store could not be
// imported, empty when it was or when there was none.
func (s *Store) ConfigImportError() (string, error) {
	return s.Setting(configImportErrorSetting)
}

// importConfigFile carries the editor and the keys of the config.toml
// earlier releases read into the store. The file is left as it was.
func (s *Store) importConfigFile(dir string) error {
	imported, err := s.Setting(configImportSetting)
	if err != nil || imported != "" {
		return err
	}
	rows, err := legacyRows(dir)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Another process may have imported since the check above; its rows
	// are the user's settings now.
	claimed, err := tx.Exec(
		`INSERT INTO settings (key, value) VALUES (?, 'done') ON CONFLICT(key) DO NOTHING`,
		configImportSetting)
	if err != nil {
		return err
	}
	if first, err := claimed.RowsAffected(); err != nil || first == 0 {
		return err
	}
	for key, value := range rows {
		if _, err := tx.Exec(
			`INSERT INTO settings (key, value) VALUES (?, ?)
			 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// legacyRows is the settings a config.toml in dir turns into. A file that
// cannot be read becomes the one row that says why. A key the file set to
// its default is left out, the way the picker leaves it, so it keeps
// following the default.
func legacyRows(dir string) (map[string]string, error) {
	file, found, err := config.ReadLegacyFile(dir)
	if err != nil {
		return map[string]string{configImportErrorSetting: err.Error()}, nil
	}
	rows := map[string]string{}
	if !found {
		return rows, nil
	}
	if editor := strings.TrimSpace(file.Editor); editor != "" {
		rows[editorSetting] = editor
	}
	for _, table := range []struct {
		defaults keybind.Table
		written  map[string]keybind.Binding
	}{
		{keybind.DefaultSession(), file.Keybindings.Session},
		{keybind.DefaultList(), file.Keybindings.List},
	} {
		named := make(map[string][]string, len(table.written))
		for name, binding := range table.written {
			if !sameKeys(binding, table.defaults.Binding(name)) {
				named[name] = keySpecs(binding)
			}
		}
		if len(named) == 0 {
			continue
		}
		encoded, err := json.Marshal(named)
		if err != nil {
			return nil, err
		}
		rows[keysSetting(table.defaults.Scope())] = string(encoded)
	}
	return rows, nil
}
