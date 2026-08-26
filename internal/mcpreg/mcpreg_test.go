package mcpreg

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/hooks"
)

func TestStyleResolution(t *testing.T) {
	cases := []struct {
		tool, explicit, want string
	}{
		{"claude", "", "claude"},
		{"codex", "", "codex"},
		{"opencode", "", "opencode"},
		{"grok", "", "grok"},
		{"gemini", "", "gemini"},
		{"hermes", "", "hermes"},
		{"command-code", "", "command-code"},
		{"pi", "", "none"},
		{"aider", "", "none"},
		{"command-code", "none", "none"},
		{"my-claude", "claude", "claude"},
		{"claude", "none", "none"},
		{"claude", "bogus", "none"},
	}
	for _, c := range cases {
		if got := Style(c.tool, c.explicit); got != c.want {
			t.Fatalf("Style(%q, %q) = %q, want %q", c.tool, c.explicit, got, c.want)
		}
	}
}

func TestDefaultCommandCodeStyleRegisters(t *testing.T) {
	cfg, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	if got := Style("command-code", cfg.Tools["command-code"].MCP); got != "command-code" {
		t.Fatalf("default command-code style = %q, want command-code", got)
	}
	if got := Style("pi", cfg.Tools["pi"].MCP); got != StyleNone {
		t.Fatalf("default pi style = %q, want %q", got, StyleNone)
	}
}

func TestApplyClaudeWritesConfigAndFlag(t *testing.T) {
	dir := t.TempDir()
	env := map[string]string{}
	command, err := Apply("claude", "/opt/bin/agent-manager", dir, "claude", env)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "mcp-claude.json")
	if !strings.Contains(command, "--mcp-config") || !strings.Contains(command, path) {
		t.Fatalf("command = %q", command)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]map[string]struct {
		Command string            `json:"command"`
		Args    []string          `json:"args"`
		Env     map[string]string `json:"env"`
	}
	if err := json.Unmarshal(content, &parsed); err != nil {
		t.Fatal(err)
	}
	server := parsed["mcpServers"]["agent-manager"]
	if server.Command != "/opt/bin/agent-manager" || len(server.Args) != 1 || server.Args[0] != "mcp" {
		t.Fatalf("server = %+v", server)
	}
	if server.Env["AGENT_MANAGER_SESSION_ID"] != "${AGENT_MANAGER_SESSION_ID}" {
		t.Fatalf("env = %v", server.Env)
	}
	if len(env) != 0 {
		t.Fatalf("claude style should not touch env, got %v", env)
	}
}

func TestApplyCodexAppendsOverrides(t *testing.T) {
	env := map[string]string{}
	command, err := Apply("codex", "/opt/bin/agent-manager", t.TempDir(), "codex", env)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`mcp_servers.agent-manager.command="/opt/bin/agent-manager"`,
		`mcp_servers.agent-manager.args=["mcp"]`,
		`mcp_servers.agent-manager.env_vars=["AGENT_MANAGER_SESSION_ID"]`,
	} {
		if !strings.Contains(command, want) {
			t.Fatalf("command %q missing %q", command, want)
		}
	}
}

