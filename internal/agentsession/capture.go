// Package agentsession reads back the conversation id an agent CLI minted
// for a session the manager launched, for tools that do not accept a
// chosen id at launch (codex, opencode, gemini, hermes). Revive resumes that
// exact id instead of the working directory's most recent conversation, which
// is the wrong one whenever sessions share a directory.
package agentsession

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	_ "modernc.org/sqlite"
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

var hermesProfilePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

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
// ("codex", "opencode", "gemini", "hermes" or "command-code"). claimed holds ids already
// bound to other sessions, so two sessions started in one directory do not
// capture the same conversation. It returns ok=false when no confident match
// exists yet; the caller retries on the next poll.
func Capture(sessionStore, cwd string, launchedAt time.Time, claimed map[string]bool) (string, bool) {
	switch sessionStore {
	case "codex":
		return captureCodex(codexRoot(), cwd, launchedAt, claimed)
	case "opencode":
		return captureOpencode(cwd, launchedAt, claimed)
	case "gemini":
		return captureGemini(geminiRoot(), cwd, launchedAt, claimed)
	case "hermes":
		return captureHermes(hermesStateDB(), cwd, launchedAt, claimed)
	case "command-code":
		return captureCommandCode(commandCodeRoot(), cwd, launchedAt, claimed)
	default:
		return "", false
	}
}

// Recapture returns the conversation a resumed session picked, without the
// earliest-write tie-break Capture applies: a resumed session replays an
// existing conversation rather than minting one, so the store holds a
// single conversation touched at or after the relaunch at best, and a
// shared cwd can hold several. When the candidates for cwd and time do not
// come to exactly one, the row is left without an id rather than guessing.
// hermes keeps no write signal to tell a resumed conversation apart, so it
// falls through to captureHermes and its launch-fresh sessions only.
func Recapture(sessionStore, cwd string, launchedAt time.Time, claimed map[string]bool) (string, bool) {
	cutoff := launchedAt.Add(-clockSlack)
	var cands []candidate
	switch sessionStore {
	case "codex":
		cands = codexCandidates(codexRoot(), cwd, cutoff, claimed)
	case "gemini":
		cands = geminiCandidates(geminiRoot(), cwd, cutoff, claimed)
	case "command-code":
		cands = commandCodeCandidates(commandCodeRoot(), cwd, cutoff, claimed)
	case "opencode":
		return recaptureOpencode(cwd, cutoff, claimed)
	case "hermes":
		return captureHermes(hermesStateDB(), cwd, launchedAt, claimed)
	default:
		return "", false
	}
	if len(cands) != 1 {
		return "", false
	}
	return cands[0].id, true
}

func commandCodeRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".commandcode", "projects")
}

// captureCommandCode finds the conversation Command Code minted for a session
// launched in cwd. Each transcript's first line is a session record carrying
// the id and the working directory, so capture walks the per-project folders
// instead of guessing cmd's project folder naming.
func captureCommandCode(root, cwd string, launchedAt time.Time, claimed map[string]bool) (string, bool) {
	return pickEarliest(commandCodeCandidates(root, cwd, launchedAt.Add(-clockSlack), claimed))
}

func commandCodeCandidates(root, cwd string, cutoff time.Time, claimed map[string]bool) []candidate {
	if root == "" {
		return nil
	}
	wantCwd := resolvePath(cwd)
	var cands []candidate
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".jsonl") || strings.Contains(name, ".checkpoints.") {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.ModTime().Before(cutoff) {
			return nil
		}
		id, sessionCwd, ok := commandCodeMeta(path)
		if !ok || resolvePath(sessionCwd) != wantCwd || claimed[id] {
			return nil
		}
		cands = append(cands, candidate{id: id, modTime: info.ModTime()})
		return nil
	})
	return cands
}

func commandCodeMeta(path string) (id, cwd string, ok bool) {
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
		Type      string `json:"type"`
		ID        string `json:"id"`
		SessionID string `json:"sessionId"`
		Cwd       string `json:"cwd"`
	}
	if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
		return "", "", false
	}
	if line.Type != "session" {
		return "", "", false
	}
	// A forked transcript keeps the parent's id in "id" and mints its own in
	// "sessionId" (cmd 1.32.2); prefer it so a fork is never mistaken for its
	// parent, whose id is already claimed.
	if sessionIDPattern.MatchString(line.SessionID) {
		return line.SessionID, line.Cwd, true
	}
	if !sessionIDPattern.MatchString(line.ID) {
		return "", "", false
	}
	return line.ID, line.Cwd, true
}

