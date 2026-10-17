package agentsession

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func snapshotGemini(root, cwd string) (map[string]int64, bool) {
	return snapshotCandidates(geminiCandidates(root, cwd, time.Time{}, nil))
}

func recaptureGemini(root, cwd string, snapshot map[string]int64, claimed map[string]bool) []candidate {
	cands, err := geminiCandidates(root, cwd, time.Time{}, claimed)
	return recaptureCandidates(cands, err, snapshot)
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
func geminiSessionMeta(path string) (id, projectHash string, err error) {
	first, err := firstLine(path)
	if err != nil {
		return "", "", err
	}
	var header struct {
		SessionID   string `json:"sessionId"`
		ProjectHash string `json:"projectHash"`
	}
	if json.Unmarshal(first, &header) != nil || !sessionIDPattern.MatchString(header.SessionID) {
		return "", "", nil
	}
	return header.SessionID, header.ProjectHash, nil
}

// captureGemini finds the conversation gemini minted for a session launched
// in cwd. A fork via `gemini --session-file` names its own id, so the file
// is located by matching the project hash gemini records for cwd among
// session files written at or after launch.
func captureGemini(root, cwd string, launchedAt time.Time, claimed map[string]bool) (string, bool) {
	return captureCandidates(geminiCandidates(root, cwd, launchedAt.Add(-clockSlack), claimed))
}

func geminiCandidates(root, cwd string, cutoff time.Time, claimed map[string]bool) ([]candidate, error) {
	if root == "" {
		return nil, os.ErrNotExist
	}
	wantHash := geminiProjectHash(cwd)
	var cands []candidate
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasPrefix(name, "session-") || !strings.HasSuffix(name, ".jsonl") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.ModTime().Before(cutoff) {
			return nil
		}
		id, hash, err := geminiSessionMeta(path)
		if err != nil {
			return err
		}
		if id == "" || hash != wantHash || claimed[id] {
			return nil
		}
		cands = append(cands, candidate{id: id, modTime: info.ModTime()})
		return nil
	})
	return cands, err
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
		if sessionID, _, _ := geminiSessionMeta(path); sessionID == id {
			found = path
		}
		return nil
	})
	if found == "" {
		return "", fmt.Errorf("no gemini session file found for conversation %s", id)
	}
	return found, nil
}
