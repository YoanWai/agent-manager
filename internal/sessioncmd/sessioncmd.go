// Package sessioncmd implements the session-scoped commands an agent uses
// to talk to its running manager: naming the session, declaring review
// targets, and operating managed terminals. The CLI subcommands and the MCP
// server share this layer so validation and behavior stay identical.
package sessioncmd

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/YoanWai/agent-manager/internal/hooks"
)

var sessionIDPattern = regexp.MustCompile(`^[0-9a-f]+$`)
var reviewCommentIDPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

func validSession(sessionID string) error {
	if sessionID == "" {
		return fmt.Errorf("not inside an Agent Manager session or terminal (%s is unset and this pane is not one Agent Manager runs)", hooks.EnvSessionID)
	}
	if !sessionIDPattern.MatchString(sessionID) {
		return fmt.Errorf("invalid session id %q", sessionID)
	}
	return nil
}

// Both sides are resolved first because git reports a toplevel with symlinks expanded.
func pathWithin(path, root string) bool {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	return path == root || strings.HasPrefix(path, root+string(filepath.Separator))
}
