// Package mcpreg registers the agent-manager MCP server into the sessions
// the manager spawns, per tool. Each supported tool has a registration
// style: a launch flag, a generated config file, or a one-time global
// registration. Generated artifacts live under the manager's hooks
// directory and reference the running binary, so any install location
// (homebrew, go install) works untouched.
package mcpreg

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/atomicfile"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

const serverName = "agent-manager"

const StyleNone = "none"

// RegistrationTimeout bounds one whole registration: every command Apply
// runs for a tool shares it, so a tool that registers with several commands
// (Hermes runs up to four) cannot add their delays up. Registration runs on
// the spawn path, so a CLI that stops answering (a network or auth wait)
// would otherwise hold later spawns, kills and other manager actions queued
// behind it for as long as it hangs. It is a variable so tests can shorten it.
var RegistrationTimeout = 2 * time.Minute

// registrationWaitDelay is how long a command's output pipes may stay open
// after it exits: a CLI that leaves a background child holding stdout has
// finished, and waiting for the pipe to close would freeze the manager on
// the child instead.
const registrationWaitDelay = time.Second

// TimeoutError reports a registration that used up RegistrationTimeout. The
// tool is fine and no marker was written, so the next spawn tries again; the
// session can start now without the agent-manager tools.
type TimeoutError struct {
	// Tool is the tool whose registration timed out.
	Tool  string
	Limit time.Duration
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("%s MCP registration did not finish within %s", e.Tool, formatLimit(e.Limit))
}

func formatLimit(d time.Duration) string {
	switch {
	case d >= time.Minute && d%time.Minute == 0:
		return fmt.Sprintf("%dm", d/time.Minute)
	case d >= time.Second && d%time.Second == 0:
		return fmt.Sprintf("%ds", d/time.Second)
	default:
		return d.String()
	}
}

// HermesMCPUnavailableError reports a Hermes whose optional MCP SDK is not
// installed, so no registration can succeed until the package is added to
// the environment Hermes runs in. PipCommand installs it there, and is
// empty when that interpreter cannot be resolved.
type HermesMCPUnavailableError struct {
	PipCommand string
}

func (e HermesMCPUnavailableError) Error() string {
	if e.PipCommand == "" {
		return "hermes is missing MCP support: install the mcp package into its Python, then spawn again"
	}
	return "hermes is missing MCP support: run " + e.PipCommand + ", then spawn again"
}

var knownStyles = map[string]bool{
	"claude":       true,
	"codex":        true,
	"opencode":     true,
	"grok":         true,
	"gemini":       true,
	"hermes":       true,
	"command-code": true,
	"muse":         true,
	"antigravity":  true,
	StyleNone:      true,
}

// Style resolves a tool's registration style: the explicit `mcp` config
// value wins, otherwise a tool whose config key names a known style uses
// it, and anything else registers nothing.
func Style(toolName, explicit string) string {
	if explicit != "" {
		if knownStyles[explicit] {
			return explicit
		}
		return StyleNone
	}
	if knownStyles[toolName] {
		return toolName
	}
	return StyleNone
}

