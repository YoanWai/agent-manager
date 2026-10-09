package conversation

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var hermesUnsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// HermesConversation reads the conversation Hermes last wrote for a terminal.
// Hermes writes it on startup and on /new, and again as it closes, which
// covers /branch and /resume unless it is killed outright. Only a crumb
// written for the session's directory between since and until counts, where
// a zero until sets no bound: a pane started after the session's died can be
// handed the same terminal.
func HermesConversation(tty string, since, until time.Time, cwd string) (string, error) {
	home := os.Getenv("HERMES_HOME")
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		home = filepath.Join(userHome, ".hermes")
	}
	name := hermesUnsafeName.ReplaceAllString(strings.Trim(strings.TrimSpace(tty), "/"), "-")
	if name == "" || !strings.HasPrefix(tty, "/dev/") {
		return "", nil
	}
	data, err := os.ReadFile(filepath.Join(home, "terminal-sessions", "tty-"+name))
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var crumb struct {
		SessionID string  `json:"session_id"`
		Cwd       string  `json:"cwd"`
		Written   float64 `json:"ts"`
	}
	if json.Unmarshal(data, &crumb) != nil || crumb.Written < unixSeconds(since) || !sameDir(crumb.Cwd, cwd) {
		return "", nil
	}
	if !until.IsZero() && crumb.Written > unixSeconds(until) {
		return "", nil
	}
	return crumb.SessionID, nil
}

func sameDir(a, b string) bool {
	resolvedA, errA := filepath.EvalSymlinks(a)
	resolvedB, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && resolvedA == resolvedB
}

func unixSeconds(at time.Time) float64 {
	return float64(at.UnixNano()) / 1e9
}
