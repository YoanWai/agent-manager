//go:build windows

package notify

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// publishRetry bounds how long a publish waits out another rename that
// holds the destination open.
const publishRetry = 2 * time.Second

// Windows refuses to replace a file while another rename has it open,
// failing with access denied or a sharing violation until that rename
// finishes, so the publish retries rather than dropping the click.
func publishRename(from, to string) error {
	deadline := time.Now().Add(publishRetry)
	for delay := time.Millisecond; ; delay = min(2*delay, 10*time.Millisecond) {
		err := os.Rename(from, to)
		if err == nil || !transientRename(err) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(delay)
	}
}

func transientRename(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_SHARING_VIOLATION)
}
