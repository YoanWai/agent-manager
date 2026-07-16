# Agent Manager

A terminal UI to manage AI coding-agent sessions on your machine. Create and enter sessions for Claude Code, OpenCode, or any CLI tool; organize them in a nested group tree with manual ordering; archive finished ones; watch live status, a live pane preview, the combined footprint of your agents, and machine resource gauges.

## Requirements

- Go 1.24+
- tmux

## Install

```bash
git clone https://github.com/YoanWai/agent-manager.git
cd agent-manager
go install .
```

Installs `agent-manager` to `$(go env GOPATH)/bin`.

## Usage

```bash
agent-manager
```

Sessions run inside tmux (`am_*` namespace), so they survive the manager quitting. Inside a session, **Ctrl+Q** detaches back to the manager.

### Keys

| Key | Action |
|-----|--------|
| `n` | New session (name, tool, directory, group picker) |
| `g` | New group (name, parent, default path) |
| `enter` | Attach session / fold group |
| `ctrl+q` | Inside a session: back to the manager |
| `shift+↑` / `shift+↓` | Reorder session or group among its siblings |
| `m` | Move session to another group |
| `r` | Rename session or group |
| `a` / `u` | Archive / restore |
| `d` | Delete session, or a group + its entire subtree |
| `space` | Collapse / expand group |
| `t` | Toggle archived view |
| `/` | Search |
| `ctrl+r` | Force refresh |
| `?` | Help |
| `q` | Quit (sessions keep running) |

### Groups

Groups are paths (`backend/api/auth`) forming a tree of unlimited depth. Sessions can live at any node, including the root. Create subgroups inline with `g`, and reorder both groups and sessions with `shift+↑↓`; the order persists.

### Status

Each session's tmux pane is polled (default every 2s) and matched against per-tool regex rules to derive a status: `working`, `ready`, `errored`, `idle`, or `dead`. The selected session's pane tail renders live in the preview panel, and moving the cursor fetches the preview immediately.

### Stats

The header shows a fleet summary: per-status session counts and the combined CPU/RAM of every live agent's full process tree. The Computer block in the sessions panel shows machine gauges: CPU, memory (used/total), swap, root-disk free space, and network up/down rates.

## Configuration

Config lives in your OS user config dir (`~/Library/Application Support/agent-manager/config.toml` on macOS, `~/.config/agent-manager/config.toml` on Linux) and is created with defaults on first run:

```toml
poll_interval = "2s"

[tools.claude]
command = "claude"
default_status = "idle"
rules = [
  { state = "working", pattern = "esc to interrupt" },
  { state = "errored", pattern = "(?i)^error:" },
]
```

Add any CLI tool as a `[tools.<name>]` block with a `command` and status `rules` (first match wins; `default_status` applies when nothing matches).

State is stored next to the config in `state.db` (SQLite).

## Development

```bash
go test ./...   # includes end-to-end tests against a real tmux server
go run .
```

## License

[MIT](LICENSE)
