// Package mcpreg registers the agent-manager MCP server into the sessions
// the manager spawns, per tool. Each supported tool has a registration
// style: a launch flag, a generated config file, or a one-time global
// registration. Generated artifacts live under the manager's hooks
// directory and reference the running binary, so any install location
// (homebrew, go install) works untouched.
package mcpreg

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

const serverName = "agent-manager"

// ErrHermesMCPUnavailable reports a Hermes whose optional MCP SDK is not
// installed, so no registration can succeed until `hermes setup` adds it.
var ErrHermesMCPUnavailable = errors.New("hermes is missing MCP support: run hermes setup, then spawn again")

var knownStyles = map[string]bool{
	"claude":   true,
	"codex":    true,
	"opencode": true,
	"grok":     true,
	"gemini":   true,
	"hermes":   true,
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
	case "gemini":
		if err := ensureGeminiRegistered(exe, hooksDir); err != nil {
			return "", err
		}
		return command, nil
	case "hermes":
		if err := ensureHermesRegistered(exe, hooksDir); err != nil {
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
// per binary path, via grok's own config writer.
func ensureGrokRegistered(exe, hooksDir string) error {
	cmd := exec.Command("grok", "mcp", "add",
		"--scope", "user",
		"-e", hooks.EnvSessionID+"=${"+hooks.EnvSessionID+"}",
		serverName, "--", exe, "mcp")
	return ensureRegisteredOnce("grok", exe, hooksDir, cmd)
}

// ensureGeminiRegistered adds the server to gemini's user-scope
// settings.json once per binary path, via gemini's own config writer.
// Gemini expands ${VAR} in a server's env block at launch, so the entry
// forwards the per-session id env var.
func ensureGeminiRegistered(exe, hooksDir string) error {
	cmd := exec.Command("gemini", "mcp", "add",
		"--scope", "user",
		"-e", hooks.EnvSessionID+"=${"+hooks.EnvSessionID+"}",
		serverName, exe, "mcp")
	return ensureRegisteredOnce("gemini", exe, hooksDir, cmd)
}

type hermesMCPEntry struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
	Enabled *bool             `json:"enabled"`
}

func readHermesMCPEntry() (hermesMCPEntry, bool) {
	cmd := exec.Command("hermes", "config", "get", "mcp_servers."+serverName, "--json")
	for _, item := range os.Environ() {
		if !strings.HasPrefix(item, hooks.EnvSessionID+"=") {
			cmd.Env = append(cmd.Env, item)
		}
	}
	out, err := cmd.Output()
	if err != nil {
		return hermesMCPEntry{}, false
	}
	var entry hermesMCPEntry
	if json.Unmarshal(out, &entry) != nil {
		return hermesMCPEntry{}, false
	}
	return entry, true
}

func validHermesMCPEntry(entry hermesMCPEntry, exe string) bool {
	return entry.Command == exe && len(entry.Args) == 1 && entry.Args[0] == "mcp" &&
		entry.Env[hooks.EnvSessionID] == "${"+hooks.EnvSessionID+"}" &&
		(entry.Enabled == nil || *entry.Enabled)
}

// ensureHermesRegistered accepts the mcp-add defaults after Hermes verifies
// the server. A stale entry adds an overwrite confirmation first.
func ensureHermesRegistered(exe, hooksDir string) error {
	marker := filepath.Join(hooksDir, "mcp-hermes-registered")
	if content, err := os.ReadFile(marker); err == nil && string(content) == exe {
		return nil
	}
	_, existed := readHermesMCPEntry()
	cmd := exec.Command("hermes", "mcp", "add", serverName,
		"--command", exe,
		"--env", hooks.EnvSessionID+"=${"+hooks.EnvSessionID+"}",
		"--args", "mcp")
	if existed {
		cmd.Stdin = strings.NewReader("y\n\n")
	} else {
		cmd.Stdin = strings.NewReader("\n")
	}
	out, err := cmd.CombinedOutput()
	// Hermes without its optional SDK refuses to connect but still exits 0
	// after the save-anyway prompt, so the message is the only signal.
	if strings.Contains(string(out), "requires the 'mcp' Python SDK") {
		return ErrHermesMCPUnavailable
	}
	if err != nil {
		return fmt.Errorf("hermes mcp add: %w: %s", err, out)
	}
	entry, ok := readHermesMCPEntry()
	if !ok || !validHermesMCPEntry(entry, exe) {
		return fmt.Errorf("hermes mcp add did not save an enabled agent-manager server: %s", out)
	}
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(marker, []byte(exe), 0o644)
}

// ensureRegisteredOnce runs a tool's own mcp-add command once per binary
// path. The marker file records the registered path so upgrades that move
// the binary re-register. The check-then-add window is benign: the add
// commands update in place, so two racing managers just write the same
// entry twice.
func ensureRegisteredOnce(tool, exe, hooksDir string, cmd *exec.Cmd) error {
	marker := filepath.Join(hooksDir, "mcp-"+tool+"-registered")
	if content, err := os.ReadFile(marker); err == nil && string(content) == exe {
		return nil
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s mcp add: %v: %s", tool, err, out)
	}
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(marker, []byte(exe), 0o644)
}
