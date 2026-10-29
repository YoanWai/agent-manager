//go:build windows

package agentsession

import (
	"os"
	"path/filepath"
)

// hermesDefaultHome is where hermes keeps its data when HERMES_HOME is unset:
// native Windows hermes uses %LOCALAPPDATA%\hermes, not ~/.hermes (which is
// the WSL layout).
func hermesDefaultHome(env Env) (string, error) {
	local := env.get("LOCALAPPDATA")
	if local == "" {
		var err error
		if local, err = os.UserCacheDir(); err != nil {
			return "", err
		}
	}
	return filepath.Join(local, "hermes"), nil
}
