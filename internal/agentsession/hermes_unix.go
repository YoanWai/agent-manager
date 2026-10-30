//go:build !windows

package agentsession

import "path/filepath"

// hermesDefaultHome is where hermes keeps its data when HERMES_HOME is unset.
func hermesDefaultHome(env Env) (string, error) {
	home, err := env.homeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".hermes"), nil
}
