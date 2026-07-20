package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/YoanWai/agent-manager/internal/ui"
	tea "github.com/charmbracelet/bubbletea"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Println("agent-manager", version)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "rename" {
		if err := renameCommand(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "agent-manager:", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "review-repo" {
		dir, err := config.Dir()
		if err == nil {
			err = runReviewRepo(os.Args[2:], os.Getenv(hooks.EnvSessionID), dir)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "agent-manager:", err)
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "agent-manager:", err)
		os.Exit(1)
	}
}

func renameCommand(args []string) error {
	dir, err := config.Dir()
	if err != nil {
		return err
	}
	return runRename(args, os.Getenv(hooks.EnvSessionID), dir)
}

var sessionIDPattern = regexp.MustCompile(`^[0-9a-f]+$`)

// runRename records a session's self-chosen name for the running manager
// to apply on its next poll. It only writes the name file; the manager
// owns the database and the tmux label.
func runRename(args []string, sessionID, configDir string) error {
	if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
		return fmt.Errorf(`usage: agent-manager rename "<name>"`)
	}
	if sessionID == "" {
		return fmt.Errorf("not inside an agent-manager session (%s is unset)", hooks.EnvSessionID)
	}
	if !sessionIDPattern.MatchString(sessionID) {
		return fmt.Errorf("invalid session id %q", sessionID)
	}
	path := hooks.NewManager(configDir).NameFile(sessionID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(args[0]), 0o644); err != nil {
		return err
	}
	fmt.Println("session renamed to", strings.TrimSpace(args[0]))
	return nil
}

// runReviewRepo records the repo a session is working in, so review opens
// there instead of guessing from the working directory.
func runReviewRepo(args []string, sessionID, configDir string) error {
	if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
		return fmt.Errorf(`usage: agent-manager review-repo <path>`)
	}
	if sessionID == "" {
		return fmt.Errorf("not inside an agent-manager session (%s is unset)", hooks.EnvSessionID)
	}
	if !sessionIDPattern.MatchString(sessionID) {
		return fmt.Errorf("invalid session id %q", sessionID)
	}
	driver, err := git.New()
	if err != nil {
		return err
	}
	target := strings.TrimSpace(args[0])
	abs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	roots, err := driver.ResolveRepos(abs)
	if err != nil {
		if errors.Is(err, git.ErrNotARepo) {
			return fmt.Errorf("%s is not inside a git repository", target)
		}
		return err
	}
	root := roots[0]
	// ResolveRepos also discovers repos nested under a non-repo umbrella and
	// ranks them, which would silently record a guess instead of a declaration.
	if !pathWithin(abs, root) {
		return fmt.Errorf("%s is not inside a git repository", target)
	}
	path := hooks.NewManager(configDir).ReviewRepoFile(sessionID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(root), 0o644); err != nil {
		return err
	}
	fmt.Println("review repo set to", root)
	return nil
}

// pathWithin reports whether path is root or sits under it. Both sides are
// resolved first because git reports a toplevel with symlinks expanded, which
// on macOS turns /var into /private/var.
func pathWithin(path, root string) bool {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	return path == root || strings.HasPrefix(path, root+string(filepath.Separator))
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	driver, err := tmux.New()
	if err != nil {
		return err
	}

	engine, err := status.NewEngine(cfg)
	if err != nil {
		return err
	}

	dir, err := config.Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	st, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		return err
	}
	defer st.Close()

	model := ui.New(cfg, st, driver, engine, hooks.NewManager(dir))
	program := tea.NewProgram(model, tea.WithAltScreen())
	model.StartPoller(program.Send)
	_, err = program.Run()
	return err
}
