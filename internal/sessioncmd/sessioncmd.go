// Package sessioncmd implements the session-scoped commands an agent uses
// to talk to its running manager: naming the session, declaring review
// targets, and operating managed terminals. The CLI subcommands and the MCP
// server share this layer so validation and behavior stay identical.
package sessioncmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/store"
)

var sessionIDPattern = regexp.MustCompile(`^[0-9a-f]+$`)
var reviewCommentIDPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

func validSession(sessionID string) error {
	if sessionID == "" {
		return fmt.Errorf("not inside an agent-manager session (%s is unset)", hooks.EnvSessionID)
	}
	if !sessionIDPattern.MatchString(sessionID) {
		return fmt.Errorf("invalid session id %q", sessionID)
	}
	return nil
}

func writeMailbox(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// Rename records a session's self-chosen name for the running manager to
// apply on its next poll. It only writes the name file; the manager owns
// the database and the tmux label.
func Rename(configDir, sessionID, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("name is empty")
	}
	if err := validSession(sessionID); err != nil {
		return "", err
	}
	if err := writeMailbox(hooks.NewManager(configDir).NameFile(sessionID), name); err != nil {
		return "", err
	}
	return "session renamed to " + name, nil
}

// ReviewRepo records the repo a session is working in, so review opens
// there instead of guessing from the working directory.
func ReviewRepo(configDir, sessionID, target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", errors.New("path is empty")
	}
	if err := validSession(sessionID); err != nil {
		return "", err
	}
	driver, err := git.New()
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	roots, err := driver.ResolveRepos(abs)
	if err != nil {
		if errors.Is(err, git.ErrNotARepo) {
			return "", fmt.Errorf("%s is not inside a git repository", target)
		}
		return "", err
	}
	root := roots[0]
	// ResolveRepos also discovers repos nested under a non-repo umbrella and
	// ranks them, which would silently record a guess instead of a declaration.
	if !pathWithin(abs, root) {
		return "", fmt.Errorf("%s is not inside a git repository", target)
	}
	if err := writeMailbox(hooks.NewManager(configDir).ReviewRepoFile(sessionID), root); err != nil {
		return "", err
	}
	return "review repo set to " + root, nil
}

// ReviewBase records the base ref the session's branch scope diffs against,
// resolved in the repo holding cwd. An empty ref clears the override.
func ReviewBase(configDir, sessionID, cwd, ref string) (string, error) {
	if err := validSession(sessionID); err != nil {
		return "", err
	}
	driver, err := git.New()
	if err != nil {
		return "", err
	}
	repo, err := driver.OpenRepo(cwd)
	if err != nil {
		if errors.Is(err, git.ErrNotARepo) {
			return "", errors.New("not inside a git repository")
		}
		return "", err
	}
	ref = strings.TrimSpace(ref)
	if ref != "" {
		if err := driver.ResolveRef(repo.Root, ref); err != nil {
			return "", err
		}
	}
	if err := writeMailbox(hooks.NewManager(configDir).ReviewBaseFile(sessionID), repo.Root+"\n"+ref+"\n"); err != nil {
		return "", err
	}
	if ref == "" {
		return "review base cleared for " + repo.Root, nil
	}
	return "review base set to " + ref, nil
}

var validScopes = map[string]bool{
	"uncommitted": true,
	"branch":      true,
	"last_commit": true,
	"staged":      true,
}

// ReviewScope records the diff scope the review screen should open with
// for this session: uncommitted, branch (vs target), last_commit, or staged.
func ReviewScope(configDir, sessionID, scope string) (string, error) {
	scope = strings.TrimSpace(scope)
	if !validScopes[scope] {
		return "", fmt.Errorf("unknown scope %q (uncommitted, branch, last_commit, staged)", scope)
	}
	if err := validSession(sessionID); err != nil {
		return "", err
	}
	if err := writeMailbox(hooks.NewManager(configDir).ReviewScopeFile(sessionID), scope); err != nil {
		return "", err
	}
	return "review scope set to " + scope, nil
}

// Status changes leave the review history intact.
func ReviewComment(configDir, sessionID, commentID string, handled bool) (string, error) {
	if err := validSession(sessionID); err != nil {
		return "", err
	}
	commentID = strings.TrimSpace(commentID)
	if !reviewCommentIDPattern.MatchString(commentID) {
		return "", fmt.Errorf("invalid review comment id %q", commentID)
	}
	st, err := store.Open(filepath.Join(configDir, "state.db"))
	if err != nil {
		return "", err
	}
	found, err := st.SetReviewCommentHandled(sessionID, commentID, handled)
	closeErr := st.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	if !found {
		return "", fmt.Errorf("review comment %s was not found for this session", commentID)
	}
	message := "review comment " + commentID + " reopened"
	if handled {
		message = "review comment " + commentID + " marked handled"
	}
	return message, nil
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
