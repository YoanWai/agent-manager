//go:build windows

package update

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

const (
	archiveExt = ".zip"
	binaryName = "agent-manager.exe"
)

// swapBinary moves target aside to target.old and staged into its place:
// Windows lets a running executable be renamed but not replaced or deleted.
// The .old left by the previous update goes first; it stays locked while an
// agent-manager started before that update still runs from it.
func swapBinary(staged, target string) error {
	old := target + ".old"
	if err := os.Remove(old); err != nil && !errors.Is(err, fs.ErrNotExist) {
		os.Remove(staged)
		return fmt.Errorf("update: %s.old is still in use; close other agent-manager windows and retry: %w", target, err)
	}
	if err := os.Rename(target, old); err != nil {
		os.Remove(staged)
		return err
	}
	if err := os.Rename(staged, target); err != nil {
		os.Rename(old, target)
		os.Remove(staged)
		return err
	}
	return nil
}
