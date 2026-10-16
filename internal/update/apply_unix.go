//go:build !windows

package update

import "os"

const (
	archiveExt = ".tar.gz"
	binaryName = "agent-manager"
)

// swapBinary renames staged over target. The running process keeps its
// (now unlinked) image and the next start runs the new build under a fresh
// inode, which also sidesteps macOS's per-inode signature cache.
func swapBinary(staged, target string) error {
	if err := os.Rename(staged, target); err != nil {
		os.Remove(staged)
		return err
	}
	return nil
}
