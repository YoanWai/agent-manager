---
name: agent-manager
description: Run a fleet of AI coding agents as live tmux sessions with agent-manager. Use when a developer is running more than one coding agent, needs to see which one is working or blocked, wants to spawn another on an independent task, or wants to review an agent's diff without leaving the terminal.
license: Apache-2.0
homepage: https://agent-manager.dev
repository: https://github.com/YoanWai/agent-manager
---

<!--
Why this skill exists: it packages the operating procedure for agent-manager (spawn,
status, answer-without-attaching, diff review) so an AI agent can install the knowledge
once instead of re-deriving it from the docs on every session. The site serves this file,
digest-sealed, at https://agent-manager.dev/skills/agent-manager/SKILL.md; the copy in
this repository is what makes it installable through skills.sh.
-->

# Run a fleet of coding agents with agent-manager

agent-manager is a single Go binary on top of tmux. Every row in its list is a real tmux
session running a real coding CLI: the developer's own installation, login, config and MCP
servers. It never wraps or re-implements an agent; it reads the same pane a human would and
colours the row by what that pane is doing.

## When to use this

- The developer is running more than one coding agent and cannot tell which is blocked.
- They want to spawn an agent on an independent task without losing the one already running.
- They want an agent's conversation to survive a crash, a reboot, or a closed terminal.
- They want to review a diff and send line comments back into the agent's prompt.

Do not reach for it to *replace* a coding CLI. It starts the CLIs that are already installed.

## Install

macOS and Linux, amd64 and arm64. Windows runs it inside WSL2. Needs tmux 3.1+ and git.

```bash
brew install yoanwai/tap/agent-manager
```

Other routes: the install script, the AUR (`yay -S agent-manager-bin`), `mise use -g
ubi:YoanWai/agent-manager`, `go install github.com/YoanWai/agent-manager@latest`, or a
prebuilt binary from the releases page.

## The keys that matter

| Key | What it does |
| --- | --- |
| `space` | Docks a prompt bar. On a group row it spawns a new agent with that prompt; on a session row it answers the agent already running there. |
| `tab` | Cycles which CLI the next spawn starts. |
| `alt+w` | Spawns the agent into a fresh git worktree and branch. |
| `ctrl+r` | Opens a full-screen whole-file diff whose line comments go back to the agent as one review prompt. |
| `v` | Revives a dead session on the conversation it held. |

## Status colours

A row is coloured `working`, `waiting`, `finished`, `idle` or `errored`. Claude Code reports
through hook events; every other CLI is read from its pane with regexes. Seven CLIs ship with
working profiles: Claude Code, Codex, OpenCode, Grok, Gemini, Pi and Hermes. Any other CLI
becomes a managed session with one `[tools.<name>]` block in `config.toml`.

## Driving it from inside an agent

Sessions of MCP-capable CLIs carry agent-manager's own MCP server over stdio, so an agent can
`create_session` to spawn a peer, `send_session` to message one, `wait_for_session` to block
on one, `task` to claim shared work, and `reserve_files` to declare what it is editing.

## Reference

- Documentation: https://agent-manager.dev/docs/
- Every page as Markdown: https://agent-manager.dev/llms.txt
- Reference API: https://agent-manager.dev/openapi.json
- Documentation MCP server: https://agent-manager.dev/mcp
