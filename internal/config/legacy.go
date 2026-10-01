package config

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/BurntSushi/toml"

	"github.com/YoanWai/agent-manager/internal/keybind"
)

// LegacyFile is what a config.toml asked for before Settings held it: the
// editor, and the keys it named.
type LegacyFile struct {
	Editor      string      `toml:"editor"`
	Keybindings Keybindings `toml:"keybindings"`
}

type Keybindings struct {
	Session map[string]keybind.Binding `toml:"session"`
	List    map[string]keybind.Binding `toml:"list"`
}

// ReadLegacyFile reads the config.toml an earlier release kept in dir, and
// refuses key tables that release would have refused. found is false when
// dir has no such file.
func ReadLegacyFile(dir string) (file LegacyFile, found bool, err error) {
	path := filepath.Join(dir, "config.toml")
	if _, err := toml.DecodeFile(path, &file); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return LegacyFile{}, false, nil
		}
		return LegacyFile{}, false, fmt.Errorf("%s: %w", path, err)
	}
	if _, err := keybind.SessionTable(file.Keybindings.Session); err != nil {
		return LegacyFile{}, false, fmt.Errorf("%s: %w", path, err)
	}
	if _, err := keybind.ListTable(file.Keybindings.List); err != nil {
		return LegacyFile{}, false, fmt.Errorf("%s: %w", path, err)
	}
	return file, true, nil
}
