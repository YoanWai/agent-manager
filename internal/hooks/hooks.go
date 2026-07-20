// Package hooks wires Claude Code hook events into status files: each
// managed session gets AGENT_MANAGER_STATUS_FILE in its environment, and
// the generated settings file makes Claude Code write its lifecycle state
// there. The poller reads these files as a tier-1 status source, ahead of
// the pane-regex heuristics.
package hooks

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/YoanWai/agent-manager/internal/status"
)

const EnvStatusFile = "AGENT_MANAGER_STATUS_FILE"

// EnvSessionID identifies the managed session to the rename subcommand;
// every session gets it regardless of tool.
const EnvSessionID = "AGENT_MANAGER_SESSION_ID"

// StatusSourceClaude is the status_source config value that enables this
// package for a tool.
const StatusSourceClaude = "claude-hooks"

const settingsName = "claude-settings.json"

type Manager struct {
	dir string
}

func NewManager(configDir string) *Manager {
	return &Manager{dir: filepath.Join(configDir, "hooks")}
}

type hookCommand struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

type hookMatcher struct {
	Matcher string        `json:"matcher,omitempty"`
	Hooks   []hookCommand `json:"hooks"`
}

type settingsFile struct {
	Hooks map[string][]hookMatcher `json:"hooks"`
}

// statusCommand always exits 0 and no-ops outside managed sessions, so
// the settings file is harmless if Claude Code loads it elsewhere.
func statusCommand(state string) string {
	return `[ -z "$` + EnvStatusFile + `" ] || printf ` + state + ` > "$` + EnvStatusFile + `"`
}

// notificationCommand filters the payload on stdin: Claude Code fires
// Notification both for permission prompts and for the 60-second idle
// "waiting for your input" reminder, and only the former means the agent
// is blocked.
func notificationCommand() string {
	return `[ -z "$` + EnvStatusFile + `" ] || grep -q "waiting for your input" || printf ` + status.Waiting + ` > "$` + EnvStatusFile + `"`
}

func settingsContent() ([]byte, error) {
	report := func(matcher, state string) []hookMatcher {
		return []hookMatcher{{Matcher: matcher, Hooks: []hookCommand{{Type: "command", Command: statusCommand(state)}}}}
	}
	content := settingsFile{Hooks: map[string][]hookMatcher{
		"UserPromptSubmit": report("", status.Working),
		"PreToolUse":       report("*", status.Working),
		"PostToolUse":      report("*", status.Working),
		"Notification":     {{Hooks: []hookCommand{{Type: "command", Command: notificationCommand()}}}},
		"Stop":             report("", status.Finished),
		// compact fires SessionStart in the middle of an active turn
		"SessionStart": report("startup|resume|clear", status.Idle),
		"SessionEnd": {{Hooks: []hookCommand{{
			Type:    "command",
			Command: `[ -z "$` + EnvStatusFile + `" ] || rm -f "$` + EnvStatusFile + `"`,
		}}}},
	}}
	return json.MarshalIndent(content, "", "  ")
}

// EnsureSettings writes the hook settings file, refreshing it when the
// wanted content changed (e.g. after an upgrade), and returns its path.
func (m *Manager) EnsureSettings() (string, error) {
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return "", err
	}
	wanted, err := settingsContent()
	if err != nil {
		return "", err
	}
	path := filepath.Join(m.dir, settingsName)
	existing, err := os.ReadFile(path)
	if err == nil && bytes.Equal(existing, wanted) {
		return path, nil
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if err := os.WriteFile(path, wanted, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func (m *Manager) StatusFile(id string) string {
	return filepath.Join(m.dir, id+".status")
}

// Read returns the hook-reported status for a session. The file is
// written by shell hooks, so anything but a known status is rejected.
func (m *Manager) Read(id string) (string, bool) {
	raw, err := os.ReadFile(m.StatusFile(id))
	if err != nil {
		return "", false
	}
	state := strings.TrimSpace(string(raw))
	switch state {
	case status.Working, status.Waiting, status.Finished, status.Idle:
		return state, true
	}
	return "", false
}

func (m *Manager) Remove(id string) error {
	return removeIfExists(m.StatusFile(id))
}

// NameFile is the mailbox the rename subcommand writes a session's
// self-chosen name into; the poller applies and deletes it.
func (m *Manager) NameFile(id string) string {
	return filepath.Join(m.dir, id+".name")
}

const maxNameLength = 80

// ReadName returns the pending rename for a session. found reports that
// the file exists, so the caller can consume it even when the content
// normalizes to nothing. The file is written by agents, so the name is
// squashed to one bounded line.
func (m *Manager) ReadName(id string) (name string, found bool) {
	raw, err := os.ReadFile(m.NameFile(id))
	if err != nil {
		return "", false
	}
	name = strings.Join(strings.Fields(string(raw)), " ")
	if runes := []rune(name); len(runes) > maxNameLength {
		name = string(runes[:maxNameLength])
	}
	return name, true
}

func (m *Manager) RemoveName(id string) error {
	return removeIfExists(m.NameFile(id))
}

// ReviewRepoFile is the mailbox the review-repo subcommand writes the repo
// a session is working in into; the poller applies and deletes it.
func (m *Manager) ReviewRepoFile(id string) string {
	return filepath.Join(m.dir, id+".reviewrepo")
}

func (m *Manager) ReadReviewRepo(id string) (root string, found bool) {
	raw, err := os.ReadFile(m.ReviewRepoFile(id))
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(raw)), true
}

func (m *Manager) RemoveReviewRepo(id string) error {
	return removeIfExists(m.ReviewRepoFile(id))
}

func removeIfExists(path string) error {
	err := os.Remove(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