func hermesStateDB() string {
	root := strings.TrimSpace(os.Getenv("HERMES_HOME"))
	if root != "" && filepath.Base(filepath.Dir(root)) == "profiles" {
		return filepath.Join(root, "state.db")
	}
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		root = filepath.Join(home, ".hermes")
	}
	if data, err := os.ReadFile(filepath.Join(root, "active_profile")); err == nil {
		profile := strings.TrimSpace(string(data))
		if profile != "default" && hermesProfilePattern.MatchString(profile) {
			return filepath.Join(root, "profiles", profile, "state.db")
		}
	}
	return filepath.Join(root, "state.db")
}

// captureHermes finds the CLI conversation Hermes created after first input.
// Hermes records exact cwd and Unix creation time in state.db, which
// distinguishes parallel sessions in the same project.
func captureHermes(path, cwd string, launchedAt time.Time, claimed map[string]bool) (string, bool) {
	if path == "" {
		return "", false
	}
	if _, err := os.Stat(path); err != nil {
		return "", false
	}
	dsn := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	query := dsn.Query()
	query.Set("mode", "ro")
	dsn.RawQuery = query.Encode()
	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return "", false
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	rows, err := db.QueryContext(ctx, `
		SELECT id, cwd, started_at
		FROM sessions
		WHERE source = 'cli' AND started_at >= ?
		ORDER BY started_at ASC
		LIMIT 200`, float64(launchedAt.UnixNano())/float64(time.Second))
	if err != nil {
		return "", false
	}
	defer rows.Close()
	wantCwd := resolvePath(cwd)
	var cands []candidate
	for rows.Next() {
		var id string
		var sessionCwd sql.NullString
		var started float64
		if rows.Scan(&id, &sessionCwd, &started) != nil {
			return "", false
		}
		if !sessionIDPattern.MatchString(id) || claimed[id] || !sessionCwd.Valid || resolvePath(sessionCwd.String) != wantCwd {
			continue
		}
		cands = append(cands, candidate{id: id, modTime: time.Unix(0, int64(started*float64(time.Second)))})
	}
	if rows.Err() != nil {
		return "", false
	}
	return pickEarliest(cands)
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

// sessionIDPattern is the shape a conversation id has in every store we
// read: a UUID from codex, gemini and hermes, and opencode's ses_ token.
// The id arrives from a file or database the agent CLI owns rather than
// from us, and it goes on to name a session and reach a command line, so
// one shaped like anything else is left where it was found.
var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// opencodeListIDs returns session ids newest-first from `opencode session
// list` run in cwd. A package variable so tests substitute canned output.
var opencodeListIDs = func(cwd string) ([]string, bool) {
	out, err := runOpencode(cwd, "session", "list")
	if err != nil {
		return nil, false
	}
	return dedupeOrdered(opencodeIDPattern.FindAllString(string(out), -1)), true
}

// A package variable so tests substitute canned output.
var opencodeSessionMeta = func(cwd, id string) (directory string, created, updated time.Time, ok bool) {
	out, err := runOpencodeHead(cwd, "export", id)
	if err != nil {
		return "", time.Time{}, time.Time{}, false
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

// The export leads with a human preamble and is read only as a prefix, so
// the info object is extracted by brace matching rather than decoding the
// whole (possibly truncated) document.
func parseOpencodeExport(out []byte) (directory string, created, updated time.Time, ok bool) {
	obj, found := extractInfoObject(out)
	if !found {
		return "", time.Time{}, time.Time{}, false
	}
	var info struct {
		Directory string `json:"directory"`
		Time      struct {
			Created int64 `json:"created"`
			Updated int64 `json:"updated"`
		} `json:"time"`
	}
	if err := json.Unmarshal(obj, &info); err != nil || info.Directory == "" {
		return "", time.Time{}, time.Time{}, false
	}
	return info.Directory, time.UnixMilli(info.Time.Created), time.UnixMilli(info.Time.Updated), true
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
	return pickEarliest(codexCandidates(root, cwd, launchedAt.Add(-clockSlack), claimed))
}

func codexCandidates(root, cwd string, cutoff time.Time, claimed map[string]bool) []candidate {
	if root == "" {
		return nil
	}
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
	return cands
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
	if line.Type != "session_meta" || !sessionIDPattern.MatchString(line.Payload.SessionID) {
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
		dir, created, _, ok := opencodeSessionMeta(cwd, id)
		if !ok || resolvePath(dir) != wantCwd || created.Before(cutoff) {
			continue
		}
		cands = append(cands, candidate{id: id, modTime: created})
	}
	return pickEarliest(cands)
}

// The write signal is info.time.updated, which advances each time the
// conversation is reopened, so a resumed conversation is the one whose
// update time falls at or after the restart.
func recaptureOpencode(cwd string, cutoff time.Time, claimed map[string]bool) (string, bool) {
	ids, ok := opencodeListIDs(cwd)
	if !ok {
		return "", false
	}
	wantCwd := resolvePath(cwd)
	var cands []candidate
	for i, id := range ids {
		if i >= opencodeScanLimit {
			break
		}
		if claimed[id] {
			continue
		}
		dir, _, updated, ok := opencodeSessionMeta(cwd, id)
		if !ok || resolvePath(dir) != wantCwd || updated.Before(cutoff) {
			continue
		}
		cands = append(cands, candidate{id: id, modTime: updated})
	}
	if len(cands) != 1 {
		return "", false
	}
	return cands[0].id, true
}

func geminiRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".gemini", "tmp")
}

// geminiProjectHash mirrors gemini's per-project key, the hex sha256 of the
// resolved working directory. Session files record it in their header, so a
// conversation can be matched to its directory without relying on the
// per-project folder name, which gemini does not derive consistently.
func geminiProjectHash(cwd string) string {
	sum := sha256.Sum256([]byte(resolvePath(cwd)))
	return hex.EncodeToString(sum[:])
}

// geminiSessionMeta reads the session id and project hash from a gemini
// session file's first line.
func geminiSessionMeta(path string) (id, projectHash string, ok bool) {
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
	var header struct {
		SessionID   string `json:"sessionId"`
		ProjectHash string `json:"projectHash"`
	}
	if err := json.Unmarshal(scanner.Bytes(), &header); err != nil {
		return "", "", false
	}
	if !sessionIDPattern.MatchString(header.SessionID) {
		return "", "", false
	}
	return header.SessionID, header.ProjectHash, true
}

// captureGemini finds the conversation gemini minted for a session launched
// in cwd. A fork via `gemini --session-file` names its own id, so the file
// is located by matching the project hash gemini records for cwd among
// session files written at or after launch.
func captureGemini(root, cwd string, launchedAt time.Time, claimed map[string]bool) (string, bool) {
	return pickEarliest(geminiCandidates(root, cwd, launchedAt.Add(-clockSlack), claimed))
}

func geminiCandidates(root, cwd string, cutoff time.Time, claimed map[string]bool) []candidate {
	if root == "" {
		return nil
	}
	wantHash := geminiProjectHash(cwd)
	var cands []candidate
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasPrefix(name, "session-") || !strings.HasSuffix(name, ".jsonl") {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.ModTime().Before(cutoff) {
			return nil
		}
		id, hash, ok := geminiSessionMeta(path)
		if !ok || hash != wantHash || claimed[id] {
			return nil
		}
		cands = append(cands, candidate{id: id, modTime: info.ModTime()})
		return nil
	})
	return cands
}

