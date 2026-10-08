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
  finds none awake, it starts `agent-manager serve --background` there. That
  headless manager runs the same poller as the TUI.

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
| Message an agent | `send <id> <text> --from <sender> --json` |
| Spawn | `spawn --json ...`, `terminal create --json ...` |
| Lifecycle | `kill`, `revive`, `archive [--restore]`, `terminal close` |
| Groups | `create-group`, `delete-group` |
| Attach | `ssh -t <dest> tmux -L agentmgr attach-session -t <session>` |

The snapshot envelope carries `version`. A remote that answers with an
unknown version, or does not know `snapshot`, is shown offline with the
message to update agent-manager on that host.

`send --from` is for a caller with no session on that host, such as an agent
or the user on the local machine. The delivered message names the sender and
says replies cannot reach it, the way a message from a terminal does.

### SSH invocation

Every call runs `ssh` with:
- `BatchMode=yes`, so it never prompts.
- `ForwardAgent=no` and `ClearAllForwardings=yes`.
- `ConnectTimeout=5` and server-alive limits.
- A per-profile `ControlMaster`/`ControlPersist` socket under a short private
  directory, so a poll does not open a new connection each time.

The remote command runs in the user's login shell (`exec "$SHELL" -lc ...`),
so the `PATH` the user set up there finds the binary, wherever it was
installed. Every argument is single-quoted. Each call has a deadline, and
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
