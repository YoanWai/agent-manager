//go:build windows

package mcpreg

import (
	"path/filepath"
	"strings"
)

// The install directory Hermes reports is its site-packages, which a
// Windows environment keeps under Lib with the interpreter in Scripts.
func pythonFromVersion(version string) string {
	for _, line := range strings.Split(version, "\n") {
		dir, found := strings.CutPrefix(strings.TrimSpace(line), "Install directory:")
		if !found {
			continue
		}
		dir = filepath.Clean(strings.TrimSpace(dir))
		lib := filepath.Dir(dir)
		if !strings.EqualFold(filepath.Base(dir), "site-packages") || !strings.EqualFold(filepath.Base(lib), "Lib") {
			return ""
		}
		return filepath.Join(filepath.Dir(lib), "Scripts", "python.exe")
	}
	return ""
}
