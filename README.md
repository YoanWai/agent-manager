<h1 align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/brand/wordmark-dark.svg">
    <img src="docs/brand/wordmark-light.svg" alt="agent-manager" width="340">
  </picture>
</h1>

<p align="center"><b>The fastest workflow for every AI coding agent.</b></p>

<p align="center"><!-- trendshift:start --><a href="https://trendshift.io/repositories/89312?utm_source=trendshift-badge&amp;utm_medium=badge&amp;utm_campaign=badge-trendshift-89312" target="_blank" rel="noopener noreferrer"><img src="https://trendshift.io/api/badge/trendshift/repositories/89312/daily?language=Go" alt="agent-manager on Trendshift" width="250" height="55"></a><!-- trendshift:end --></p>

<p align="center">
  <a href="https://github.com/YoanWai/agent-manager/stargazers"><img src="https://img.shields.io/github/stars/YoanWai/agent-manager?style=for-the-badge&label=stars&labelColor=1f2328&color=96591f" alt="stars"></a>
  <a href="https://github.com/YoanWai/agent-manager"><img src="https://img.shields.io/endpoint?url=https%3A%2F%2Fraw.githubusercontent.com%2FYoanWai%2Fagent-manager%2Fbadges%2Fclones.json&style=for-the-badge&labelColor=1f2328" alt="clones in the last 14 days"></a>
  <a href="https://github.com/YoanWai/agent-manager/releases/latest"><img src="https://img.shields.io/github/v/release/YoanWai/agent-manager?style=for-the-badge&label=release&labelColor=1f2328&color=2f5f8f" alt="latest release"></a>
  <br>
  <a href="https://github.com/YoanWai/agent-manager/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/YoanWai/agent-manager/ci.yml?branch=main&style=for-the-badge&label=build&labelColor=1f2328&color=2f7f62" alt="CI status"></a>
  <a href="https://pkg.go.dev/github.com/YoanWai/agent-manager"><img src="https://img.shields.io/badge/go.dev-reference-007d9c?style=for-the-badge&logo=go&logoColor=white" alt="Go package reference"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/YoanWai/agent-manager?style=for-the-badge&label=license&labelColor=1f2328&color=59636e" alt="licence"></a>
</p>

![five prompts to five fresh agents without moving the cursor, one per CLI, then the blocked one answered and its diff opened](docs/demo.gif)

Claude Code, Codex, OpenCode, Grok, Gemini CLI, and Pi run side by side. Each tool runs in its own persistent tmux session.

agent-manager is a thin layer over the CLIs you already have. Each session launches your own installed tool as-is: your login, your subscription, your config files, your MCP servers, and every feature the tool ships all carry over, exactly as they behave in a plain terminal.

Instead of hunting through terminal tabs to see which agent is done and which is stuck, every session shows up in one list with live status, grouped into a project tree you can fold and reorder. You answer any of them without attaching: `space` sends a prompt straight into a session's pane, or spawns a new agent in the selected group. A dead session revives on its own conversation with `v`. And `ctrl+r` opens a full-file diff of what an agent changed, syntax-highlighted, where the comments you leave on lines go back to the agent's pane as one review prompt when you press `C`.

Press `f` on a session to continue its conversation in a separate named fork.

The work around the agents lives in the same list: `T` opens a plain shell tab in the selected group for the commands you run yourself, and `o` opens the row's directory in your editor.

Not here yet: cost tracking, mouse-driven navigation, and agents that can talk to each other.

