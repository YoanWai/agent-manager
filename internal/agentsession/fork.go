package agentsession

import (
	"encoding/json"
	"fmt"
	"strings"
)

// MintsForkID reports whether a session store makes a fork of a conversation
// itself and names it, which is what a fork_command with {new_id} and no
// {id} needs. opencode's interactive command cannot fork, so its API is
// asked and the copy is opened by id.
func MintsForkID(sessionStore string) bool {
	return sessionStore == "opencode"
}

// Fork copies the conversation sourceID through the store's own tool and
// returns the copy's id.
func Fork(sessionStore, cwd, sourceID string) (string, error) {
	if !MintsForkID(sessionStore) {
		return "", fmt.Errorf("session_store %q makes no fork of its own", sessionStore)
	}
	out, err := runOpencode(cwd, "api", "session.fork", "--standalone", "--param", "sessionID="+sourceID, "--data", "{}")
	if err != nil {
		return "", fmt.Errorf("opencode could not fork %s: %w", sourceID, err)
	}
	var answer struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &answer); err != nil {
		return "", fmt.Errorf("opencode answered the fork of %s with %q: %w", sourceID, strings.TrimSpace(string(out)), err)
	}
	if !strings.HasPrefix(answer.Data.ID, "ses_") || !ValidSessionID(answer.Data.ID) {
		return "", fmt.Errorf("opencode named the fork of %s %q", sourceID, answer.Data.ID)
	}
	return answer.Data.ID, nil
}
