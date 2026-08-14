package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/mcpserver"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/YoanWai/agent-manager/internal/ui"
	"github.com/YoanWai/agent-manager/internal/update"
	tea "github.com/charmbracelet/bubbletea"
)

const devVersion = "dev"

var version = devVersion

// A packager building from source can mark the binary as externally
// managed with -X main.buildSource=Homebrew, which disables self-update.
// No official channel sets it; released binaries rely on the install-path
// check in internal/update instead.
var buildSource = ""

// resolveVersion falls back to the module version so `go install` builds, which
// carry no ldflags, report the tag they came from. Pseudo-versions stay "dev":
// they name a commit rather than a release, and the update check compares
// against release tags.
func resolveVersion(embedded string, info *debug.BuildInfo, ok bool) string {
	if embedded != devVersion {
		return embedded
	}
	if !ok || info == nil {
		return devVersion
	}
	moduleVersion := strings.TrimPrefix(info.Main.Version, "v")
	if strings.ContainsAny(moduleVersion, "-+") || strings.Count(moduleVersion, ".") != 2 {
		return devVersion
	}
	for _, field := range strings.Split(moduleVersion, ".") {
		if _, err := strconv.Atoi(field); err != nil {
			return devVersion
		}
	}
	return moduleVersion
}

func main() {
	info, hasInfo := debug.ReadBuildInfo()
	version = resolveVersion(version, info, hasInfo)
	update.SetBuildSource(buildSource)

	if len(os.Args) > 1 {
		if os.Args[1] == "--help" || os.Args[1] == "-h" {
			if err := printHelp(os.Stdout); err != nil {
				fmt.Fprintln(os.Stderr, "agent-manager:", err)
				os.Exit(1)
			}
			return
		}
		if os.Args[1] == "--version" || os.Args[1] == "-v" {
			fmt.Println("agent-manager", version)
			return
		}
		if command, ok := subcommands()[os.Args[1]]; ok {
			if err := command(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, "agent-manager:", err)
				os.Exit(1)
			}
			return
		}
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "agent-manager:", err)
		os.Exit(1)
	}
}

func printHelp(w io.Writer) error {
	_, err := fmt.Fprint(w, `Usage: agent-manager [command]

Run the interactive manager when no command is given.

Commands:
  mcp          Run the Model Context Protocol server for an agent session
  rename       Rename the current agent session
  review-repo  Record the repository for the current agent session
  review-base  Record the base ref for the current agent session

Options:
  -h, --help     Show this help text
  -v, --version  Print the installed version
`)
	return err
}

func subcommands() map[string]func(args []string) error {
	return map[string]func(args []string) error{
		"rename":      withConfigDir(runRename),
		"review-repo": withConfigDir(runReviewRepo),
		"review-base": withConfigDir(runReviewBase),
		"mcp": withConfigDir(func(args []string, sessionID, configDir string) error {
			return mcpserver.Run(configDir, sessionID, version)
		}),
	}
}

func withConfigDir(command func(args []string, sessionID, configDir string) error) func([]string) error {
	return func(args []string) error {
		dir, err := config.Dir()
		if err != nil {
			return err
		}
		return command(args, os.Getenv(hooks.EnvSessionID), dir)
	}
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

	model := ui.New(cfg, st, driver, engine, hooks.NewManager(dir), version)
	// Mouse reporting claims the wheel for the app, so a notch neither
	// scrolls the host's scrollback out from under the manager nor arrives
	// as an arrow key that walks the session cursor. Alternate scroll is
	// cleared too: a crashed earlier run can leave it set.
	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if err := ui.DisableAlternateScroll(); err != nil {
		return err
	}
	// The terminal's own background follows the theme while the manager
	// runs, so window padding outside the cell grid matches the frame —
	// through tmux's passthrough envelope when a multiplexer is hosting us.
	ui.EnableTerminalPassthrough()
	ui.SyncTerminalBackground()
	model.StartPoller(program.Send)
	final, runErr := program.Run()
	ui.ResetTerminalBackground()
	if runErr == nil {
		if finished, ok := final.(*ui.Model); ok && finished.RestartPath() != "" {
			// A self-update swapped the binary on disk; exec replaces this
			// process with the new build so the manager comes back updated
			// without touching the tmux sessions it manages.
			return syscall.Exec(finished.RestartPath(), os.Args, os.Environ())
		}
	}
	return runErr
}