**Jump to:** [Install](#install) · [Usage](#usage) · [Keys](docs/usage.md#keys) · [Diff review](docs/usage.md#diff-review) · [Configuration](docs/configuration.md)

## Supported tools

Status detection supports **Claude Code**, **OpenCode**, **Codex**, **Grok Build**, **Gemini CLI**, and **Pi** by default. Other CLI tools can run as sessions. Add a `[tools.<name>]` block to give another tool live status rules (see [Configuration](docs/configuration.md)).

## Install

Runs on macOS and Linux, and on Windows inside [WSL2](docs/install.md#windows).

### Homebrew (macOS / Linux)

```bash
brew install yoanwai/tap/agent-manager
```

Installs tmux with it if missing. The tap ships a cask, so an install from the older formula switches over with `brew uninstall agent-manager` followed by the command above.

### Install script (macOS / Linux)

```bash
curl -fsSL https://raw.githubusercontent.com/YoanWai/agent-manager/main/install.sh | sh
```

Downloads the latest release for your platform, verifies it against the published checksums, and installs it to `~/.local/bin`. Set `AGENT_MANAGER_INSTALL_DIR` for another directory and `AGENT_MANAGER_VERSION` to pin a version. Install tmux with your own package manager.

Arch Linux, mise, `go install`, prebuilt binaries, Windows (WSL2), and updating: [docs/install.md](docs/install.md).

## Usage

```bash
agent-manager
```

Sessions run inside tmux (`am_*` namespace), so they survive the manager quitting. Inside a session, **Ctrl+Q** detaches back to the manager and **Ctrl+R** opens its diff review. `agent-manager --version` prints the version.

Agent sessions live on a private tmux server named `agentmgr`, so they never mix with the tmux you run yourself and a `kill-server` on your own socket leaves them alone. To reach one from a plain shell, name that server: `tmux -L agentmgr ls`, then `tmux -L agentmgr attach -t am_<id>`.

The full reference, every key, the quick prompt, killing and reviving, diff review, groups, status detection, stats, and themes, lives in [docs/usage.md](docs/usage.md). The short version:

| Key | Action |
|-----|--------|
| `n` | New session (name, tool, directory, optional starting prompt, group) |
| `space` | Quick prompt: answer the selected session, or spawn an agent in the selected group |
| `enter` | Focus the session in place; keys go to the agent while the list stays |
| `ctrl+r` | Review the session's changes as full-file diffs; `c` comments a line, `C` sends the comments to the agent |
| `x` / `v` | Kill a session to free its RAM / revive it on its own conversation |
| `R` | Restart a session on an empty context: same name, group, directory and tool, fresh conversation |
| `s` | Settings (default tool, theme, list density, review layout) |
| `?` | The key map: every binding, grouped and scrollable, with `/` to search it |

A session can spawn into its own git worktree (`<repo>-worktrees/<name>`, branch `am/<name>`), toggled on the `n` form, with `alt+w` in the quick prompt, or by default in Settings.

![the session tree, with a waiting agent's permission prompt in the preview](docs/screenshot-sessions.png)

![review, side by side, with the changed lines tinted in full file context](docs/screenshot-review.png)

Configuration (adding your own tools, status rules, revive commands) is in [docs/configuration.md](docs/configuration.md).

## Development

```bash
go run .
env -u TMUX TMUX_TMPDIR=/tmp/amtest go test ./...   # end-to-end tests drive a real tmux server
```

See [CONTRIBUTING.md](.github/CONTRIBUTING.md) for the checks CI runs, and [AGENTS.md](AGENTS.md) if you point a coding agent at this repo.

## Contributing

Bug reports, feature ideas, and pull requests are welcome. See [CONTRIBUTING.md](.github/CONTRIBUTING.md) for setup and the checks CI runs. Questions and setups worth sharing go in [Discussions](https://github.com/YoanWai/agent-manager/discussions). Security reports go through a [private advisory](https://github.com/YoanWai/agent-manager/security/advisories/new); see [SECURITY.md](.github/SECURITY.md).

### Contributors

<a href="https://github.com/YoanWai/agent-manager/graphs/contributors">
  <img src="https://contrib.rocks/image?repo=YoanWai/agent-manager" alt="Contributors" />
</a>

## License

[Apache-2.0](LICENSE)
