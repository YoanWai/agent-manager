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
		id, workspace, created, ok := museMeta(path)
		if !ok || created.Before(cutoff) || resolvePath(workspace) != wantCwd || claimed[id] {
			return nil
		}
		cands = append(cands, candidate{id: id, modTime: info.ModTime()})
		return nil
	})
	return cands, err
}

// Permission records precede metadata. Bound the scan so an incomplete or
// incompatible log never makes the poller read an entire conversation.
func museMeta(path string) (id, cwd string, created time.Time, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", time.Time{}, false
	}
	defer f.Close()
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
			return "", "", time.Time{}, false
		}
		return record.Stream.ID, record.Payload.Record.Workspace, time.UnixMicro(record.RecordedAt), true
	}
	return "", "", time.Time{}, false
}
