package notify

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// focusFile is the stem of the file where a clicked notification leaves
// the id of the session it named. The click lands in another process (the
// macOS helper, or the goroutine waiting on notify-send), so the request
// crosses to the manager through the config directory. Several managers
// share that directory, and each names its own file after its process, so
// the manager whose banner was clicked is the one that acts on it.
const focusFile = "notify-focus"

var (
	focusSeq   atomic.Uint64
	sweepStale sync.Once
)

func focusPath(configDir string, manager int) string {
	return filepath.Join(configDir, focusFile+"."+strconv.Itoa(manager))
}

// scratchPath names a file no other caller can be using: rename replaces
// its destination, so two callers sharing one name would trample each
// other's request rather than each taking its own.
func scratchPath(configDir, stage string) string {
	name := focusFile + "." + stage + "." +
		strconv.Itoa(os.Getpid()) + "." + strconv.FormatUint(focusSeq.Add(1), 10)
	return filepath.Join(configDir, name)
}

// RequestFocus publishes the click through a rename, so a manager polling
// mid-write can never read half an id. manager is the process id of the
// manager that posted the banner.
func RequestFocus(configDir string, manager int, sessionID string) error {
	pending := scratchPath(configDir, "pending")
	if err := os.WriteFile(pending, []byte(sessionID+"\n"), 0o600); err != nil {
		return err
	}
	if err := os.Rename(pending, focusPath(configDir, manager)); err != nil {
		os.Remove(pending)
		return err
	}
	return nil
}

// TakeFocus returns the session id of a click on this manager's own
// banner. The rename is the claim: only one caller can win it, and a click
// published after it lands in a new file rather than under the winner's
// read.
func TakeFocus(configDir string) (string, bool) {
	sweepStale.Do(func() { sweepAbandonedFocus(configDir) })
	claimed := scratchPath(configDir, "claimed")
	if err := os.Rename(focusPath(configDir, os.Getpid()), claimed); err != nil {
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

// sweepAbandonedFocus clears requests left by managers that exited before
// polling for them. Anything younger than the click window may still
// belong to a manager that is running, so only older files go.
func sweepAbandonedFocus(configDir string) {
	stale, err := filepath.Glob(filepath.Join(configDir, focusFile+".*"))
	if err != nil {
		return
	}
	for _, path := range stale {
		info, err := os.Stat(path)
		if err != nil || time.Since(info.ModTime()) < clickTimeout {
			continue
		}
		os.Remove(path)
	}
}
