# Agent Manager

![agent-manager demo](docs/demo.gif)

A terminal UI to manage AI coding-agent sessions on your machine. Create and enter sessions, organize them in a nested group tree with manual ordering, archive finished ones, and watch live status, a live pane preview, the combined footprint of your agents, and machine resource gauges.

## Supported tools

Status detection currently supports **Claude Code** and **OpenCode** out of the box. Any other CLI tool can run as a session; add a `[tools.<name>]` block with status rules to get live status for it (see [Configuration](#configuration)).

## Install

### Homebrew (macOS / Linux)

```bash
brew install yoanwai/tap/agent-manager
```

Installs tmux with it if missing.

### Go

```bash
go install github.com/YoanWai/agent-manager@latest
```

Requires Go 1.24+ and tmux; installs to `$(go env GOPATH)/bin`.

### Prebuilt binaries

Download from [Releases](https://github.com/YoanWai/agent-manager/releases) (macOS and Linux, amd64/arm64).

### Windows

Run inside [WSL2](https://learn.microsoft.com/windows/wsl/install): agent-manager lives on tmux, which is a Linux/macOS tool. In a WSL shell, install with Homebrew or grab the Linux binary from Releases.

## Usage

```bash
agent-manager
```

Sessions run inside tmux (`am_*` namespace), so they survive the manager quitting. Inside a session, **Ctrl+Q** detaches back to the manager.

### Keys

| Key | Action |
|-----|--------|
| `n` | New session (name, tool, directory, optional starting prompt, group picker) |
| `g` | New group (name, parent, default path) |
| `enter` | Attach session / fold group |
| `ctrl+q` | Inside a session: back to the manager |
| `shift+↑` / `shift+↓` | Reorder session or group among its siblings |
| `m` | Move session to another group |
| `r` | Rename session / edit group (name and default path) |
| `v` | Revive a dead session (`revive_command`, e.g. `claude --continue`, resumes the conversation) |
| `a` / `u` | Archive / restore |
| `d` | Delete session, or a group + its entire subtree |
| `space` | Quick prompt: answer the selected session, or spawn an agent in the selected group |
| `f` | Fold / unfold group |
| `s` | Settings (default tool for quick spawn) |
| `t` | Toggle archived view |
| `/` | Search |
| `ctrl+r` | Force refresh |
| `?` | Help |
| `q` | Quit (sessions keep running) |

### Quick prompt

Press `space` to dock a prompt bar at the bottom of the sidebar. The target follows the cursor while the bar is open (`↑↓` still navigate):

- On a **session** row, `enter` sends the typed text straight into the session's pane, so the agent gets it as a user message without you attaching. The bar stays open and clears, ready for the next answer.
- On a **group** row, `enter` spawns a new agent in that group with the prompt embedded, using the group's default path. The spawn tool starts at the Settings (`s`) default and `tab` cycles it (claude ↔ opencode ↔ any configured tool); the footer shows the current pick. The agent starts working on the prompt immediately.

`esc` closes the bar. The new-session form's optional `prompt` field launches an agent the same way; tools whose CLI takes the prompt behind a flag declare it with `prompt_flag` (see [Configuration](#configuration)).

### Groups

Groups are paths (`backend/api/auth`) forming a tree of unlimited depth. Sessions can live at any node, including the root. Create subgroups inline with `g`, and reorder both groups and sessions with `shift+↑↓`; the order persists.

### Status

Each session's tmux pane is polled (default every 2s) to derive a status:

| Status | Meaning |
|--------|---------|
| `working` | The agent is busy on a turn |
| `waiting` | Blocked on you: a dialog, a permission ask, or a plain-text question |
| `finished` | Turn ended — an alert that clears to `idle` once you enter the session |
| `errored` | The tool reported an error |
| `idle` | Nothing running |
| `dead` | The tmux session is gone |

Detection matches per-tool regex rules against the visible pane, analyzes the newest turn to tell `finished` from `waiting`, and treats streaming output (content changing between polls) as `working`. Polling keeps running while you are inside a session, so statuses stay live. The selected session's pane tail renders in the preview panel, and moving the cursor fetches the preview immediately.

### Stats

The header shows a fleet summary: per-status session counts and the combined CPU/RAM of every live agent's full process tree. The Computer block in the sessions panel shows machine gauges: CPU, memory (used/total), swap, root-disk free space, and network up/down rates.

## Configuration

Config lives in your OS user config dir (`~/Library/Application Support/agent-manager/config.toml` on macOS, `~/.config/agent-manager/config.toml` on Linux) and is created on first run with working defaults for Claude Code and OpenCode.

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

Rules match top-down against the visible pane text; first match wins, and `default_status` applies when nothing matches. Optional per-tool fields refine detection: `activity_cutoff` (regex locating the tool's input box, everything above it is turn content), `turn_end` (a turn-summary line marking the turn as over), `chrome_line`, `blocked_line`, and `trailing_note`. The generated config's `claude` and `opencode` blocks show all of them in use.

`prompt_flag` controls how the new-session form's optional prompt is embedded into the launch command. Tools that take the prompt as a positional argument (Claude Code: `claude 'the prompt'`) leave it empty; tools whose positional argument means something else declare the flag (OpenCode: `prompt_flag = "--prompt"`, since its positional argument is the project path). The prompt only shapes the launch command; revive (`v`) uses `revive_command` untouched.

State is stored next to the config in `state.db` (SQLite).

## Development

```bash
go test ./...   # includes end-to-end tests against a real tmux server
go run .
```

## License

[AGPL-3.0](LICENSE)
