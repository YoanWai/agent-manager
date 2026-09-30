package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

// prepareMainProcess recognizes tests re-executed by mainTestCommand and
// replaces test flags with the agent-manager arguments following "--".
func prepareMainProcess() bool {
	if os.Getenv("AGENT_MANAGER_MAIN_TEST") != "1" {
		return false
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{"agent-manager"}, os.Args[i+1:]...)
			return true
		}
	}
	return false
}

func mainTestCommand(t *testing.T, args ...string) *exec.Cmd {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)
	commandArgs := []string{"-test.run=^" + t.Name() + "$"}
	if coverDir := testCoverDir(); coverDir != "" {
		commandArgs = append(commandArgs, "-test.gocoverdir="+coverDir)
	}
	commandArgs = append(commandArgs, "--")
	commandArgs = append(commandArgs, args...)
	cmd := exec.CommandContext(ctx, os.Args[0], commandArgs...)
	cmd.Env = append(os.Environ(), "AGENT_MANAGER_MAIN_TEST=1")
	cmd.WaitDelay = time.Second
	return cmd
}

func replaceEnv(env []string, values ...string) []string {
	replacements := make(map[string]string, len(values)/2)
	for i := 0; i < len(values); i += 2 {
		replacements[values[i]] = values[i+1]
	}
	result := make([]string, 0, len(env)+len(replacements))
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		if _, replaced := replacements[key]; !replaced && key != "TMUX" {
			result = append(result, entry)
		}
	}
	for key, value := range replacements {
		result = append(result, key+"="+value)
	}
	return result
}

func testCoverDir() string {
	if coverFlag := flag.Lookup("test.gocoverdir"); coverFlag != nil {
		return coverFlag.Value.String()
	}
	return ""
}

func TestPrintHelpDoesNotRequireATerminal(t *testing.T) {
	var out bytes.Buffer
	if err := printHelp(&out, t.TempDir()); err != nil {
		t.Fatalf("printHelp: %v", err)
	}
	for _, want := range []string{
		"Usage: agent-manager [command]",
		"Run the interactive manager when no command is given.",
		"-h, --help",
		"-v, --version",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("help text does not contain %q:\n%s", want, out.String())
		}
	}
}

type failingHelpWriter struct{}

func (failingHelpWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestPrintHelpReturnsWriteError(t *testing.T) {
	if err := printHelp(failingHelpWriter{}, t.TempDir()); err == nil {
		t.Fatal("printHelp succeeded after the writer failed")
	}
}

func TestMainPrintsHelpWithoutStartingTUI(t *testing.T) {
	if prepareMainProcess() {
		main()
		return
	}
	for _, helpFlag := range []string{"--help", "-h"} {
		t.Run(helpFlag, func(t *testing.T) {
			cmd := mainTestCommand(t, helpFlag)
			home := t.TempDir()
			cmd.Env = replaceEnv(cmd.Env, "HOME", home, "XDG_CONFIG_HOME", filepath.Join(home, ".config"))
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("agent-manager %s: %v\n%s", helpFlag, err, out)
			}
			if !strings.Contains(string(out), "Usage: agent-manager [command]") {
				t.Fatalf("agent-manager %s did not print help:\n%s", helpFlag, out)
			}
		})
	}
}

func TestMainDispatchesNonInteractiveCommands(t *testing.T) {
	if prepareMainProcess() {
		main()
		return
	}
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"version", []string{"--version"}, "agent-manager dev"},
		{"subcommand help", []string{"sessions", "-h"}, "usage: agent-manager sessions"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := mainTestCommand(t, tt.args...).CombinedOutput()
			if err != nil {
				t.Fatalf("agent-manager %s: %v\n%s", strings.Join(tt.args, " "), err, out)
			}
			if !strings.Contains(string(out), tt.want) {
				t.Fatalf("agent-manager %s output does not contain %q:\n%s", strings.Join(tt.args, " "), tt.want, out)
			}
		})
	}
}

func TestMainReportsHeadlessStartupFailure(t *testing.T) {
	if prepareMainProcess() {
		main()
		return
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	root, err := os.MkdirTemp("/tmp", "ammain")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Errorf("remove startup test directory: %v", err)
		}
	})
	for _, dir := range []string{"home", "config", "tmux"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	cmd := mainTestCommand(t)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Env = replaceEnv(cmd.Env,
		"HOME", filepath.Join(root, "home"),
		"XDG_CONFIG_HOME", filepath.Join(root, "config"),
		"TMUX_TMPDIR", filepath.Join(root, "tmux"),
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("agent-manager unexpectedly started without a controlling terminal:\n%s", out)
	}
	if !strings.Contains(string(out), "could not open a new TTY") {
		t.Fatalf("agent-manager reported the wrong startup failure:\n%s", out)
	}
}

