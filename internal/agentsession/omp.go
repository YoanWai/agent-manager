package agentsession

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ompRoot is where omp keeps one directory of session files per working
// directory: ~/.omp/agent/sessions, or $PI_CODING_AGENT_DIR/sessions.
func ompRoot() string {
	agentDir := os.Getenv("PI_CODING_AGENT_DIR")
	if agentDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		agentDir = filepath.Join(home, ".omp", "agent")
	}
	return filepath.Join(agentDir, "sessions")
}

// captureOmp reads the breadcrumb omp keeps for the terminal it runs in,
// terminal-sessions/<tty> next to the sessions root ("/dev/pts/3" is
// "pts-3"): the cwd, then the session file, then "fresh" until that file is
// written. omp writes a session file only once the first reply lands, so in
// a shared directory the first file to appear can belong to a later launch;
// the breadcrumb names this pane's own. It counts only when written at or
// after launch, with no clock slack: a pane can inherit a tty number from one
// that just closed, and that pane's breadcrumb predates this launch. omp
// writes it after the manager stamps the launch, on the same clock. The
// session it names may be older than the launch, when the user opened an
// existing conversation. Without a usable breadcrumb nothing is bound, and
// revive falls back to omp's picker.
func captureOmp(root, cwd, terminal string, launchedAt time.Time, claimed map[string]bool) (string, bool) {
	name, ok := strings.CutPrefix(terminal, "/dev/")
	if root == "" || !ok || name == "" {
		return "", false
	}
	crumb := filepath.Join(filepath.Dir(root), "terminal-sessions", strings.ReplaceAll(name, "/", "-"))
	info, err := os.Stat(crumb)
	if err != nil || info.ModTime().Before(launchedAt) {
		return "", false
	}
	data, err := os.ReadFile(crumb)
	if err != nil {
		return "", false
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) < 2 || resolvePath(lines[0]) != resolvePath(cwd) || lines[1] == "" {
		return "", false
	}
	for _, extra := range lines[2:] {
		if extra == "fresh" {
			return "", false
		}
	}
	id, sessionCwd, _, err := ompMeta(lines[1])
	if err != nil || id == "" || resolvePath(sessionCwd) != resolvePath(cwd) || claimed[id] {
		return "", false
	}
	return id, true
}

func snapshotOmp(root, cwd string) (map[string]int64, bool) {
	return snapshotCandidates(ompCandidates(root, cwd, time.Time{}, nil))
}

func recaptureOmp(root, cwd string, snapshot map[string]int64, claimed map[string]bool) []candidate {
	cands, err := ompCandidates(root, cwd, time.Time{}, claimed)
	return recaptureCandidates(cands, err, snapshot)
}

// ompCandidates reads the session files one level under each directory in
// root. The directory next to each file with the same stem holds that
// session's artifacts, subagent logs included, so it is not descended into.
// omp writes a session file only once the first reply lands, so a fresh
// launch has nothing to capture until then.
func ompCandidates(root, cwd string, cutoff time.Time, claimed map[string]bool) ([]candidate, error) {
	if root == "" {
		return nil, os.ErrNotExist
	}
	dirs, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	wantCwd := resolvePath(cwd)
	var cands []candidate
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(root, dir.Name()))
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				return nil, err
			}
			if info.ModTime().Before(cutoff) {
				continue
			}
			id, sessionCwd, created, err := ompMeta(filepath.Join(root, dir.Name(), entry.Name()))
			if err != nil {
				return nil, err
			}
			if id == "" || created.Before(cutoff) || resolvePath(sessionCwd) != wantCwd || claimed[id] {
				continue
			}
			cands = append(cands, candidate{id: id, modTime: info.ModTime()})
		}
	}
	return cands, nil
}

// ompMeta reads the session header. omp 18 opens every file with a fixed
// title slot record and writes the header second, so the first two lines
// are read and the one typed "session" wins.
func ompMeta(path string) (id, cwd string, created time.Time, err error) {
	file, err := os.Open(path)
	if err != nil {
		return "", "", time.Time{}, err
	}
	defer func() {
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
	}()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for i := 0; i < 2 && scanner.Scan(); i++ {
		var header struct {
			Type      string `json:"type"`
			ID        string `json:"id"`
			Cwd       string `json:"cwd"`
			Timestamp string `json:"timestamp"`
		}
		if json.Unmarshal(scanner.Bytes(), &header) != nil || header.Type != "session" {
			continue
		}
		stamp, parseErr := time.Parse(time.RFC3339Nano, header.Timestamp)
		if parseErr != nil || !sessionIDPattern.MatchString(header.ID) || header.Cwd == "" {
			return "", "", time.Time{}, nil
		}
		return header.ID, header.Cwd, stamp, nil
	}
	return "", "", time.Time{}, scanErr(scanner)
}
