//go:build !windows

package mcpreg

import (
	"path/filepath"
	"strings"
)

// The install directory Hermes reports is its site-packages, so the
// interpreter sits three levels above it.
func pythonFromVersion(version string) string {
	for _, line := range strings.Split(version, "\n") {
		dir, found := strings.CutPrefix(strings.TrimSpace(line), "Install directory:")
		if !found {
			continue
		}
		dir = strings.TrimSpace(dir)
		if filepath.Base(dir) != "site-packages" {
			return ""
		}
		root := filepath.Dir(filepath.Dir(filepath.Dir(dir)))
		return filepath.Join(root, "bin", "python3")
	}
	return ""
}
