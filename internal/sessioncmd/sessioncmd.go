// Package sessioncmd implements the session-scoped commands an agent uses
// to talk to its running manager: naming the session, declaring review
// targets, and operating managed terminals. The CLI subcommands and the MCP
// server share this layer so validation and behavior stay identical.
package sessioncmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
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

// The manager polls these files, so one lands complete or not at all: a
// half-written name reads as a rename to nothing.
func writeMailbox(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return hooks.WriteWhole(path, content)
}

// A rename is applied by the manager's poll, so the answer arrives one
// interval later. The floor covers a poll that ran long, and the ceiling
// keeps a generous poll_interval from parking the agent's tool call.
const (
	renameWaitFloor  = 10 * time.Second
	renameWaitCap    = 30 * time.Second
	renameResultPoll = 100 * time.Millisecond
)

// Rename waits for the manager's poll because the manager owns the
// database and the tmux label: a name it cannot give the session comes
// back as the reason, and a name no manager is running to apply is
// reported as queued rather than as done.
func Rename(ctx context.Context, configDir, sessionID, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("name is empty")
	}
	if err := validSession(sessionID); err != nil {
		return "", err
	}
	// Whether anyone is home is read before the name is queued, so a
	// manager that cannot be reached at all is reported instead of a name
	// left pending behind an error.
	awake, pollInterval, err := managerAwake(configDir)
	if err != nil {
		return "", err
	}
	mailbox := hooks.NewManager(configDir)
	request, err := hooks.NewRequestID()
	if err != nil {
		return "", err
	}
	if err := writeMailbox(mailbox.NameFile(sessionID), hooks.NameRequest(request, name)); err != nil {
		return "", err
	}
	if !awake {
		return fmt.Sprintf("rename to %q is queued: no Agent Manager is running to apply it; it takes effect when one opens", name), nil
	}
	// The manager answers the name as it will take it, so the wait
	// recognizes its own answer by that name rather than the typed one.
	asked := hooks.NormalizeName(name)
	deadline := time.Now().Add(min(max(3*pollInterval, renameWaitFloor), renameWaitCap))
	for time.Now().Before(deadline) {
		verdict, found, err := mailbox.ReadNameResult(sessionID, request)
		if err != nil {
			return "", fmt.Errorf("read the rename answer: %w", err)
		}
		if !found || verdict.Requested != asked {
			// A verdict for another rename belongs to whoever asked for it.
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(renameResultPoll):
			}
			continue
		}
		if err := mailbox.RemoveNameResult(sessionID, request); err != nil {
			return "", err
		}
		if verdict.Refusal != nil {
			return "", fmt.Errorf("session keeps its name: %w", verdict.Refusal)
		}
		return "session renamed to " + verdict.Applied, nil
	}
	return fmt.Sprintf("rename to %q is queued: Agent Manager is running but has not applied it yet", name), nil
}

func managerAwake(configDir string) (bool, time.Duration, error) {
	cfg, err := config.LoadDir(configDir)
	if err != nil {
		return false, 0, err
	}
	st, err := store.Open(filepath.Join(configDir, "state.db"))
	if err != nil {
		return false, 0, err
	}
	defer st.Close()
	awake, err := st.ManagerAwake(time.Now(), cfg.PollInterval.Duration)
	return awake, cfg.PollInterval.Duration, err
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
