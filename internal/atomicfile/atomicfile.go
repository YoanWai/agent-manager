// Package atomicfile replaces small local files without exposing a partial
// write to readers.
package atomicfile

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
)

// WriteFile writes data to a temporary sibling, syncs it, then renames it over
// path. The destination directory is created when needed.
func WriteFile(path string, data []byte, perm fs.FileMode) error {
	return writeFile(path, data, perm, os.CreateTemp)
}

// WriteIfChanged writes a generated file only when its content changed, so
// concurrent launches reading the same path never see it rewritten.
func WriteIfChanged(path string, data []byte, perm fs.FileMode) error {
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, data) {
		return nil
	}
	return WriteFile(path, data, perm)
}

func writeFile(path string, data []byte, perm fs.FileMode, createTemp func(string, string) (*os.File, error)) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := createTemp(dir, "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
