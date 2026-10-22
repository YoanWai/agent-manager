//go:build !windows

package agentsession

import (
	"os"
	"path/filepath"
)

// hermesDefaultHome is where hermes keeps its data when HERMES_HOME is unset.
func hermesDefaultHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".hermes"), nil
}
