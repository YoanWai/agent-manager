package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/YoanWai/agent-manager/internal/cli"
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/mcpserver"
	"github.com/YoanWai/agent-manager/internal/notify"
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

	// The macOS notifier bundle runs a copy of this binary; a click on a
	// banner relaunches it with no arguments at all.
	if notify.LaunchedAsHelper() {
		os.Exit(notify.HelperMain(os.Args[1:]))
	}

	if len(os.Args) > 1 {
		if os.Args[1] == "help" || os.Args[1] == "--help" || os.Args[1] == "-h" {
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
				// A subcommand's -h has already printed its usage, and
				// asking for it is not a failure.
				if errors.Is(err, cli.ErrUsageShown) {
					return
				}
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
	_, err := fmt.Fprintln(w, cli.Help(version))
	return err
}

func subcommands() map[string]func(args []string) error {
	table := map[string]func(args []string) error{
		"mcp": withConfigDir(func(args []string, caller func() string, configDir string) error {
			return mcpserver.Run(configDir, caller(), version)
		}),
	}
	for name, command := range cli.Commands(version) {
		table[name] = withConfigDir(command)
	}
	return table
}

func withConfigDir(command cli.Command) func([]string) error {
	return func(args []string) error {
		dir, err := config.Dir()
		if err != nil {
			return err
		}
		return command(args, sync.OnceValue(callerSession), dir)
	}
}

// callerSession identifies the session a subcommand speaks for. The
// environment variable stays authoritative: an MCP server launched under
// Codex is handed that variable and nothing else of the pane's
// environment, so a pane lookup could not stand in for it.
//
// Falling back to the pane is what makes a terminal a caller. A terminal
// opens on the user's shell with no launch command, so tmux gets no launch
// script to export the id from, and the shell would otherwise have no way
// to say which session it is.
//
// The process ancestry is the last resort, for an MCP server Muse starts
// with none of the pane's environment.
func callerSession() string {
	if id := os.Getenv(hooks.EnvSessionID); id != "" {
		return id
	}
	if id := sessionFromPane(); id != "" {
		return id
	}
	return sessionFromAncestry()
}

// sessionFromPane asks the manager's tmux server which session owns the
// pane this command runs in. Every failure answers empty: `update` and the
// rest have to keep working outside tmux, and on a machine without tmux
// installed at all.
func sessionFromPane() string {
	tmuxEnv, pane := os.Getenv("TMUX"), os.Getenv("TMUX_PANE")
	if tmuxEnv == "" || pane == "" {
		return ""
	}
	driver, err := tmux.New()
	if err != nil {
		return ""
	}
	id, err := driver.SessionOfPane(tmuxEnv, pane)
	if err != nil {
		return ""
	}
	return id
}

// Like sessionFromPane, every failure answers empty.
func sessionFromAncestry() string {
	driver, err := tmux.New()
	if err != nil {
		return ""
	}
	id, err := driver.SessionOfProcess(os.Getpid())
	if err != nil {
		return ""
	}
	return id
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
	program := tea.NewProgram(model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
		tea.WithOutput(model.CursorOutput(os.Stdout)),
	)
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
