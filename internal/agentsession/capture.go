// Package agentsession reads back the conversation id an agent CLI minted
// for a session the manager launched, for tools that do not accept a
// chosen id at launch (codex, opencode). Revive resumes that exact id
// instead of the working directory's most recent conversation, which is
// the wrong one whenever sessions share a directory.
package agentsession

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// clockSlack absorbs small differences between the manager's launch clock
// and the filesystem timestamp on the store entry the CLI writes.
const clockSlack = 10 * time.Second

// opencodeScanLimit caps how many of the most-recent sessions we inspect per
// capture. `opencode session list` is newest-first, so a just-launched
// session sits near the top; scanning further only spends process spawns.
const opencodeScanLimit = 40

// opencodeCLITimeout bounds each opencode subprocess so a hung CLI cannot
// stall the poll loop.
const opencodeCLITimeout = 15 * time.Second

// opencodeExportHeadBytes is how much of `opencode export` output to read.
// The payload leads with the session's info block, then streams the entire
// conversation, which for a long session takes far longer than the metadata
// is worth waiting for. Reading a bounded prefix captures the info block and
// lets us stop before the transcript.
const opencodeExportHeadBytes = 64 * 1024

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
		return captureOpencode(cwd, launchedAt, claimed)
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

// runOpencode runs an opencode subcommand from cwd and returns its stdout.
// Reading ids and metadata from opencode's own CLI keeps capture working
// when opencode changes its private on-disk storage layout between releases,
// which a hardcoded store path does not survive. The command runs in cwd
// because `opencode session list` is scoped to the calling directory's
// project, so a session's own conversations only surface from its cwd.
func runOpencode(cwd string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), opencodeCLITimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "opencode", args...)
	cmd.Dir = cwd
	return cmd.Output()
}

// runOpencodeHead runs an opencode subcommand from cwd and returns at most
// opencodeExportHeadBytes of stdout, killing the process once it has that
// much. `opencode export` on a long session would otherwise stream the whole
// transcript and blow past the timeout before the leading info block is even
// consumed.
func runOpencodeHead(cwd string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), opencodeCLITimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "opencode", args...)
	cmd.Dir = cwd
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	buf := make([]byte, opencodeExportHeadBytes)
	n, _ := io.ReadFull(pipe, buf)
	cancel()
	_ = cmd.Wait()
	return buf[:n], nil
}

var opencodeIDPattern = regexp.MustCompile(`ses_[A-Za-z0-9]+`)

// opencodeListIDs returns session ids newest-first from `opencode session
// list` run in cwd. A package variable so tests substitute canned output.
var opencodeListIDs = func(cwd string) ([]string, bool) {
	out, err := runOpencode(cwd, "session", "list")
	if err != nil {
		return nil, false
	}
	return dedupeOrdered(opencodeIDPattern.FindAllString(string(out), -1)), true
}

// opencodeSessionMeta returns a session's working directory and creation
// time from `opencode export <id>` run in cwd. A package variable so tests
// substitute canned output.
var opencodeSessionMeta = func(cwd, id string) (directory string, created time.Time, ok bool) {
	out, err := runOpencodeHead(cwd, "export", id)
	if err != nil {
		return "", time.Time{}, false
	}
	return parseOpencodeExport(out)
}

// dedupeOrdered removes duplicates while keeping first-seen order.
func dedupeOrdered(items []string) []string {
	seen := make(map[string]bool, len(items))
	out := items[:0]
	for _, item := range items {
		if seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

// parseOpencodeExport reads directory and creation time from the info block
// of an `opencode export` payload. The output leads with a human preamble
// and is read only as a prefix, so it extracts just the info object by brace
// matching rather than decoding the whole (possibly truncated) document.
func parseOpencodeExport(out []byte) (directory string, created time.Time, ok bool) {
	obj, found := extractInfoObject(out)
	if !found {
		return "", time.Time{}, false
	}
	var info struct {
		Directory string `json:"directory"`
		Time      struct {
			Created int64 `json:"created"`
		} `json:"time"`
	}
	if err := json.Unmarshal(obj, &info); err != nil || info.Directory == "" {
		return "", time.Time{}, false
	}
	return info.Directory, time.UnixMilli(info.Time.Created), true
}

// extractInfoObject returns the JSON object that follows the "info" key,
// balancing braces so a truncated export prefix still yields a complete info
// object as long as that block was fully read.
func extractInfoObject(out []byte) ([]byte, bool) {
	key := bytes.Index(out, []byte(`"info"`))
	if key < 0 {
		return nil, false
	}
	open := bytes.IndexByte(out[key:], '{')
	if open < 0 {
		return nil, false
	}
	start := key + open
	depth, inString, escaped := 0, false, false
	for i := start; i < len(out); i++ {
		c := out[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return out[start : i+1], true
			}
		}
	}
	return nil, false
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

// captureOpencode finds the conversation opencode minted for a session
// launched in cwd, reading ids and metadata from opencode's public CLI
// rather than its private storage. It inspects the most-recent sessions,
// keeps those whose directory matches and that were created at or after
// launch, and returns the earliest such conversation not already claimed.
func captureOpencode(cwd string, launchedAt time.Time, claimed map[string]bool) (string, bool) {
	ids, ok := opencodeListIDs(cwd)
	if !ok {
		return "", false
	}
	cutoff := launchedAt.Add(-clockSlack)
	wantCwd := resolvePath(cwd)
	var cands []candidate
	for i, id := range ids {
		if i >= opencodeScanLimit {
			break
		}
		if claimed[id] {
			continue
		}
		dir, created, ok := opencodeSessionMeta(cwd, id)
		if !ok || resolvePath(dir) != wantCwd || created.Before(cutoff) {
			continue
		}
		cands = append(cands, candidate{id: id, modTime: created})
	}
	return pickEarliest(cands)
}
