//go:build !darwin

package conversation

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
)

// openFiles lists the paths of the files and directories pid holds open,
// from its /proc fd links.
func openFiles(pid int) ([]string, error) {
	dir := filepath.Join("/proc", strconv.Itoa(pid), "fd")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, entry := range entries {
		// An fd closed between the listing and this read is no longer open.
		if target, err := os.Readlink(filepath.Join(dir, entry.Name())); err == nil {
			paths = append(paths, target)
		}
	}
	return paths, nil
}
