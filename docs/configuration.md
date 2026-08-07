# Configuration

Config lives in your OS user config dir (`~/Library/Application Support/agent-manager/config.toml` on macOS, `~/.config/agent-manager/config.toml` on Linux, with `XDG_CONFIG_HOME` honored when set) and is created on first run with defaults for Claude Code, OpenCode, Codex, Grok Build, Gemini CLI, and Pi.

The Pi defaults require Pi 0.76.0 or later because they use `--session-id`.

Top-level: `poll_interval` (default `"2s"`) sets how often panes are polled for status, preview, and stats.

The generated file also carries a `[tools.terminal]` block. That one is the shell `T` opens, not an agent CLI: an empty `command` leaves the pane on `$SHELL`, and setting one opens a different shell. `shell = true` is what marks it — never the name — so the tool pickers skip it and the keys that write into a pane refuse it (see [Terminal tabs](usage.md#terminal-tabs)). Any block can carry the flag, and a `[tools.terminal]` block already in your own config keeps whatever it already means.

Add any CLI tool as a `[tools.<name>]` block:

```toml
[tools.mytool]
command = "mytool"
default_status = "idle"
rules = [
  { state = "working", pattern = "esc to interrupt" },
  { state = "errored", pattern = "(?im)^\\s*error:" },
]
```

Rules match top-down against the visible pane text; first match wins, and `default_status` applies when nothing matches.

**Status detection.** Optional per-tool fields refine it: `activity_cutoff` (regex locating the tool's input box, everything above it is turn content), `turn_end` (a turn-summary line marking the turn as over), `busy_line` (work that outlives its turn, such as background agents), `chrome_line`, `blocked_line`, and `trailing_note`. `status_source = "claude-hooks"` switches status to Claude Code hook events (see [Status](usage.md#status)). The generated config's `claude` and `opencode` blocks show all of them in use.

**Revive.** `resume_by_id_command` resumes one exact conversation, with `{id}` replaced by the session's captured agent id. That id comes either from launching under an id the manager mints (`session_id_flag`, e.g. `--session-id`) or from reading back an id the tool minted itself (`session_store = "codex" | "opencode"`). `revive_command` is what `v` falls back to when no id is available, e.g. `claude --continue`.

**Forks.** `fork_command` creates a conversation from an existing session. Agent Manager replaces and shell-quotes these placeholders:

- `{id}`: The source conversation ID. This placeholder is required.
- `{new_id}`: A new UUID that Agent Manager records for exact revival.
- `{name}`: The new Agent Manager session name.

Claude Code and Codex include default fork commands. A custom tool can omit `{new_id}` when its `session_store` captures the generated ID.

**Prompts.** `prompt_flag` controls how the new-session form's optional prompt is embedded into the launch command. Tools that take the prompt as a positional argument (Claude Code: `claude 'the prompt'`) leave it empty; tools whose positional argument means something else declare the flag (OpenCode: `prompt_flag = "--prompt"`, since its positional argument is the project path). The prompt only shapes the launch command; revive (`v`) uses the revive commands untouched.

**MCP.** `mcp = "claude" | "codex" | "opencode" | "grok" | "gemini" | "none"` picks how the agent-manager MCP server is registered into the tool's sessions (see [MCP](usage.md#mcp-how-agents-discover-these-commands)). An empty value uses the tool's config key when it names a known style.

State is stored next to the config in `state.db` (SQLite).

