//go:build windows

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// checkBatchShim refuses a .cmd or .bat: cmd.exe reparses its arguments, so
// a prompt holding `"` runs as commands. PowerShell prefers a .ps1 beside it,
// the pair npm installs, for a command that names no extension.
func checkBatchShim(binary, path string) error {
	ext := filepath.Ext(path)
	switch strings.ToLower(ext) {
	case ".cmd", ".bat":
	default:
		return nil
	}
	if filepath.Ext(binary) == "" {
		if _, err := os.Stat(strings.TrimSuffix(path, ext) + ".ps1"); err == nil {
			return nil
		}
	}
	return fmt.Errorf("%s is a batch file, which cannot take a prompt holding \" safely; point the tool's command at its .exe or .ps1", path)
}