// Startup is the only place the alternate-scroll reset goes out. Reading
// the call out of the syntax tree fails if it is dropped, which is what
// would put wheel notches back on the session cursor (#110).
func TestStartupDisablesAlternateScroll(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}
	var run *ast.FuncDecl
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == "run" && fn.Recv == nil {
			run = fn
		}
	}
	if run == nil {
		t.Fatal("main.go has no run function")
	}
	found := false
	ast.Inspect(run, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "DisableAlternateScroll" {
			return true
		}
		if pkg, ok := selector.X.(*ast.Ident); ok && pkg.Name == "ui" {
			found = true
		}
		return true
	})
	if !found {
		t.Fatal("run() never calls ui.DisableAlternateScroll")
	}
}

// Without mouse reporting the terminal keeps the wheel and scrolls the
// manager out of view, which is what alternate scroll used to prevent.
func TestStartupClaimsMouse(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}
	found := false
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "NewProgram" {
			return true
		}
		for _, arg := range call.Args {
			option, ok := arg.(*ast.CallExpr)
			if !ok {
				continue
			}
			name, ok := option.Fun.(*ast.SelectorExpr)
			if ok && name.Sel.Name == "WithMouseCellMotion" {
				found = true
			}
		}
		return true
	})
	if !found {
		t.Fatal("the program starts without mouse reporting")
	}
}

func TestResolveVersion(t *testing.T) {
	cases := []struct {
		label         string
		embedded      string
		moduleVersion string
		hasInfo       bool
		want          string
	}{
		{"ldflags win", "0.11.0", "v0.11.0", true, "0.11.0"},
		{"ldflags win over missing info", "0.11.0", "", false, "0.11.0"},
		{"go install at a tag", devVersion, "v0.11.0", true, "0.11.0"},
		{"pseudo-version", devVersion, "v0.10.6-0.20260730153639-3b5b8a9a5649", true, devVersion},
		{"go run", devVersion, "(devel)", true, devVersion},
		{"empty module version", devVersion, "", true, devVersion},
		{"no build info", devVersion, "", false, devVersion},
		{"two components", devVersion, "v0.11", true, devVersion},
		{"non-numeric", devVersion, "vX.Y.Z", true, devVersion},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			var info *debug.BuildInfo
			if tc.hasInfo {
				info = &debug.BuildInfo{Main: debug.Module{Version: tc.moduleVersion}}
			}
			if got := resolveVersion(tc.embedded, info, tc.hasInfo); got != tc.want {
				t.Fatalf("resolveVersion(%q, %q) = %q, want %q", tc.embedded, tc.moduleVersion, got, tc.want)
			}
		})
	}
}

// isolateTmux points the manager's socket at an empty server, so a test run
// from inside a managed pane cannot find that pane.
func isolateTmux(t *testing.T) {
	t.Helper()
	// Short on purpose: tmux silently falls back to the default socket once
	// TMUX_TMPDIR/tmux-<uid>/<socket> passes 104 characters.
	tmpdir, err := os.MkdirTemp("/tmp", "amcaller")
	if err != nil {
		t.Fatalf("socket dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpdir) })
	t.Setenv("TMUX_TMPDIR", tmpdir)
}

// The environment variable is what an agent's launch exports and what an
// MCP server under Codex is handed, so it wins; a terminal pane, which
// carries neither, names its session through tmux instead.
func TestCallerSessionPrefersTheEnvironmentOverThePane(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	isolateTmux(t)

	driver, err := tmux.New()
	if err != nil {
		t.Fatalf("tmux driver: %v", err)
	}
	id := "ca11ab1e"
	if err := driver.Create(id, "/tmp", "", nil, 80, 24); err != nil {
		t.Fatalf("create the terminal pane: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })
	out, err := exec.Command("tmux", "-L", driver.SocketName(), "display-message", "-p", "-t", tmux.PaneTarget(id), "#{socket_path},#{pid},0 #{pane_id}").CombinedOutput()
	if err != nil {
		t.Fatalf("pane id: %v: %s", err, out)
	}
	tmuxEnv, pane, _ := strings.Cut(strings.TrimSpace(string(out)), " ")
	t.Setenv("TMUX", tmuxEnv)
	t.Setenv("TMUX_PANE", pane)

	t.Setenv(hooks.EnvSessionID, "deadbeef")
	if got := callerSession(); got != "deadbeef" {
		t.Fatalf("callerSession with the variable set = %q, want deadbeef", got)
	}

	t.Setenv(hooks.EnvSessionID, "")
	if got := callerSession(); got != id {
		t.Fatalf("callerSession from the pane = %q, want %q", got, id)
	}
}

// A subcommand run outside tmux, or on a machine without it, has no pane to
// ask, and says which of the two ways to identify a caller failed.
func TestCallerSessionOutsideTmuxLeavesTheCommandToExplainIt(t *testing.T) {
	isolateTmux(t)
	t.Setenv(hooks.EnvSessionID, "")
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")
	if got := callerSession(); got != "" {
		t.Fatalf("callerSession outside tmux = %q, want empty", got)
	}
	_, err := sessioncmd.ReviewScope(t.TempDir(), callerSession(), "branch")
	if err == nil {
		t.Fatal("a command with no caller succeeded")
	}
	for _, want := range []string{"session or terminal", "AGENT_MANAGER_SESSION_ID is unset", "not one Agent Manager runs"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %q", err, want)
		}
	}
}
