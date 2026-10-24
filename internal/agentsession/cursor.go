package agentsession

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Cursor stores a chat under chats/<md5(path)>/<id>/meta.json. path is
// path.resolve of the process cwd, which does not walk symlinks, while the
// cwd a process inherits on macOS is already the real path. Both spellings
// are hashed so a session launched through /tmp still finds a store recorded
// under /private/tmp.
func cursorConfigDir() string {
	if dir := strings.TrimSpace(os.Getenv("CURSOR_CONFIG_DIR")); dir != "" {
		return dir
	}
	if dir := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); dir != "" {
		return filepath.Join(dir, "cursor")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".cursor")
}

func cursorChatBucket(path string) string {
	sum := md5.Sum([]byte(path))
	return hex.EncodeToString(sum[:])
}

func cursorPaths(cwd string) []string {
	var paths []string
	add := func(path string) {
		if path == "" {
			return
		}
		for _, have := range paths {
			if have == path {
				return
			}
		}
		paths = append(paths, path)
	}
	add(cwd)
	if abs, err := filepath.Abs(cwd); err == nil {
		add(abs)
	}
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		add(resolved)
	}
	return paths
}

func cursorBuckets(cwd string) []string {
	seen := map[string]bool{}
	var buckets []string
	for _, path := range cursorPaths(cwd) {
		bucket := cursorChatBucket(path)
		if seen[bucket] {
			continue
		}
		seen[bucket] = true
		buckets = append(buckets, bucket)
	}
	return buckets
}

type cursorChatMeta struct {
	CreatedAtMs int64  `json:"createdAtMs"`
	UpdatedAtMs int64  `json:"updatedAtMs"`
	Cwd         string `json:"cwd"`
}

var errMalformedCursorMeta = errors.New("meta.json is not the object cursor-agent writes")

func readCursorMeta(path string) (cursorChatMeta, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return cursorChatMeta{}, err
	}
	var meta cursorChatMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return cursorChatMeta{}, errMalformedCursorMeta
	}
	return meta, nil
}

func captureCursor(cwd string, launchedAt time.Time, claimed map[string]bool) (string, bool) {
	return captureCandidates(cursorCandidates(cwd, launchedAt.Add(-clockSlack), claimed, false))
}

func snapshotCursor(cwd string) (map[string]int64, bool) {
	return snapshotCandidates(cursorCandidates(cwd, time.Time{}, nil, true))
}

func recaptureCursor(cwd string, snapshot map[string]int64, claimed map[string]bool) []candidate {
	cands, err := cursorCandidates(cwd, time.Time{}, claimed, true)
	return recaptureCandidates(cands, err, snapshot)
}

// byUpdate ranks a chat by updatedAtMs, which is what a resume touches.
// A launch ranks by createdAtMs so the first chat written after it wins.
func cursorCandidates(cwd string, cutoff time.Time, claimed map[string]bool, byUpdate bool) ([]candidate, error) {
	root := cursorConfigDir()
	if root == "" {
		return nil, os.ErrNotExist
	}
	want := resolvePath(cwd)
	chats := filepath.Join(root, "chats")
	var cands []candidate
	for _, bucket := range cursorBuckets(cwd) {
		entries, err := os.ReadDir(filepath.Join(chats, bucket))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			id := entry.Name()
			if !sessionIDPattern.MatchString(id) || claimed[id] {
				continue
			}
			meta, err := readCursorMeta(filepath.Join(chats, bucket, id, "meta.json"))
			if errors.Is(err, fs.ErrNotExist) || errors.Is(err, errMalformedCursorMeta) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if meta.Cwd == "" || meta.CreatedAtMs <= 0 || resolvePath(meta.Cwd) != want {
				continue
			}
			created := time.UnixMilli(meta.CreatedAtMs)
			when := created
			if byUpdate && meta.UpdatedAtMs > 0 {
				when = time.UnixMilli(meta.UpdatedAtMs)
			}
			if !byUpdate && created.Before(cutoff) {
				continue
			}
			cands = append(cands, candidate{id: id, modTime: when})
		}
	}
	return cands, nil
}
