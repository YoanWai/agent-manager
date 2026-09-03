package notify

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
)

// focusFile is where a clicked notification leaves the id of the session
// it named. The click lands in another process (the macOS helper, or the
// goroutine waiting on notify-send), so the request crosses to the manager
// through the config directory, which every manager on the machine polls.
const focusFile = "notify-focus"

var focusSeq atomic.Uint64

// scratchPath names a file no other caller can be using: rename replaces
// its destination, so two callers sharing one name would trample each
// other's request rather than each taking its own.
func scratchPath(configDir, stage string) string {
	name := focusFile + "." + stage + "." +
		strconv.Itoa(os.Getpid()) + "." + strconv.FormatUint(focusSeq.Add(1), 10)
	return filepath.Join(configDir, name)
}

// RequestFocus publishes the click through a rename, so a manager polling
// mid-write can never read half an id.
func RequestFocus(configDir, sessionID string) error {
	pending := scratchPath(configDir, "pending")
	if err := os.WriteFile(pending, []byte(sessionID+"\n"), 0o600); err != nil {
		return err
	}
	if err := os.Rename(pending, filepath.Join(configDir, focusFile)); err != nil {
		os.Remove(pending)
		return err
	}
	return nil
}

// TakeFocus returns the session id of a pending click. The rename is the
// claim: only one caller can win it, and a click published after it lands
// in a new file rather than under the winner's read.
func TakeFocus(configDir string) (string, bool) {
	claimed := scratchPath(configDir, "claimed")
	if err := os.Rename(filepath.Join(configDir, focusFile), claimed); err != nil {
		return "", false
	}
	defer os.Remove(claimed)
	data, err := os.ReadFile(claimed)
	if err != nil {
		return "", false
	}
	id := strings.TrimSpace(string(data))
	return id, id != ""
}
