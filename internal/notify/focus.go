package notify

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// focusFile is where a clicked notification leaves the id of the session
// it named. The click lands in another process (the macOS helper, or the
// goroutine waiting on notify-send), so the request crosses to the manager
// through the config directory, which every manager on the machine polls.
const focusFile = "notify-focus"

// RequestFocus records that the user clicked the banner for sessionID.
func RequestFocus(configDir, sessionID string) error {
	return os.WriteFile(filepath.Join(configDir, focusFile), []byte(sessionID+"\n"), 0o600)
}

// TakeFocus returns the session id of a pending click and clears it, so
// the first manager to poll acts on it and no other does.
func TakeFocus(configDir string) (string, bool) {
	path := filepath.Join(configDir, focusFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", false
	}
	id := strings.TrimSpace(string(data))
	return id, id != ""
}
