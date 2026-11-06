package agentsession

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

const kiroCLITimeout = 15 * time.Second

// kiroWorkspace is one envelope of `kiro-cli chat --list-sessions -f json`:
// the sessions Kiro keeps for one working directory.
type kiroWorkspace struct {
	Cwd      string        `json:"cwd"`
	Sessions []kiroSession `json:"sessions"`
}

type kiroSession struct {
	SessionID string    `json:"sessionId"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// The list is scoped to the calling directory, so it runs in the session's
// cwd. A package variable so tests substitute canned output.
var kiroSessionList = func(cwd string, env Env) ([]kiroWorkspace, error) {
	ctx, cancel := context.WithTimeout(context.Background(), kiroCLITimeout)
	defer cancel()
	cmd := env.command(ctx, "kiro-cli", "chat", "--list-sessions", "-f", "json")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("kiro-cli chat --list-sessions: %w", err)
	}
	return parseKiroSessions(out)
}

func parseKiroSessions(out []byte) ([]kiroWorkspace, error) {
	var workspaces []kiroWorkspace
	if err := json.Unmarshal(out, &workspaces); err != nil {
		return nil, fmt.Errorf("kiro-cli chat --list-sessions printed no session list: %w", err)
	}
	return workspaces, nil
}

// kiroCandidates ranks each session by updatedAt, which Kiro sets when it
// lists a new session and moves with every turn. The list carries no
// creation time.
func kiroCandidates(cwd string, env Env, cutoff time.Time, claimed map[string]bool) ([]candidate, error) {
	workspaces, err := kiroSessionList(cwd, env)
	if err != nil {
		return nil, err
	}
	want := resolvePath(cwd)
	var cands []candidate
	for _, workspace := range workspaces {
		if resolvePath(workspace.Cwd) != want {
			continue
		}
		for _, session := range workspace.Sessions {
			if !sessionIDPattern.MatchString(session.SessionID) || claimed[session.SessionID] || session.UpdatedAt.Before(cutoff) {
				continue
			}
			cands = append(cands, candidate{id: session.SessionID, modTime: session.UpdatedAt})
		}
	}
	return cands, nil
}

// captureKiro takes the earliest unclaimed session listed for cwd since
// the launch. Kiro lists it once the workspace trust gate is accepted,
// before the first prompt.
func captureKiro(cwd string, launchedAt time.Time, claimed map[string]bool) (string, bool) {
	return captureCandidates(kiroCandidates(cwd, managerEnv, launchedAt.Add(-clockSlack), claimed))
}

func snapshotKiro(cwd string, env Env) (map[string]int64, bool) {
	return snapshotCandidates(kiroCandidates(cwd, env, time.Time{}, nil))
}

func recaptureKiro(cwd string, snapshot map[string]int64, claimed map[string]bool) []candidate {
	cands, err := kiroCandidates(cwd, managerEnv, time.Time{}, claimed)
	return recaptureCandidates(cands, err, snapshot)
}
