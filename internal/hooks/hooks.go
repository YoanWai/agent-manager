// Package hooks wires Claude Code hook events into status files: each
// managed session gets AGENT_MANAGER_STATUS_FILE in its environment, and
// the generated settings file makes Claude Code write its lifecycle state
// there. The poller reads these files as a tier-1 status source, ahead of
// the pane-regex heuristics.
package hooks

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

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

// Dir exposes the hooks directory for other generated per-session
// artifacts, like MCP registration configs.
func (h *Manager) Dir() string {
	return h.dir
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

// blockingNotifications are the Notification types that leave the turn
// stuck on the user. The event also fires for the idle reminder, for
// authentication and for background agents finishing, none of which
// block, so the matcher names the two that do.
const blockingNotifications = "permission_prompt|elicitation_dialog"

// limitStopFailures are the StopFailure error types that mean the account
// hit a usage or rate limit. The pane also classifies those as errored;
// the hook write covers the turn before the banner is visible.
const limitStopFailures = "rate_limit"

func settingsContent() ([]byte, error) {
	report := func(matcher, state string) []hookMatcher {
		return []hookMatcher{{Matcher: matcher, Hooks: []hookCommand{{Type: "command", Command: statusCommand(state)}}}}
	}
	content := settingsFile{Hooks: map[string][]hookMatcher{
		"UserPromptSubmit": report("", status.Working),
		"PreToolUse":       report("*", status.Working),
		"PostToolUse":      report("*", status.Working),
		"Notification":     report(blockingNotifications, status.Waiting),
		"Stop":             report("", status.Finished),
		"StopFailure":      report(limitStopFailures, status.Errored),
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
	case status.Working, status.Waiting, status.Finished, status.Idle, status.Errored:
		return state, true
	}
	return "", false
}

func (m *Manager) Remove(id string) error {
	return removeIfExists(m.StatusFile(id))
}

// NameFile is the mailbox the rename subcommand writes a session's
// self-chosen name into; the poller applies and deletes it. The first
// line is the id of the request, so its answer comes back to the caller
// that asked and not to another rename for the same session.
func (m *Manager) NameFile(id string) string {
	return filepath.Join(m.dir, id+".name")
}

// NewRequestID names one rename, keeping its answer apart from the answer
// to a rename another caller queued for the same session.
func NewRequestID() (string, error) {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

// NameRequest is one queued rename: the id its answer comes back under,
// and the name the session is asked to take.
func NameRequest(request, name string) string {
	return request + "\n" + name
}

const maxNameLength = 80

// ReadName returns the pending rename for a session: the id of the
// request and the name it asks for. found reports that the file exists,
// so the caller can consume it even when the content normalizes to
// nothing.
func (m *Manager) ReadName(id string) (request, name string, found bool) {
	raw, err := os.ReadFile(m.NameFile(id))
	if err != nil {
		return "", "", false
	}
	request, name = parseNameRequest(string(raw))
	return request, name, true
}

var requestIDPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

// parseNameRequest splits a queued rename into the request its answer
// comes back under and the name asked for. The file is written by agents,
// so a first line that is not a request this package issued is no request
// at all: the whole file is the name, and the rename is applied with
// nobody to answer rather than letting that line reach a file path.
func parseNameRequest(raw string) (request, name string) {
	first, rest, split := strings.Cut(raw, "\n")
	if !split || !requestIDPattern.MatchString(first) {
		return "", NormalizeName(raw)
	}
	return first, NormalizeName(rest)
}

// NormalizeName is the name a session actually takes: written by agents,
// so squashed to one bounded line. Whoever asks for a rename normalizes
// its own name the same way to recognize the answer it gets back.
func NormalizeName(raw string) string {
	name := strings.Join(strings.Fields(raw), " ")
	if runes := []rune(name); len(runes) > maxNameLength {
		name = string(runes[:maxNameLength])
	}
	return name
}

func (m *Manager) RemoveName(id string) error {
	if err := removeIfExists(m.claimedNameFile(id)); err != nil {
		return err
	}
	return removeIfExists(m.NameFile(id))
}

// NameResultLifetime is how long an answer nobody claimed is kept. It is
// far longer than any caller waits, so a sweep never takes an answer out
// from under one still reading for it.
const NameResultLifetime = 5 * time.Minute

// SweepNameResults drops answers nobody came back for, including those of
// sessions that have since been deleted, whose ids never come round again.
func (m *Manager) SweepNameResults(now time.Time) error {
	paths, err := filepath.Glob(filepath.Join(m.dir, "*.renamed"))
	if err != nil {
		return err
	}
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return err
		}
		if now.Sub(info.ModTime()) < NameResultLifetime {
			continue
		}
		if err := removeIfExists(path); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) claimedNameFile(id string) string {
	return filepath.Join(m.dir, id+".name.claimed")
}

// ClaimName takes a pending rename for the manager to apply: the file is
// moved aside in one step, so a rename written while this one is being
// applied lands on a free mailbox and waits for the next poll instead of
// being consumed with it. A claim left by a manager that stopped midway
// is picked up again ahead of anything newer. ReleaseName ends the claim.
func (m *Manager) ClaimName(id string) (request, name string, found bool, err error) {
	claimed := m.claimedNameFile(id)
	raw, err := os.ReadFile(claimed)
	if errors.Is(err, fs.ErrNotExist) {
		if err := os.Rename(m.NameFile(id), claimed); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return "", "", false, nil
			}
			return "", "", false, err
		}
		raw, err = os.ReadFile(claimed)
	}
	if err != nil {
		return "", "", false, err
	}
	request, name = parseNameRequest(string(raw))
	return request, name, true, nil
}

