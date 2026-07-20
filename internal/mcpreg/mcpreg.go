// Package mcpreg registers the agent-manager MCP server into the sessions
// the manager spawns, per tool. Each supported tool has a registration
// style: a launch flag, a generated config file, or a one-time global
// registration. Generated artifacts live under the manager's hooks
// directory and reference the running binary, so any install location
// (homebrew, go install) works untouched.
package mcpreg

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

const serverName = "agent-manager"

var knownStyles = map[string]bool{
	"claude":   true,
	"codex":    true,
	"opencode": true,
	"grok":     true,
	"none":     true,
}

// Style resolves a tool's registration style: the explicit `mcp` config
// value wins, otherwise a tool whose config key names a known style uses
// it, and anything else registers nothing.
func Style(toolName, explicit string) string {
	if explicit != "" {
		if knownStyles[explicit] {
			return explicit
		}
		return "none"
	}
	if knownStyles[toolName] {
		return toolName
	}
	return "none"
}

// Apply mutates a session launch so the tool sees the MCP server: it may
// append flags to command, add environment variables, write a config file
// under hooksDir, or run a one-time registration. exe is the agent-manager
// binary the generated configs point at.
func Apply(style, exe, hooksDir, command string, env map[string]string) (string, error) {
	switch style {
	case "claude":
		path, err := writeConfig(hooksDir, "mcp-claude.json", claudeConfig(exe))
		if err != nil {
			return "", err
		}
		return command + " --mcp-config " + tmux.ShellQuote(path), nil
	case "codex":
		overrides := []string{
			fmt.Sprintf(`mcp_servers.%s.command=%q`, serverName, exe),
			fmt.Sprintf(`mcp_servers.%s.args=["mcp"]`, serverName),
			fmt.Sprintf(`mcp_servers.%s.env_vars=[%q]`, serverName, hooks.EnvSessionID),
		}
		for _, override := range overrides {
			command += " -c " + tmux.ShellQuote(override)
		}
		return command, nil
	case "opencode":
		path, err := writeConfig(hooksDir, "mcp-opencode.json", opencodeConfig(exe))
		if err != nil {
			return "", err
		}
		env["OPENCODE_CONFIG"] = path
		return command, nil
	case "grok":
		if err := ensureGrokRegistered(exe, hooksDir); err != nil {
			return "", err
		}
		return command, nil
	default:
		return command, nil
	}
}

func claudeConfig(exe string) []byte {
	config := map[string]any{
		"mcpServers": map[string]any{
			serverName: map[string]any{
				"command": exe,
				"args":    []string{"mcp"},
				"env": map[string]string{
					hooks.EnvSessionID: "${" + hooks.EnvSessionID + "}",
				},
			},
		},
	}
	data, _ := json.MarshalIndent(config, "", "  ")
	return data
}

func opencodeConfig(exe string) []byte {
	config := map[string]any{
		"$schema": "https://opencode.ai/config.json",
		"mcp": map[string]any{
			serverName: map[string]any{
				"type":    "local",
				"command": []string{exe, "mcp"},
				"enabled": true,
				"environment": map[string]string{
					hooks.EnvSessionID: "{env:" + hooks.EnvSessionID + "}",
				},
			},
		},
	}
	data, _ := json.MarshalIndent(config, "", "  ")
	return data
}

// writeConfig writes content only when it changed, so concurrent spawns
// reading the same path never observe a partial rewrite of identical bytes.
func writeConfig(dir, name string, content []byte) (string, error) {
	path := filepath.Join(dir, name)
	if existing, err := os.ReadFile(path); err == nil && string(existing) == string(content) {
		return path, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// ensureGrokRegistered adds the server to grok's user-scope config once
// per binary path, via grok's own config writer. The marker file records
// the registered path so upgrades that move the binary re-register.
func ensureGrokRegistered(exe, hooksDir string) error {
	marker := filepath.Join(hooksDir, "mcp-grok-registered")
	if content, err := os.ReadFile(marker); err == nil && string(content) == exe {
		return nil
	}
	cmd := exec.Command("grok", "mcp", "add",
		"--scope", "user",
		"-e", hooks.EnvSessionID+"=${"+hooks.EnvSessionID+"}",
		serverName, "--", exe, "mcp")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("grok mcp add: %v: %s", err, out)
	}
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(marker, []byte(exe), 0o644)
}
