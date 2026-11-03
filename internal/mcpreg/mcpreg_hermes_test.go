package mcpreg

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

func TestApplyHermesSkipsWhenMarkerMatches(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mcp-hermes-registered"), []byte("/opt/bin/agent-manager"), 0o644); err != nil {
		t.Fatal(err)
	}
	command, err := Apply("hermes", "/opt/bin/agent-manager", dir, "hermes --cli", map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if command != "hermes --cli" {
		t.Fatalf("command = %q", command)
	}
}

func TestValidHermesMCPEntry(t *testing.T) {
	enabled := true
	entry := hermesMCPEntry{
		Command: "/opt/bin/agent-manager",
		Args:    []string{"mcp"},
		Env:     map[string]string{"AGENT_MANAGER_SESSION_ID": "${AGENT_MANAGER_SESSION_ID}"},
		Enabled: &enabled,
	}
	if !validHermesMCPEntry(entry, "/opt/bin/agent-manager") {
		t.Fatal("valid Hermes MCP entry was rejected")
	}
	entry.Args = []string{"wrong"}
	if validHermesMCPEntry(entry, "/opt/bin/agent-manager") {
		t.Fatal("Hermes MCP entry with wrong args was accepted")
	}
}

func TestApplyHermesRegistersAndVerifies(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake Hermes executable is a shell script")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
if [ "$1" = "config" ] && [ "$2" = "get" ]; then
  if [ -n "$AGENT_MANAGER_SESSION_ID" ]; then
    touch "$FAKE_HERMES_LEAK"
  fi
  if [ -f "$FAKE_HERMES_STATE" ]; then
    printf '{"command":"%s","args":["mcp"],"env":{"AGENT_MANAGER_SESSION_ID":"${AGENT_MANAGER_SESSION_ID}"},"enabled":true}\n' "$FAKE_HERMES_EXE"
    exit 0
  fi
  exit 1
fi
if [ "$1" = "mcp" ] && [ "$2" = "add" ]; then
  printf '%s\n' "$@" > "$FAKE_HERMES_ARGS"
  sed -n l > "$FAKE_HERMES_STDIN"
  touch "$FAKE_HERMES_STATE"
  exit 0
fi
exit 1
`
	if err := os.WriteFile(filepath.Join(bin, "hermes"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(dir, "registered")
	argsPath := filepath.Join(dir, "args")
	stdinPath := filepath.Join(dir, "stdin")
	leakPath := filepath.Join(dir, "leaked")
	exe := "/opt/bin/agent-manager"
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_HERMES_STATE", state)
	t.Setenv("FAKE_HERMES_ARGS", argsPath)
	t.Setenv("FAKE_HERMES_STDIN", stdinPath)
	t.Setenv("FAKE_HERMES_LEAK", leakPath)
	t.Setenv("FAKE_HERMES_EXE", exe)
	t.Setenv(hooks.EnvSessionID, "parent-session")

	command, err := Apply("hermes", exe, dir, "hermes --cli", map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if command != "hermes --cli" {
		t.Fatalf("command = %q", command)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := "mcp\nadd\nagent-manager\n--command\n/opt/bin/agent-manager\n--env\nAGENT_MANAGER_SESSION_ID=${AGENT_MANAGER_SESSION_ID}\n--args\nmcp\n"
	if string(args) != wantArgs {
		t.Fatalf("hermes args = %q want %q", args, wantArgs)
	}
	stdin, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(stdin) != "$\n" {
		t.Fatalf("hermes stdin = %q want one blank answer", stdin)
	}
	if _, err := os.Stat(leakPath); !os.IsNotExist(err) {
		t.Fatal("config verification expanded the parent manager's session id")
	}
	marker, err := os.ReadFile(filepath.Join(dir, "mcp-hermes-registered"))
	if err != nil || string(marker) != exe {
		t.Fatalf("marker = %q err=%v", marker, err)
	}
}

func TestApplyHermesReportsMissingMCPSupport(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake Hermes executable is a shell script")
	}
	bin := t.TempDir()
	// A Homebrew Hermes without the optional SDK: add refuses to connect,
	// saves nothing, and still exits 0 after its save-anyway prompt.
	script := `#!/bin/sh
if [ "$1" = "config" ]; then
  exit 1
fi
printf "Failed to connect: MCP server 'agent-manager' requires the 'mcp' Python SDK, but it is not installed. Run 'hermes setup' to install MCP support, then retry.\n"
exit 0
`
	if err := os.WriteFile(filepath.Join(bin, "hermes"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	_, err := Apply("hermes", "/opt/bin/agent-manager", t.TempDir(), "hermes --cli", map[string]string{})
	var unavailable HermesMCPUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("err = %v, want HermesMCPUnavailableError", err)
	}
}

func TestPythonFromVersionReadsInstallDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture is a POSIX install layout")
	}
	version := "Hermes Agent v0.20.0 (2026.8.3)\n" +
		"Install directory: /opt/homebrew/Cellar/hermes-agent/2026.8.3_1/libexec/lib/python3.14/site-packages\n" +
		"Python: 3.14.7\n" +
		"OpenAI SDK: 2.24.0\n"
	want := "/opt/homebrew/Cellar/hermes-agent/2026.8.3_1/libexec/bin/python3"
	if got := pythonFromVersion(version); got != want {
		t.Fatalf("pythonFromVersion = %q, want %q", got, want)
	}
	bare := "Hermes Agent v0.20.0 (2026.8.3)\nPython: 3.14.7\n"
	if got := pythonFromVersion(bare); got != "" {
		t.Fatalf("version output naming no install directory = %q, want no interpreter", got)
	}
	elsewhere := "Install directory: /opt/hermes/src\n"
	if got := pythonFromVersion(elsewhere); got != "" {
		t.Fatalf("install directory outside a python prefix = %q, want no interpreter", got)
	}
}

func fakeHermesEnvironment(t *testing.T, pipStatus int) string {
	t.Helper()
	root := t.TempDir()
	sitePackages := filepath.Join(root, "lib", "python3.14", "site-packages")
	if err := os.MkdirAll(sitePackages, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	python := fmt.Sprintf("#!/bin/sh\nexit %d\n", pipStatus)
	if err := os.WriteFile(filepath.Join(root, "bin", "python3"), []byte(python), 0o755); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	hermes := "#!/bin/sh\nprintf 'Install directory: %s\\n' " + tmux.ShellQuote(sitePackages) + "\n"
	if err := os.WriteFile(filepath.Join(bin, "hermes"), []byte(hermes), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return filepath.Join(root, "bin", "python3")
}

func TestHermesPipCommandNamesTheInterpreterThatHasPip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake Hermes executable is a shell script")
	}
	python := fakeHermesEnvironment(t, 0)
	if got, err := hermesPipCommand(context.Background()); err != nil || got != tmux.ShellInvoke(python)+" -m pip install mcp" {
		t.Fatalf("hermesPipCommand = %q, %v, want an install command", got, err)
	}
}

func TestHermesPipCommandIsEmptyWithoutPip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake Hermes executable is a shell script")
	}
	fakeHermesEnvironment(t, 1)
	if got, err := hermesPipCommand(context.Background()); err != nil || got != "" {
		t.Fatalf("hermesPipCommand = %q, %v, want none when the interpreter has no pip", got, err)
	}
}

func TestEnsureHermesRegisteredWrapsExitError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake Hermes executable is a shell script")
	}
	bin := t.TempDir()
	script := `#!/bin/sh
if [ "$1" = "config" ]; then
  exit 1
fi
printf 'registration failed\n' >&2
exit 7
`
	if err := os.WriteFile(filepath.Join(bin, "hermes"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	err := ensureHermesRegistered(context.Background(), "/opt/bin/agent-manager", t.TempDir())
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error should wrap exec.ExitError, got %v", err)
	}
	if exitErr.ExitCode() != 7 {
		t.Fatalf("exit code = %d, want 7", exitErr.ExitCode())
	}
}
