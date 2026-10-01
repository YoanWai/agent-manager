//go:build windows

package ui

import (
	"path/filepath"
	"strings"
)

// executableName is the name a process tree reports for a program path:
// the process snapshot names an image in lower case without its .exe, so
// a command spelled claude.exe or C:\Tools\Claude.exe is the same claude.
func executableName(path string) string {
	return strings.TrimSuffix(strings.ToLower(filepath.Base(path)), ".exe")
}
