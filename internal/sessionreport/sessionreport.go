// Package sessionreport wires each launch so the running agent reports every
// conversation it switches to, and a revive resumes the conversation the
// agent was on rather than the one it launched with. A CLI is wired through
// a launch flag or environment variable naming a file in the manager's own
// hooks directory, never through a file the user or the CLI owns. The style
// name is also the tool name a report carries, which the store matches
// against the session's row. Muse, Grok, Antigravity and Hermes have no such
// flag; the conversation package reads what they keep instead.
package sessionreport

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/YoanWai/agent-manager/internal/atomicfile"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

const (
	StyleClaude = "claude"
	StyleNone   = "none"
)

// Styles lists every value session_report accepts.
var Styles = []string{StyleClaude, "pi", "omp", "opencode", "command-code", "codex", "gemini", "muse", "grok", "antigravity", "hermes", StyleNone}

// HookCommand is the command a hook-config CLI runs: it reads the hook's JSON
// input on stdin and reports the string field key. exec makes the CLI that
// fired the hook the report's parent, which is how the report proves it
// comes from the session's own agent.
func HookCommand(exe, tool, key string) string {
	return "exec " + tmux.ShellQuote(exe) + " track-conversation --tool " + tool + " --key " + key
}

// spawnedFlag lets a report come from below the launched process, for the npm
// CLIs a launcher such as a Volta shim spawns rather than execs.
const spawnedFlag = "--spawned"

// Target is the launch a reporter is wired into.
type Target struct {
	// Exe is the agent-manager binary a report runs.
	Exe string
	// HooksDir is the manager's own directory for generated files.
	HooksDir string
	// TelemetryFile is where gemini logs this session's telemetry.
	TelemetryFile string
	// Cwd is the directory the agent starts in.
	Cwd string
}

// Apply wires a launch so the agent reports each conversation it switches
// to: it may append flags to command, set environment variables, or write a
// file under the hooks directory. Claude's wiring rides the settings file
// the hooks package writes.
func Apply(style string, target Target, command string, env map[string]string) (string, error) {
	switch style {
	case "pi":
		return withExtension(target.HooksDir, "track-pi.ts", extensionSource(target.Exe, []string{"--tool", style, spawnedFlag}, "session_start"), command)
	case "omp":
		return withExtension(target.HooksDir, "track-omp.ts", extensionSource(target.Exe, []string{"--tool", style}, "session_start", "session_switch", "session_branch"), command)
	case "opencode":
		return command, wireOpencode(target.Exe, target.HooksDir, env)
	case "command-code":
		return withMod(target.HooksDir, fmt.Appendf(nil, commandCodeModTemplate, jsString(target.Exe), jsString(spawnedFlag)), command)
	case "codex":
		return command + codexHooks(target.Exe), nil
	case "gemini":
		return wireGemini(target, command, env)
	default:
		return command, nil
	}
}

// warnUnwired lets a launch go ahead when the tool cannot be wired: the
// agent runs as it did before this wiring existed, and the pane says so
// before the agent starts.
func warnUnwired(tool, command, reason string) string {
	notice := fmt.Sprintf("agent-manager: %s will not report conversation switches, so revive reopens the conversation it started with: %s", tool, reason)
	return "printf '%s\\n' " + tmux.ShellQuote(notice) + " >&2; " + command
}

// jsString quotes a value for the generated JavaScript and TypeScript
// sources, which read a JSON string literal as a string.
func jsString(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}

const extensionTemplate = `import { execFileSync } from "node:child_process";

export default function (pi: any) {
  for (const event of %s) {
    pi.on(event, (_event: any, ctx: any) => {
      if (!ctx.hasUI) return;
      execFileSync(%s, ["track-conversation", ...%s, "--id", ctx.sessionManager.getSessionId()], { stdio: ["ignore", "ignore", "pipe"] });
    });
  }
}
`

// The report runs synchronously, so two switches in quick succession land in
// the order the agent made them. A task subagent loads the agent's
// extensions in the agent's own process, with no UI, so only a session that
// has one is the conversation the pane shows.
func extensionSource(exe string, reportFlags []string, events ...string) []byte {
	return fmt.Appendf(nil, extensionTemplate, jsString(events), jsString(exe), jsString(reportFlags))
}

// withExtension loads the extension next to whatever extensions the user
// already has: pi and omp read -e anywhere on the line, any number of times.
func withExtension(hooksDir, name string, source []byte, command string) (string, error) {
	path := filepath.Join(hooksDir, name)
	if err := atomicfile.WriteIfChanged(path, source, 0o644); err != nil {
		return "", err
	}
	return command + " -e " + tmux.ShellQuote(path), nil
}

const commandCodeModTemplate = `import { execFileSync } from "node:child_process";

export default function (cmd: any) {
  cmd.hooks({
    onSessionStart: (info: any) => {
      execFileSync(%s, ["track-conversation", "--tool", "command-code", %s, "--id", info.sessionId], { stdio: ["ignore", "ignore", "pipe"] });
    },
  });
}
`