// SupportsSessionFile reports whether a session store keeps conversations in
// a file a fork can load, which is what the {session_file} placeholder needs.
// gemini is the only store that does; codex and opencode fork by id.
func SupportsSessionFile(sessionStore string) bool {
	return sessionStore == "gemini"
}

// SessionFile locates the on-disk file holding a conversation, so a fork can
// hand it to a command that loads a file instead of an id (`gemini
// --session-file`). The tool's session_store selects the layout, so a tool
// that stores nothing forkable is refused rather than read as gemini.
func SessionFile(sessionStore, id string) (string, error) {
	if !SupportsSessionFile(sessionStore) {
		return "", fmt.Errorf("session_store %q keeps no session file to fork from", sessionStore)
	}
	return geminiSessionFileIn(geminiRoot(), id)
}

func geminiSessionFileIn(root, id string) (string, error) {
	if root == "" {
		return "", fmt.Errorf("cannot resolve the gemini home directory")
	}
	prefix := id
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}
	var found string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || found != "" {
			return nil
		}
		name := d.Name()
		if !strings.HasPrefix(name, "session-") || !strings.HasSuffix(name, ".jsonl") {
			return nil
		}
		if !strings.Contains(name, prefix) {
			return nil
		}
		if sessionID, _, ok := geminiSessionMeta(path); ok && sessionID == id {
			found = path
		}
		return nil
	})
	if found == "" {
		return "", fmt.Errorf("no gemini session file found for conversation %s", id)
	}
	return found, nil
}
