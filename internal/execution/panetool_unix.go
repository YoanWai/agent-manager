//go:build !windows

package execution

import "path/filepath"

// executableName is the name a process tree reports for a program path.
func executableName(path string) string {
	return filepath.Base(path)
}
