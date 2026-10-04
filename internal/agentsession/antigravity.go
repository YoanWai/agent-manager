package agentsession

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

func antigravityRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".gemini", "antigravity-cli")
}

func captureAntigravity(root, cwd string, launchedAt time.Time, claimed map[string]bool) (string, bool) {
	return captureCandidates(antigravityCandidates(root, cwd, launchedAt.Add(-clockSlack), claimed))
}

func snapshotAntigravity(root, cwd string) (map[string]int64, bool) {
	return snapshotCandidates(antigravityCandidates(root, cwd, time.Time{}, nil))
}

func recaptureAntigravity(root, cwd string, snapshot map[string]int64, claimed map[string]bool) []candidate {
	cands, err := antigravityCandidates(root, cwd, time.Time{}, claimed)
	return recaptureCandidates(cands, err, snapshot)
}

// antigravityCandidates reads agy's own conversation index. A row is written
// when the first prompt is sent, with a zero last_modified_time until that
// turn ends, so a conversation becomes a candidate once it has a turn behind
// it. Subagent conversations share the workspace and are left out.
func antigravityCandidates(root, cwd string, cutoff time.Time, claimed map[string]bool) ([]candidate, error) {
	if root == "" {
		return nil, os.ErrNotExist
	}
	wantCwd := resolvePath(cwd)
	startedInCwd, err := antigravityStartedIn(root, wantCwd)
	if err != nil {
		return nil, err
	}
	db, err := openReadOnly(filepath.Join(root, "conversation_summaries.db"))
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	rows, err := db.QueryContext(ctx, `
		SELECT conversation_id, workspace_uris, last_modified_time
		FROM conversation_summaries
		WHERE parent_conversation_id = '' AND nesting_depth = 0`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var cands []candidate
	for rows.Next() {
		var id, workspaces string
		var modified time.Time
		if err := rows.Scan(&id, &workspaces, &modified); err != nil {
			return nil, err
		}
		if !sessionIDPattern.MatchString(id) || claimed[id] || modified.IsZero() || modified.Before(cutoff) {
			continue
		}
		if !antigravityRanIn(workspaces, id, wantCwd, startedInCwd) {
			continue
		}
		cands = append(cands, candidate{id: id, modTime: modified})
	}
	return cands, rows.Err()
}

// antigravityRanIn reports whether a conversation belongs to cwd. A launch
// records its cwd as the only workspace; one given --add-dir lists every
// directory sorted, with nothing marking the cwd, so it matches none. A
// conversation that started behind the trust prompt records no workspace
// at all, and only agy's own map from each directory to the conversation
// it last started there ties it to cwd.
func antigravityRanIn(workspaces, id, cwd string, startedInCwd map[string]bool) bool {
	if workspaces == "" {
		return startedInCwd[id]
	}
	var uris []string
	if json.Unmarshal([]byte(workspaces), &uris) != nil || len(uris) != 1 {
		return false
	}
	parsed, err := url.Parse(uris[0])
	if err != nil || parsed.Scheme != "file" {
		return false
	}
	return resolvePath(parsed.Path) == cwd
}

// antigravityStartedIn is the conversation agy last started in cwd, once
// per spelling of the directory agy keyed it under.
func antigravityStartedIn(root, cwd string) (map[string]bool, error) {
	data, err := os.ReadFile(filepath.Join(root, "cache", "last_conversations.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var byDir map[string]string
	if err := json.Unmarshal(data, &byDir); err != nil {
		return nil, err
	}
	started := map[string]bool{}
	for dir, id := range byDir {
		if resolvePath(dir) == cwd {
			started[id] = true
		}
	}
	return started, nil
}
