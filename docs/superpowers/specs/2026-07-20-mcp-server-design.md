# agent-manager MCP Server Design

## Goal

Every agent (claude, codex, opencode, grok) discovers and uses agent-manager's
session commands natively, in every session including resumed ones, with zero
prompt injection and zero user setup beyond the homebrew install.

## Architecture

One new subcommand, `agent-manager mcp`, runs a stdio MCP server inside the
existing binary (SDK: github.com/modelcontextprotocol/go-sdk). The server
identifies its session from the AGENT_MANAGER_SESSION_ID environment variable
and writes the same mailbox files the CLI subcommands write today; the manager
poller applies them unchanged.

The manager registers the server automatically at spawn and revive, both of
which funnel through buildLaunch. Registration style is per tool:

| Style | Mechanism |
|---|---|
| claude | generated JSON at `<configDir>/hooks/mcp-claude.json`, `--mcp-config <path>` appended to the command |
| codex | `-c 'mcp_servers.agent-manager...'` overrides appended to the command, `env_vars` forwards AGENT_MANAGER_SESSION_ID |
| opencode | generated JSON at `<configDir>/hooks/mcp-opencode.json`, OPENCODE_CONFIG env var points at it |
| grok | one-time `grok mcp add --scope user` registration, marker file prevents re-runs |
| none | no registration |

Tool config gains `mcp = "<style>"`. Empty resolves to the tool's config key
when it names a known style, otherwise none, so stock configs work untouched
and custom tools opt in explicitly.

Generated configs reference the running binary via os.Executable(), so the
homebrew path (or any other install location) is always correct.

## Tools exposed

- `rename` (name): name the session after its task. Same rules as the rename
  subcommand.
- `review_repo` (path): declare the repo or worktree under active work so
  review opens there.
- `review_base` (ref, repo_path optional): declare the ref review diffs
  against; "auto" clears the override. repo_path defaults to the server's
  working directory.

Tool logic lives in a shared internal package (`internal/sessioncmd`) used by
both the CLI subcommands and the MCP server, so behavior and validation stay
identical.

## Error handling

Tool calls return MCP tool errors (isError result) with the same messages the
CLI prints. A missing or malformed session id fails every call. The server
exits when stdin closes (client gone).

## Testing

- sessioncmd unit tests (moved CLI logic keeps its coverage).
- MCP round-trip test: in-process client connects over a pipe transport,
  lists tools, calls each, asserts mailbox files.
- buildLaunch tests per style: command string, env map, generated file shape.
