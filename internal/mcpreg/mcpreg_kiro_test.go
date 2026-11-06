package mcpreg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/hooks"
)

func TestApplyKiroRegistersGloballyWithTheSessionID(t *testing.T) {
	dir := fakeCLI(t, "kiro-cli", `if [ "$1" = "mcp" ] && [ "$2" = "add" ]; then
  printf '%s\n' "$@" > "$FAKE_KIRO_ARGS"
  exit 0
fi
exit 1
`)
	argsPath := filepath.Join(dir, "args")
	t.Setenv("FAKE_KIRO_ARGS", argsPath)
	exe := "/opt/bin/agent-manager"
	env := map[string]string{hooks.EnvSessionID: "sess-abcd"}
	command, err := Apply("kiro", exe, dir, "kiro-cli chat", env)
	if err != nil {
		t.Fatal(err)
	}
	if command != "kiro-cli chat" || env[hooks.EnvSessionID] != "sess-abcd" || len(env) != 1 {
		t.Fatalf("command = %q, env = %v", command, env)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{"mcp", "add", "--scope", "global", "--name", "agent-manager",
		"--command", exe, "--args", "mcp",
		"--env", "AGENT_MANAGER_SESSION_ID=${AGENT_MANAGER_SESSION_ID}", "--force"}, "\n") + "\n"
	if string(args) != want {
		t.Fatalf("kiro-cli args = %q want %q", args, want)
	}
	marker, err := os.ReadFile(filepath.Join(dir, "mcp-kiro-registered"))
	if err != nil || string(marker) != exe {
		t.Fatalf("marker = %q err=%v", marker, err)
	}
}

// An entry already in mcp.json makes a plain add exit 1, which --force avoids.
func TestApplyKiroReplacesAnEntryAnEarlierPathLeft(t *testing.T) {
	dir := fakeCLI(t, "kiro-cli", `for arg in "$@"; do
  [ "$arg" = "--force" ] && exit 0
done
echo "MCP server 'agent-manager' already exists in global config. Use --force to overwrite." >&2
exit 1
`)
	if err := os.WriteFile(filepath.Join(dir, "mcp-kiro-registered"), []byte("/old/agent-manager"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply("kiro", "/opt/bin/agent-manager", dir, "kiro-cli chat", map[string]string{}); err != nil {
		t.Fatal(err)
	}
	marker, err := os.ReadFile(filepath.Join(dir, "mcp-kiro-registered"))
	if err != nil || string(marker) != "/opt/bin/agent-manager" {
		t.Fatalf("marker = %q err=%v", marker, err)
	}
}

func TestApplyKiroSkipsWhenMarkerMatches(t *testing.T) {
	dir := fakeCLI(t, "kiro-cli", "exit 1\n")
	if err := os.WriteFile(filepath.Join(dir, "mcp-kiro-registered"), []byte("/opt/bin/agent-manager"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply("kiro", "/opt/bin/agent-manager", dir, "kiro-cli chat", map[string]string{}); err != nil {
		t.Fatalf("a matching marker still ran kiro-cli: %v", err)
	}
}

func TestApplyKiroReportsAFailedAdd(t *testing.T) {
	dir := fakeCLI(t, "kiro-cli", "echo 'You are not logged in' >&2\nexit 1\n")
	_, err := Apply("kiro", "/opt/bin/agent-manager", dir, "kiro-cli chat", map[string]string{})
	if err == nil || !strings.HasPrefix(err.Error(), "kiro mcp add: ") || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("err = %v", err)
	}
	assertNoMarker(t, dir, "kiro")
}
