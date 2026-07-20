// Package sessioncmd implements the session-scoped commands an agent uses
// to talk to its running manager: naming the session and declaring review
// targets. The CLI subcommands and the MCP server both call these, so
// validation and behavior stay identical. Each command writes a mailbox
// file the manager poller applies on its next cycle.
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
)

var sessionIDPattern = regexp.MustCompile(`^[0-9a-f]+$`)

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
