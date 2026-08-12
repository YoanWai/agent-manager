# Security Policy

## Supported versions

Security fixes land on the latest release. Upgrade to the newest version before reporting, in case the issue is already fixed.

| Version | Supported |
|---------|-----------|
| Latest release | ✅ |
| Older releases | Upgrade to latest |

## Reporting a vulnerability

Report privately through GitHub: **[Report a vulnerability](https://github.com/YoanWai/agent-manager/security/advisories/new)**. That opens a private advisory visible only to the maintainer.

Please include:

- What an attacker can do, and what they need to already have (local shell, a crafted repo, a malicious config, a hostile agent).
- Steps to reproduce, with the smallest input that triggers it.
- Version (`agent-manager --version`), OS, and tmux version.

You get an acknowledgement within 72 hours and an assessment within a week. Fixes ship in the next release, with credit in the advisory if you want it.

Keep the report private until a fix is released.

## What agent-manager touches

Useful context when judging whether something is a vulnerability:

- **tmux sessions.** The manager creates and drives sessions on its own tmux socket, and reads the visible text of each pane to derive status and render the preview. Pane content is treated as data, not commands.
- **Process spawning.** Sessions launch the CLI command from your config (`[tools.<name>].command`) and its revive command. Anything you put in that config runs as you.
- **Local state.** Config (`config.toml`) and state (`state.db`, SQLite) live in your OS user config directory. Session names, group paths, working directories, and review targets are stored there.
- **Generated agent config.** Launching Claude Code, Codex, OpenCode, Grok, Gemini, or an MCP-enabled Hermes tool writes or registers generated settings or MCP configuration so the agent gets status hooks and the agent-manager MCP tools.
- **MCP server.** `agent-manager mcp` speaks stdio to the agent in its own session and exposes `rename`, `review_repo`, and `review_base`. It identifies the calling session from its environment.
- **Git repositories.** Review mode runs read-only git commands against the repo a session declares or that ranking picks.
- **Network.** One request a day to the GitHub Releases API to check for a newer version.

Findings that involve one of these crossing a trust boundary in a way the docs do not describe are exactly what this policy is for.

## Out of scope

- Behavior of the AI agents themselves (Claude Code, Codex, OpenCode, Grok, Gemini, Pi, Hermes). Report those to their maintainers.
- Anything requiring an attacker to already have write access to your config file or your shell.
