# SSH connections

An SSH connection puts the sessions of an agent-manager on another machine in
this manager's list. This is the shape agreed on #552.

## Shape

- **Connections are created in the app.** The user adds one the way they add
  a group: a name and an SSH destination (`user@host` or an alias from
  `~/.ssh/config`). The manager stores it in `state.db`. There is no
  `fleet.json`.
- **A connection sits in the list like a group.** It has its own color and an
  `ssh` label. Under it are the remote host's groups and sessions, in the
  remote host's order. There can be any number of connections.
- **Each host owns its sessions.** Remote rows come from the remote manager's
  `snapshot --json` and live only in memory. Nothing remote is written to the
  local store. An unreachable host keeps its last rows, marked offline, and
  refuses actions.
- **Control flows outward.** The local machine holds the SSH keys and drives
  the remote one through the remote CLI. A remote host never connects back.
  On a remote host the MCP server sees and drives that host's sessions only.
- **The remote manager keeps running.** Queued messages, status and
  notifications need a manager running on the remote host. When a connection
  finds none awake, it starts `agent-manager serve --background` there, at
  most once a minute. That headless manager runs the same poller as the TUI,
  delivering queued messages and ending sessions after their turn. It logs to
  `serve.log` in the profile directory. Two managers on one host share the
  work the way two TUIs do.

## Identity

A remote row is addressed as `<connection>::<id>`, for example
`build-box::a1b2c3d4`. A connection name is non-empty, has no `::`, `/`,
control characters or surrounding spaces, and is unique. Local ids stay
unqualified, so every existing command, MCP tool and script keeps working.

## Protocol

The protocol is the remote host's own CLI, run over SSH as
`agent-manager <command> --json`:

| Need | Remote command |
|---|---|
| Inventory | `snapshot --json`: sessions, terminals, groups, and whether a manager is awake |
| Start the manager | `serve --background` |
| Read a screen | `read <id> --json`, `terminal read <id> --json` |
| Message an agent | `send --from=<sender> --json -- <id> <text>` |
| Spawn | `spawn --json ...`, `terminal create --json ...` |
| Lifecycle | `kill`, `revive`, `archive [--restore]`, `terminal close` |
| Groups | `create-group`, `delete-group` |
| Attach | `ssh -t <dest> tmux -L agentmgr attach-session -t <session>` |

The snapshot envelope carries `version`. A remote that answers with an
unknown version, or does not know `snapshot`, is shown offline with the
message to update agent-manager on that host.

Flags are written as `--flag=value` and operands follow a bare `--`, so a
message that looks like a flag stays a message.

An SSH call has no calling session on the remote host. The commands it uses
run without one:
- `kill`, `revive`, `archive`, `create-group` and `delete-group` act as the
  user would.
- `terminal create` opens a terminal that belongs to no session.
  `terminal send`, `read` and `close` drive only such terminals. A terminal
  nested under a session stays that session's.
- `send --from` names the sender, at most 64 bytes. The delivered message says
  it came from another of the user's machines and that a reply cannot reach
  it. Every `--from` sender shares one rate limit per target.

Tasks, file reservations, rename and review still need a calling session.

### SSH invocation

Every call runs `ssh` with:
- `BatchMode=yes`, so it never prompts.
- `ForwardAgent=no` and `ClearAllForwardings=yes`.
- `ConnectTimeout=5` and server-alive limits.
- A per-profile `ControlMaster`/`ControlPersist` socket under a short private
  directory, so a poll does not open a new connection each time.

The remote command runs in the user's login shell (`exec "$SHELL" -lc ...`),
so the `PATH` the user set up there finds the binary, wherever it was
installed. Every argument is single-quoted. The command prints a marker once the login profile has run, and the client reads only what follows it, so a profile that prints to stdout does not break the answer. The client strips control characters and escape sequences from every string a host answers with, drops rows whose ids are malformed, caps output at 8 MiB, and kills ssh's whole process group at the deadline. A stored connection is validated again before any call uses it. Each call has a deadline, and
each host has one call in flight at a time.

## Pieces

| Piece | Where | Owns |
|---|---|---|
| Connection store | `internal/store/connections.go` | The `connections` table and its CRUD |
| Snapshot and `send --from` | `internal/sessioncmd`, `internal/cli` | The envelope type, `snapshot --json`, sending from outside the host |
| Headless manager | `internal/app`, `main.go` (`serve`) | Running the poller and after-turn work without a TUI; `--background` detaches one when none is awake |
| SSH client | `internal/remote` | Refs, validation, the SSH command, snapshot, routed operations, starting the remote manager |
| UI | `internal/ui/connection_*.go` | The connection dialog, connection rows, remote rows, routing actions to `internal/remote` on the effect lane |
| MCP | `internal/mcpserver/remote*.go` | Listing every connection with qualified ids, routing qualified ids |

## Not in scope

These are refused on a remote row with a message naming the gap:
- Rename, move, delete, restart and fork.
- Diff review.
- The editor.
- File reservations and tasks, which stay per host.

Focus and attach on a remote row open an SSH attach in the terminal, not the
embedded focus view.
