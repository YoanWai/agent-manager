//go:build !windows

package notify

import "os"

func publishRename(from, to string) error {
	return os.Rename(from, to)
}
