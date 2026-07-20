package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/mcpserver"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
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
	if len(os.Args) > 1 && os.Args[1] == "review-base" {
		dir, err := config.Dir()
		if err == nil {
			err = runReviewBase(os.Args[2:], os.Getenv(hooks.EnvSessionID), dir)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "agent-manager:", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "mcp" {
		dir, err := config.Dir()
		if err == nil {
			err = mcpserver.Run(dir, os.Getenv(hooks.EnvSessionID), version)
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

func runRename(args []string, sessionID, configDir string) error {
	if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
		return fmt.Errorf(`usage: agent-manager rename "<name>"`)
	}
	message, err := sessioncmd.Rename(configDir, sessionID, args[0])
	if err != nil {
		return err
	}
	fmt.Println(message)
	return nil
}

// runReviewRepo records the repo a session is working in, so review opens
// there instead of guessing from the working directory.
func runReviewRepo(args []string, sessionID, configDir string) error {
	if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
		return fmt.Errorf(`usage: agent-manager review-repo <path>`)
	}
	message, err := sessioncmd.ReviewRepo(configDir, sessionID, args[0])
	if err != nil {
		return err
	}
	fmt.Println(message)
	return nil
}

// runReviewBase records the base ref the session's branch diffs against, read
// from the process working directory the agent runs it in. `--clear` writes an
// empty ref line, meaning delete.
func runReviewBase(args []string, sessionID, configDir string) error {
	if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
		return fmt.Errorf(`usage: agent-manager review-base <ref>|--clear`)
	}
	ref := strings.TrimSpace(args[0])
	if ref == "--clear" {
		ref = ""
	}
	message, err := sessioncmd.ReviewBase(configDir, sessionID, ".", ref)
	if err != nil {
		return err
	}
	fmt.Println(message)
	return nil
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