// withMod loads a mod for this session alongside the user's own: Command
// Code's onSessionStart fires on startup and on /clear, /clone, /fork and
// /resume, with the conversation now current. A mod runs in the CLI's own
// process, which /reload respawns as a child in the same process group.
func withMod(hooksDir string, source []byte, command string) (string, error) {
	path := filepath.Join(hooksDir, "track-cmd.ts")
	if err := atomicfile.WriteIfChanged(path, source, 0o644); err != nil {
		return "", err
	}
	return command + " --mod " + tmux.ShellQuote(path), nil
}

// codexHooks adds session-scoped hooks for this launch only. Codex asks the
// user to review a hook it has not seen before and keeps their answer by the
// hook's content, so the handler is the same on every launch. SessionStart
// fires once a conversation is started or switched to, at its first prompt.
// Codex installed from npm runs a node launcher under the launched pid,
// which spawns the native binary that fires the hooks as its child in the
// same process group; the commands Codex runs get a group of their own.
func codexHooks(exe string) string {
	handler := fmt.Sprintf(`[{hooks=[{type="command",command=%q}]}]`, HookCommand(exe, "codex", "session_id")+" "+spawnedFlag)
	flags := ""
	for _, event := range []string{"SessionStart", "UserPromptSubmit"} {
		flags += " -c " + tmux.ShellQuote("hooks."+event+"="+handler)
	}
	return flags
}

const opencodePluginTemplate = `import { execFileSync } from "node:child_process";

const tui = async (api: any) => {
  let last = "";
  const check = () => {
    const route = api.route.current;
    const sessionID = route.name === "session" ? route.params?.sessionID : undefined;
    if (!sessionID || sessionID === last) return;
    const info = api.state.session.get(sessionID);
    if (!info || info.parentID) return;
    last = sessionID;
    execFileSync(%s, ["track-conversation", "--tool", "opencode", "--id", sessionID], { stdio: ["ignore", "ignore", "pipe"] });
  };
  const solid = await import("solid-js");
  solid.createRoot(() => solid.createEffect(check));
  const timer = setInterval(check, 250);
  api.lifecycle.onDispose(() => clearInterval(timer));
};

export default { id: "agent-manager.track-conversation", tui };
`

// wireOpencode loads a TUI plugin that watches the route the TUI shows. A
// subagent's session carries a parent and is not the conversation the user
// is on. OPENCODE_TUI_CONFIG layers over the user's own tui.json, but the
// variable holds one path, so a user who already sets it keeps theirs.
func wireOpencode(exe, hooksDir string, env map[string]string) error {
	if os.Getenv("OPENCODE_TUI_CONFIG") != "" {
		return nil
	}
	plugin := filepath.Join(hooksDir, "track-opencode.ts")
	if err := atomicfile.WriteIfChanged(plugin, fmt.Appendf(nil, opencodePluginTemplate, jsString(exe)), 0o644); err != nil {
		return err
	}
	config, err := json.MarshalIndent(map[string]any{"plugin": []string{plugin}}, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(hooksDir, "track-opencode-tui.json")
	if err := atomicfile.WriteIfChanged(path, config, 0o644); err != nil {
		return err
	}
	env["OPENCODE_TUI_CONFIG"] = path
	return nil
}

// wireGemini has gemini log its telemetry to a file of the manager's own,
// where every log record names the conversation it belongs to, and nothing
// leaves the machine. The environment overrides the user's telemetry
// settings, so a user who turned telemetry on keeps theirs and the session
// reports nothing. Each launch starts the file afresh, so an agent from an
// earlier launch still running writes nowhere the poller reads. gemini
// writes metrics to it every few seconds, so a follower started beside the
// agent keeps it emptied while no manager runs.
func wireGemini(target Target, command string, env map[string]string) (string, error) {
	inUse, err := geminiTelemetryInUse(target.Cwd)
	if err != nil {
		return warnUnwired("gemini", command, err.Error()), nil
	}
	if inUse {
		return warnUnwired("gemini", command, "its own telemetry settings are in use"), nil
	}
	if err := os.Remove(target.TelemetryFile); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	env["GEMINI_TELEMETRY_ENABLED"] = "true"
	env["GEMINI_TELEMETRY_TARGET"] = "local"
	env["GEMINI_TELEMETRY_OUTFILE"] = target.TelemetryFile
	env["GEMINI_TELEMETRY_LOG_PROMPTS"] = "false"
	env["GEMINI_TELEMETRY_TRACES_ENABLED"] = "false"
	follower := tmux.ShellQuote(target.Exe) + " follow-telemetry --file " + tmux.ShellQuote(target.TelemetryFile)
	return follower + " </dev/null >/dev/null 2>&1 & " + command, nil
}