// Apply mutates a session launch so the tool sees the MCP server: it may
// append flags to command, add environment variables, write a config file
// under hooksDir, or run a one-time registration. exe is the agent-manager
// binary the generated configs point at.
func Apply(style, exe, hooksDir, command string, env map[string]string) (string, error) {
	// Every command a registration runs shares this one deadline.
	ctx, cancel := context.WithTimeout(context.Background(), RegistrationTimeout)
	defer cancel()
	switch style {
	case "claude":
		path := filepath.Join(hooksDir, "mcp-claude.json")
		if err := atomicfile.WriteIfChanged(path, claudeConfig(exe), 0o644); err != nil {
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
		path := filepath.Join(hooksDir, "mcp-opencode.json")
		if err := atomicfile.WriteIfChanged(path, opencodeConfig(exe), 0o644); err != nil {
			return "", err
		}
		env["OPENCODE_CONFIG"] = path
		return command, nil
	case "grok":
		if err := ensureGrokRegistered(ctx, exe, hooksDir); err != nil {
			return "", err
		}
		return command, nil
	case "gemini":
		if err := ensureGeminiRegistered(ctx, exe, hooksDir); err != nil {
			return "", err
		}
		return command, nil
	case "hermes":
		if err := ensureHermesRegistered(ctx, exe, hooksDir); err != nil {
			return "", err
		}
		return command, nil
	case "command-code":
		if err := ensureCommandCodeRegistered(ctx, exe, hooksDir); err != nil {
			return "", err
		}
		return command, nil
	case "muse":
		if err := ensureMuseRegistered(exe); err != nil {
			return "", err
		}
		return command, nil
	case "antigravity":
		if err := ensureAntigravityRegistered(ctx, exe, hooksDir); err != nil {
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

type museServer struct {
	Type    string   `json:"type"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
	// optional keeps a server that fails to start from blocking Muse's startup.
	Mode string `json:"mode"`
}

func museSettingsPath() (string, error) {
	config := os.Getenv("XDG_CONFIG_HOME")
	if config == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		config = filepath.Join(home, ".config")
	}
	return filepath.Join(config, "muse", "settings.json"), nil
}

// Muse has no command that adds a server, so the entry goes into its settings
// file. It carries no session id: Muse passes a server none of the session's
// environment, so the server finds its session from its process ancestry.
func ensureMuseRegistered(exe string) error {
	path, err := museSettingsPath()
	if err != nil {
		return err
	}
	// The atomic rename would replace a symlinked file (a dotfiles repo's)
	// rather than write through it.
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	settings := map[string]json.RawMessage{}
	perm := os.FileMode(0o600)
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		settings["schema_version"] = json.RawMessage("1")
	case err != nil:
		return err
	default:
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("muse settings %s: %w", path, err)
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		perm = info.Mode().Perm()
	}
	// Muse ignores every server when both spellings are present, so the entry
	// joins whichever one the file already uses.
	key := "mcpServers"
	if _, current := settings[key]; !current {
		if _, legacy := settings["mcp_servers"]; legacy {
			key = "mcp_servers"
		}
	}
	servers := map[string]json.RawMessage{}
	if raw, ok := settings[key]; ok {
		if err := json.Unmarshal(raw, &servers); err != nil {
			return fmt.Errorf("muse settings %s: %s: %w", path, key, err)
		}
	}
	entry, err := json.Marshal(museServer{Type: "stdio", Command: exe, Args: []string{"mcp"}, Mode: "optional"})
	if err != nil {
		return err
	}
	var existing bytes.Buffer
	if json.Compact(&existing, servers[serverName]) == nil && bytes.Equal(existing.Bytes(), entry) {
		return nil
	}
	servers[serverName] = entry
	if settings[key], err = json.Marshal(servers); err != nil {
		return err
	}
	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.WriteFile(path, append(out, '\n'), perm)
}

// Command Code overlays stored env onto process.env without expanding ${VAR}.
func ensureCommandCodeRegistered(ctx context.Context, exe, hooksDir string) error {
	add := command{name: "cmd", args: []string{"mcp", "add",
		"--scope", "user",
		serverName, "--", exe, "mcp"}}
	return ensureRegisteredOnce(ctx, "command-code", exe, hooksDir, add)
}

// ensureGrokRegistered adds the server to grok's user-scope config once
// per binary path, via grok's own config writer.
func ensureGrokRegistered(ctx context.Context, exe, hooksDir string) error {
	add := command{name: "grok", args: []string{"mcp", "add",
		"--scope", "user",
		"-e", hooks.EnvSessionID + "=${" + hooks.EnvSessionID + "}",
		serverName, "--", exe, "mcp"}}
	return ensureRegisteredOnce(ctx, "grok", exe, hooksDir, add)
}

// ensureGeminiRegistered adds the server to gemini's user-scope
// settings.json once per binary path, via gemini's own config writer.
// Gemini expands ${VAR} in a server's env block at launch, so the entry
// forwards the per-session id env var.
func ensureGeminiRegistered(ctx context.Context, exe, hooksDir string) error {
	add := command{name: "gemini", args: []string{"mcp", "add",
		"--scope", "user",
		"-e", hooks.EnvSessionID + "=${" + hooks.EnvSessionID + "}",
		serverName, exe, "mcp"}}
	return ensureRegisteredOnce(ctx, "gemini", exe, hooksDir, add)
}

// Antigravity passes a server its own environment and leaves ${VAR} in an
// env entry unexpanded, so the session id reaches the server by inheritance.
func ensureAntigravityRegistered(ctx context.Context, exe, hooksDir string) error {
	add := command{name: "agy", args: []string{"mcp", "add", serverName, "--", exe, "mcp"}}
	return ensureRegisteredOnce(ctx, "antigravity", exe, hooksDir, add)
}

type hermesMCPEntry struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
	Enabled *bool             `json:"enabled"`
}

func readHermesMCPEntry(ctx context.Context) (hermesMCPEntry, bool) {
	get := command{name: "hermes", args: []string{"config", "get", "mcp_servers." + serverName, "--json"}}
	for _, item := range os.Environ() {
		if !strings.HasPrefix(item, hooks.EnvSessionID+"=") {
			get.env = append(get.env, item)
		}
	}
	out, err := get.output(ctx)
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
func ensureHermesRegistered(ctx context.Context, exe, hooksDir string) error {
	marker := filepath.Join(hooksDir, "mcp-hermes-registered")
	if content, err := os.ReadFile(marker); err == nil && string(content) == exe {
		return nil
	}
	_, existed := readHermesMCPEntry(ctx)
	add := command{name: "hermes", args: []string{"mcp", "add", serverName,
		"--command", exe,
		"--env", hooks.EnvSessionID + "=${" + hooks.EnvSessionID + "}",
		"--args", "mcp"}}
	if existed {
		add.stdin = "y\n\n"
	} else {
		add.stdin = "\n"
	}
	out, err := add.combinedOutput(ctx)
	if timedOut(err) {
		return withTool(err, "hermes")
	}
	// Hermes without its optional SDK refuses to connect but still exits 0
	// after the save-anyway prompt, so the message is the only signal.
	if strings.Contains(string(out), "requires the 'mcp' Python SDK") {
		return HermesMCPUnavailableError{PipCommand: hermesPipCommand(ctx)}
	}
	if err != nil {
		return fmt.Errorf("hermes mcp add: %w%s", err, outputSuffix(out))
	}
	entry, ok := readHermesMCPEntry(ctx)
	if !ok || !validHermesMCPEntry(entry, exe) {
		if ctx.Err() != nil {
			return &TimeoutError{Tool: "hermes", Limit: RegistrationTimeout}
		}
		return fmt.Errorf("hermes mcp add did not save an enabled agent-manager server%s", outputSuffix(out))
	}
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(marker, []byte(exe), 0o644)
}

// pipx and uv build their environments without a pip module, so the line
// is only offered where the interpreter has one.
func hermesPipCommand(ctx context.Context) string {
	out, err := command{name: "hermes", args: []string{"--version"}}.output(ctx)
	if err != nil {
		return ""
	}
	python := pythonFromVersion(string(out))
	if python == "" {
		return ""
	}
	if _, err := (command{name: python, args: []string{"-m", "pip", "--version"}}).output(ctx); err != nil {
		return ""
	}
	return tmux.ShellInvoke(python) + " -m pip install mcp"
}

// ensureRegisteredOnce runs a tool's own mcp-add command once per binary
// path. The marker file records the registered path so upgrades that move
// the binary re-register. The check-then-add window is benign: the add
// commands update in place, so two racing managers just write the same
// entry twice.
func ensureRegisteredOnce(ctx context.Context, tool, exe, hooksDir string, add command) error {
	marker := filepath.Join(hooksDir, "mcp-"+tool+"-registered")
	if content, err := os.ReadFile(marker); err == nil && string(content) == exe {
		return nil
	}
	if out, err := add.combinedOutput(ctx); err != nil {
		if timedOut(err) {
			return withTool(err, tool)
		}
		return fmt.Errorf("%s mcp add: %v%s", tool, err, outputSuffix(out))
	}
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(marker, []byte(exe), 0o644)
}

// outputSuffix appends a command's output to an error message, leaving a
// command that printed nothing without a dangling colon.
func outputSuffix(out []byte) string {
	if text := strings.TrimSpace(string(out)); text != "" {
		return ": " + text
	}
	return ""
}

func timedOut(err error) bool {
	var timeout *TimeoutError
	return errors.As(err, &timeout)
}

// withTool names the tool on a timeout error from a command that does not
// know which tool it registers.
func withTool(err error, tool string) error {
	var timeout *TimeoutError
	if errors.As(err, &timeout) {
		timeout.Tool = tool
	}
	return err
}

// command is one external command a registration runs. It is bounded by the
// registration's shared context and, once it exits, by registrationWaitDelay
// for its output pipes.
type command struct {
	name  string
	args  []string
	stdin string
	// env replaces the inherited environment when set.
	env []string
}

func (c command) output(ctx context.Context) ([]byte, error) {
	return c.run(ctx, (*exec.Cmd).Output)
}

func (c command) combinedOutput(ctx context.Context) ([]byte, error) {
	return c.run(ctx, (*exec.Cmd).CombinedOutput)
}

func (c command) run(ctx context.Context, collect func(*exec.Cmd) ([]byte, error)) ([]byte, error) {
	cmd := exec.CommandContext(ctx, c.name, c.args...)
	cmd.Env = c.env
	if c.stdin != "" {
		cmd.Stdin = strings.NewReader(c.stdin)
	}
	cmd.WaitDelay = registrationWaitDelay
	out, err := collect(cmd)
	switch {
	case errors.Is(err, exec.ErrWaitDelay):
		// It exited 0; only a child it left behind still holds the pipe.
		err = nil
	case err != nil && ctx.Err() != nil:
		err = &TimeoutError{Tool: c.name, Limit: RegistrationTimeout}
	}
	return out, err
}