func TestApplyOpencodeSetsEnvToConfigFile(t *testing.T) {
	dir := t.TempDir()
	env := map[string]string{}
	command, err := Apply("opencode", "/opt/bin/agent-manager", dir, "opencode", env)
	if err != nil {
		t.Fatal(err)
	}
	if command != "opencode" {
		t.Fatalf("opencode style should not change the command, got %q", command)
	}
	path := env["OPENCODE_CONFIG"]
	if path == "" {
		t.Fatal("OPENCODE_CONFIG not set")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, want := range []string{`"/opt/bin/agent-manager"`, `"mcp"`, "{env:AGENT_MANAGER_SESSION_ID}"} {
		if !strings.Contains(text, want) {
			t.Fatalf("config %q missing %q", text, want)
		}
	}
}

func TestApplyGrokSkipsWhenMarkerMatches(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mcp-grok-registered"), []byte("/opt/bin/agent-manager"), 0o644); err != nil {
		t.Fatal(err)
	}
	command, err := Apply("grok", "/opt/bin/agent-manager", dir, "grok", map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if command != "grok" {
		t.Fatalf("command = %q", command)
	}
}

func TestApplyGeminiSkipsWhenMarkerMatches(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mcp-gemini-registered"), []byte("/opt/bin/agent-manager"), 0o644); err != nil {
		t.Fatal(err)
	}
	command, err := Apply("gemini", "/opt/bin/agent-manager", dir, "gemini", map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if command != "gemini" {
		t.Fatalf("command = %q", command)
	}
}

func TestApplyCommandCodeSkipsWhenMarkerMatches(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mcp-command-code-registered"), []byte("/opt/bin/agent-manager"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{hooks.EnvSessionID: "sess-abcd"}
	command, err := Apply("command-code", "/opt/bin/agent-manager", dir, "cmd", env)
	if err != nil {
		t.Fatal(err)
	}
	if command != "cmd" {
		t.Fatalf("command = %q", command)
	}
	if env[hooks.EnvSessionID] != "sess-abcd" {
		t.Fatalf("env = %v, want the per-session id left on the launch environment", env)
	}
}

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
	if !errors.Is(err, ErrHermesMCPUnavailable) {
		t.Fatalf("err = %v, want ErrHermesMCPUnavailable", err)
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
	err := ensureHermesRegistered("/opt/bin/agent-manager", t.TempDir())
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("error should wrap exec.ExitError, got %v", err)
	}
	if exitErr.ExitCode() != 7 {
		t.Fatalf("exit code = %d, want 7", exitErr.ExitCode())
	}
}

func TestApplyCommandCodeRegistersWithoutEnvOverlay(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake Command Code executable is a shell script")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
if [ "$1" = "mcp" ] && [ "$2" = "add" ]; then
  printf '%s\n' "$@" > "$FAKE_CMD_ARGS"
  exit 0
fi
exit 1
`
	if err := os.WriteFile(filepath.Join(bin, "cmd"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	argsPath := filepath.Join(dir, "args")
	exe := "/opt/bin/agent-manager"
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_CMD_ARGS", argsPath)
	t.Setenv(hooks.EnvSessionID, "parent-session")

	env := map[string]string{hooks.EnvSessionID: "sess-abcd"}
	command, err := Apply("command-code", exe, dir, "cmd", env)
	if err != nil {
		t.Fatal(err)
	}
	if command != "cmd" {
		t.Fatalf("command = %q", command)
	}
	if env[hooks.EnvSessionID] != "sess-abcd" || len(env) != 1 {
		t.Fatalf("env = %v, want the per-session id left on the launch environment", env)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := "mcp\nadd\n--scope\nuser\nagent-manager\n--\n/opt/bin/agent-manager\nmcp\n"
	if string(args) != wantArgs {
		t.Fatalf("cmd args = %q want %q", args, wantArgs)
	}
	if strings.Contains(string(args), hooks.EnvSessionID) || strings.Contains(string(args), "parent-session") {
		t.Fatalf("cmd mcp add overlaid an env placeholder: %q", args)
	}
	marker, err := os.ReadFile(filepath.Join(dir, "mcp-command-code-registered"))
	if err != nil || string(marker) != exe {
		t.Fatalf("marker = %q err=%v", marker, err)
	}
}

func TestEnsureRegisteredOnceNamesFailedCommand(t *testing.T) {
	err := ensureRegisteredOnce("gemini", "/opt/bin/agent-manager", t.TempDir(),
		exec.Command("agent-manager-no-such-binary"))
	if err == nil {
		t.Fatal("expected an error when the add command cannot run")
	}
	if !strings.HasPrefix(err.Error(), "gemini mcp add: ") {
		t.Fatalf("error = %q, want it to name the tool's add command", err)
	}
}

func TestApplyNoneLeavesEverything(t *testing.T) {
	env := map[string]string{}
	command, err := Apply("none", "/opt/bin/agent-manager", t.TempDir(), "aider", env)
	if err != nil || command != "aider" || len(env) != 0 {
		t.Fatalf("command = %q, env = %v, err = %v", command, env, err)
	}
}
