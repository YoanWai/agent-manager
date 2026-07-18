// Package agentsession reads back the conversation id an agent CLI minted
// for a session the manager launched, for tools that do not accept a
// chosen id at launch (codex, opencode). Revive resumes that exact id
// instead of the working directory's most recent conversation, which is
// the wrong one whenever sessions share a directory.
package agentsession

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// clockSlack absorbs small differences between the manager's launch clock
// and the filesystem timestamp on the store entry the CLI writes.
const clockSlack = 10 * time.Second

// resolvePath canonicalizes a directory so a session launched via a
// symlinked path (macOS /tmp -> /private/tmp) still matches the store
// entry, which records the resolved path. Unresolvable paths compare raw.
func resolvePath(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return p
}

// Capture returns the conversation id a tool wrote for a session launched
// in cwd at or after launchedAt. sessionStore selects the on-disk format
// ("codex" or "opencode"). claimed holds ids already bound to other
// sessions, so two sessions started in one directory do not capture the
// same conversation. It returns ok=false when no confident match exists
// yet; the caller retries on the next poll.
func Capture(sessionStore, cwd string, launchedAt time.Time, claimed map[string]bool) (string, bool) {
	switch sessionStore {
	case "codex":
		return captureCodex(codexRoot(), cwd, launchedAt, claimed)
	case "opencode":
		return captureOpencode(opencodeRoot(), cwd, launchedAt, claimed)
	default:
		return "", false
	}
}

func codexRoot() string {
	if home := os.Getenv("CODEX_HOME"); home != "" {
		return filepath.Join(home, "sessions")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex", "sessions")
}

func opencodeRoot() string {
	if data := os.Getenv("XDG_DATA_HOME"); data != "" {
		return filepath.Join(data, "opencode", "storage", "session")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "opencode", "storage", "session")
}

// candidate is a store entry that could be the session's conversation,
// ranked by write time so the earliest one created after launch wins.
type candidate struct {
	id      string
	modTime time.Time
}

// pickEarliest returns the id of the oldest candidate, i.e. the first
// conversation written after the session launched.
func pickEarliest(cands []candidate) (string, bool) {
	if len(cands) == 0 {
		return "", false
	}
	best := cands[0]
	for _, c := range cands[1:] {
		if c.modTime.Before(best.modTime) {
			best = c
		}
	}
	return best.id, true
}

// captureCodex scans rollout-*.jsonl files, each whose first line is a
// session_meta record carrying the session id and the directory it ran
// in. A file older than the launch cannot be this session's.
func captureCodex(root, cwd string, launchedAt time.Time, claimed map[string]bool) (string, bool) {
	if root == "" {
		return "", false
	}
	cutoff := launchedAt.Add(-clockSlack)
	wantCwd := resolvePath(cwd)
	var cands []candidate
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasPrefix(name, "rollout-") || !strings.HasSuffix(name, ".jsonl") {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.ModTime().Before(cutoff) {
			return nil
		}
		id, metaCwd, ok := codexMeta(path)
		if !ok || resolvePath(metaCwd) != wantCwd || claimed[id] {
			return nil
		}
		cands = append(cands, candidate{id: id, modTime: info.ModTime()})
		return nil
	})
	return pickEarliest(cands)
}

// codexMeta reads the session id and cwd from a rollout's first line.
func codexMeta(path string) (id, cwd string, ok bool) {
	file, err := os.Open(path)
	if err != nil {
		return "", "", false
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	if !scanner.Scan() {
		return "", "", false
	}
	var line struct {
		Type    string `json:"type"`
		Payload struct {
			SessionID string `json:"session_id"`
			Cwd       string `json:"cwd"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
		return "", "", false
	}
	if line.Type != "session_meta" || line.Payload.SessionID == "" {
		return "", "", false
	}
	return line.Payload.SessionID, line.Payload.Cwd, true
}

// captureOpencode scans ses_*.json files, each recording the session id
// and the directory it ran in.
func captureOpencode(root, cwd string, launchedAt time.Time, claimed map[string]bool) (string, bool) {
	if root == "" {
		return "", false
	}
	cutoff := launchedAt.Add(-clockSlack)
	wantCwd := resolvePath(cwd)
	var cands []candidate
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasPrefix(name, "ses_") || !strings.HasSuffix(name, ".json") {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.ModTime().Before(cutoff) {
			return nil
		}
		id, dir, ok := opencodeMeta(path)
		if !ok || resolvePath(dir) != wantCwd || claimed[id] {
			return nil
		}
		cands = append(cands, candidate{id: id, modTime: info.ModTime()})
		return nil
	})
	return pickEarliest(cands)
}

func opencodeMeta(path string) (id, directory string, ok bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", "", false
	}
	var session struct {
		ID        string `json:"id"`
		Directory string `json:"directory"`
	}
	if err := json.Unmarshal(raw, &session); err != nil || session.ID == "" {
		return "", "", false
	}
	return session.ID, session.Directory, true
}
