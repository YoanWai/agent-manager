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

// A click lands in another process: the macOS helper, or the goroutine
// waiting on notify-send. The config directory is how it crosses back,
// and since several managers share that directory, each file carries the
// process of the manager whose banner was clicked.
const focusFile = "notify-focus"

var (
	focusSeq   atomic.Uint64
	sweepStale sync.Once
)

func focusPath(configDir string, manager int) string {
	return filepath.Join(configDir, focusFile+"."+strconv.Itoa(manager))
}

// rename replaces its destination, so two callers sharing one scratch
// name would trample each other's request rather than each taking its
// own.
func scratchPath(configDir, stage string) string {
	name := focusFile + "." + stage + "." +
		strconv.Itoa(os.Getpid()) + "." + strconv.FormatUint(focusSeq.Add(1), 10)
	return filepath.Join(configDir, name)
}

// RequestFocus publishes through a rename, so a manager polling mid-write
// cannot read half an id. manager is the process that posted the banner.
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

// TakeFocus serves this manager's own banners only. The rename is the
// claim: one caller wins it, and a click published afterwards lands in a
// new file rather than under the winner's read.
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

// A request younger than the click window may still belong to a manager
// that is running, so only older ones are abandoned.
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
