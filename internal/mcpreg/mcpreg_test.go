package mcpreg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStyleResolution(t *testing.T) {
	cases := []struct {
		tool, explicit, want string
	}{
		{"claude", "", "claude"},
		{"codex", "", "codex"},
		{"opencode", "", "opencode"},
		{"grok", "", "grok"},
		{"aider", "", "none"},
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

func TestApplyNoneLeavesEverything(t *testing.T) {
	env := map[string]string{}
	command, err := Apply("none", "/opt/bin/agent-manager", t.TempDir(), "aider", env)
	if err != nil || command != "aider" || len(env) != 0 {
		t.Fatalf("command = %q, env = %v, err = %v", command, env, err)
	}
}