// ClaimedRequest reports the rename the manager has taken to apply, so a
// caller can tell its request being worked on from one a later rename
// replaced in the mailbox before it was ever claimed.
func (m *Manager) ClaimedRequest(id string) (request string, found bool, err error) {
	raw, err := os.ReadFile(m.claimedNameFile(id))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", false, nil
		}
		return "", false, err
	}
	request, _ = parseNameRequest(string(raw))
	return request, true, nil
}

func (m *Manager) ReleaseName(id string) error {
	return removeIfExists(m.claimedNameFile(id))
}

// NameResultFile is where the poller reports what became of one rename,
// under a request id this package issued, so the name never reaches the
// path.
// so the subcommand that queued it can tell the agent the truth instead
// of assuming success. It is named for the request, so a second rename
// for the same session neither reads nor removes this answer. The verdict
// is "renamed" or "refused", then the name that was asked for, then the
// applied name or the reason.
func (m *Manager) NameResultFile(id, request string) string {
	return filepath.Join(m.dir, id+"."+request+".renamed")
}

// WriteNameResult answers the rename that asked for requested. The
// waiting subcommand polls for this file, so it lands whole: a
// half-written verdict would read as a rename that never happened.
func (m *Manager) WriteNameResult(id, request, requested, applied string, refusal error) error {
	content := "renamed\n" + requested + "\n" + applied
	if refusal != nil {
		content = "refused\n" + requested + "\n" + refusal.Error()
	}
	return WriteWhole(m.NameResultFile(id, request), content)
}

// NameVerdict is what became of one rename: the name it asked for, and
// either the name the session took or the reason it kept the one it had.
type NameVerdict struct {
	Requested string
	Applied   string
	Refusal   error
}

// ReadNameResult returns the poller's answer, if it has written one. A
// caller compares Requested against its own name to know whether the
// answer is the one it is waiting for. Anything but the two verdicts this
// package writes is no answer at all, while a mailbox that cannot be read
// is an error rather than silence, since silence reads as "not yet".
func (m *Manager) ReadNameResult(id, request string) (verdict NameVerdict, found bool, err error) {
	raw, err := os.ReadFile(m.NameResultFile(id, request))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return NameVerdict{}, false, nil
		}
		return NameVerdict{}, false, err
	}
	state, rest, hasName := strings.Cut(string(raw), "\n")
	requested, detail, hasDetail := strings.Cut(rest, "\n")
	if !hasName || !hasDetail || requested == "" || detail == "" {
		return NameVerdict{}, false, nil
	}
	switch state {
	case "renamed":
		return NameVerdict{Requested: requested, Applied: detail}, true, nil
	case "refused":
		return NameVerdict{Requested: requested, Refusal: errors.New(detail)}, true, nil
	}
	return NameVerdict{}, false, nil
}

func (m *Manager) RemoveNameResult(id, request string) error {
	return removeIfExists(m.NameResultFile(id, request))
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

// ReviewBaseFile is the mailbox the review-base subcommand writes the repo
// root and the base ref its branch diffs against into; the poller applies
// and deletes it. The file holds two lines: the repo root then the ref.
func (m *Manager) ReviewBaseFile(id string) string {
	return filepath.Join(m.dir, id+".reviewbase")
}

func (m *Manager) ReadReviewBase(id string) (root, ref string, found bool) {
	raw, err := os.ReadFile(m.ReviewBaseFile(id))
	if err != nil {
		return "", "", false
	}
	lines := strings.SplitN(string(raw), "\n", 2)
	root = strings.TrimSpace(lines[0])
	if len(lines) > 1 {
		ref = strings.TrimSpace(lines[1])
	}
	return root, ref, true
}

func (m *Manager) RemoveReviewBase(id string) error {
	return removeIfExists(m.ReviewBaseFile(id))
}

// ReviewScopeFile is the mailbox the review-mode MCP tool writes the
// diff scope into; the poller applies and deletes it.
func (m *Manager) ReviewScopeFile(id string) string {
	return filepath.Join(m.dir, id+".reviewscope")
}

func (m *Manager) ReadReviewScope(id string) (scope string, found bool) {
	raw, err := os.ReadFile(m.ReviewScopeFile(id))
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(raw)), true
}

func (m *Manager) RemoveReviewScope(id string) error {
	return removeIfExists(m.ReviewScopeFile(id))
}

// WriteWhole leaves the file complete or absent, never half written, so
// a reader polling for it never picks up a partial line. Each writer
// stages under a name of its own, so two of them cannot publish each
// other's content.
func WriteWhole(path, content string) (err error) {
	staging, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.part")
	if err != nil {
		return err
	}
	// The staging file is gone once it lands, so this collects the one a
	// failure left behind rather than letting it sit in the mailbox.
	defer func() {
		if leftover := removeIfExists(staging.Name()); err == nil {
			err = leftover
		}
	}()
	if _, err := staging.WriteString(content); err != nil {
		return errors.Join(err, staging.Close())
	}
	if err := staging.Close(); err != nil {
		return err
	}
	if err := os.Chmod(staging.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(staging.Name(), path)
}

func removeIfExists(path string) error {
	err := os.Remove(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
