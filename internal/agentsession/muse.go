package agentsession

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"
)

func museRoot() string {
	root := os.Getenv("XDG_DATA_HOME")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		root = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(root, "muse", "sessions")
}

func captureMuse(root, cwd string, launchedAt time.Time, claimed map[string]bool) (string, bool) {
	return captureCandidates(museCandidates(root, cwd, launchedAt.Add(-clockSlack), claimed))
}

func snapshotMuse(root, cwd string) (map[string]int64, bool) {
	return snapshotCandidates(museCandidates(root, cwd, time.Time{}, nil))
}

func recaptureMuse(root, cwd string, snapshot map[string]int64, claimed map[string]bool) []candidate {
	cands, err := museCandidates(root, cwd, time.Time{}, claimed)
	return recaptureCandidates(cands, err, snapshot)
}

func museCandidates(root, cwd string, cutoff time.Time, claimed map[string]bool) ([]candidate, error) {
	if root == "" {
		return nil, os.ErrNotExist
	}
	wantCwd := resolvePath(cwd)
	var cands []candidate
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == "subagent" {
			return filepath.SkipDir
		}
		if d.IsDir() || d.Name() != "session.jsonl" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.ModTime().Before(cutoff) {
			return nil
		}
		id, workspace, created, err := museMeta(path)
		if err != nil {
			return err
		}
		if id == "" || created.Before(cutoff) || resolvePath(workspace) != wantCwd || claimed[id] {
			return nil
		}
		cands = append(cands, candidate{id: id, modTime: info.ModTime()})
		return nil
	})
	return cands, err
}

// Permission records can precede the session metadata.
func museMeta(path string) (id, cwd string, created time.Time, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", time.Time{}, err
	}
	defer func() {
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
	}()
	scanner := bufio.NewScanner(io.LimitReader(f, 1024*1024))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		var record struct {
			Stream struct {
				Kind string `json:"kind"`
				ID   string `json:"id"`
			} `json:"stream"`
			RecordedAt  int64  `json:"recorded_at"`
			PayloadType string `json:"payload_type"`
			Payload     struct {
				Record struct {
					Workspace string `json:"workspace_root"`
				} `json:"record"`
			} `json:"payload"`
		}
		if json.Unmarshal(scanner.Bytes(), &record) != nil {
			continue
		}
		if record.PayloadType != "runtime.session.metadata" {
			continue
		}
		if record.Stream.Kind != "session" || record.Stream.ID == "" || record.Payload.Record.Workspace == "" || record.RecordedAt <= 0 {
			return "", "", time.Time{}, nil
		}
		return record.Stream.ID, record.Payload.Record.Workspace, time.UnixMicro(record.RecordedAt), nil
	}
	return "", "", time.Time{}, scanErr(scanner)
}
