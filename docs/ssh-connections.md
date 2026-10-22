# SSH connections

An SSH connection puts the sessions of an agent-manager on another machine in this manager's list. This is the shape agreed on #552.

## Shape

- **Connections are created in the app.** The user adds one the way they add a group: a name and an SSH destination (`user@host` or an alias from `~/.ssh/config`). The manager stores it in `state.db`. There is no `fleet.json`.
- **A connection sits in the list like a group.** It has its own color and an `ssh` label. Under it are the remote host's groups and sessions, in the remote host's order. There can be any number of connections.
- **Each host owns its sessions.** Remote rows come from the remote manager's `snapshot --json` and live only in memory. The local store keeps only which local session opened a remote terminal through MCP. A host whose refresh failed keeps its last rows and refuses actions. Its row says why: `unreachable` when ssh failed or timed out, `agent-manager missing`, `agent-manager too old` or `agent-manager newer` when ssh got through, and `error` for anything else, with the full message in the detail pane.
- **Control flows outward.** The local machine holds the SSH keys and drives the remote one through the remote CLI. A remote host never connects back. On a remote host the MCP server sees and drives that host's sessions only.
- **The remote manager keeps running.** Queued messages, status and notifications need a manager running on the remote host. When a connection finds none awake, it starts `agent-manager serve --background` there, at most once a minute. That headless manager runs the same poller as the TUI, delivering queued messages and ending sessions after their turn. It logs to `serve.log` in the profile directory. Two managers on one host share the work the way two TUIs do.

## Identity

A remote row is addressed as `<connection>::<id>`, for example `build-box::a1b2c3d4`. A connection name is non-empty, has no `::`, `/`, control characters or surrounding spaces, does not end in `:`, and is unique. Local ids stay unqualified, so every existing command, MCP tool and script keeps working.

## Protocol

The protocol is the remote host's own CLI, run over SSH as `agent-manager <command> --json`:

| Need | Remote command |
|---|---|
| Inventory | `snapshot --json`: sessions, terminals, groups, and whether a manager is awake |
| Start the manager | `serve --background` |
| Read a screen | `read <id> --styled --json`, `terminal read <id> --styled --json`, keeping only its color sequences on this side; a host that predates `--styled` is read again without it |
| Message an agent | `send --from=<sender> --json -- <id> <text>` |
| Answer an agent from the quick bar | `type --json -- <id> <text>` |
| Spawn | `spawn --json ...`, `terminal create --json ...` |
| Lifecycle | `kill`, `revive`, `archive [--restore]`, `terminal close` |
| Groups | `create-group`, `delete-group` |
| Attach | `ssh -t <dest>` running `tmux -L agentmgr attach-session -t <session>`, then taking back any review or editor request the host's keys left on its tmux server |

The snapshot envelope carries `version`. A remote that does not know `snapshot`, or answers with an older version, says to update agent-manager on that host. One that answers with a newer version says to update it on this machine.

Flags are written as `--flag=value` and operands follow a bare `--`, so a message that looks like a flag stays a message.

Once a connection is saved, every agent on this machine can reach that host through MCP: any session can open a terminal there with `create_terminal` and run commands in it as the remote user. Add a connection only for a host you would let all of them use.

An SSH call has no calling session on the remote host. The commands it uses run without one:
- `kill`, `revive`, `archive`, `create-group` and `delete-group` act as the user would.
- `terminal create` opens a terminal that belongs to no session. `terminal send`, `read` and `close` drive only such terminals. A terminal nested under a session stays that session's.
- Every SSH call is the same caller there, so the local MCP server tells them apart: it records which session's `create_terminal` opened a remote terminal, and `send_terminal` and `close_terminal` on a `host::id` refuse any other session before calling ssh. A terminal opened with `T` on the connection row has no such record, so only the user drives it. The record goes when the terminal is closed through MCP or its session is deleted.
- `send --from` names the sender, at most 64 bytes. The delivered message says it came from another of the user's machines and that a reply cannot reach it. Every `--from` sender shares one rate limit per target.
- `type` submits the user's quick bar prompt in an agent's pane at once, as the local quick bar does. It is refused from inside a session. It carries no envelope and no rate limit, so it sits outside the guardrail the other commands keep: anything that escapes caller detection can type into any agent as the user. Each pane's send holds a lock file beside the tmux socket, so a typed prompt and the host manager's own delivery never interleave. When the host's paste started before the send failed, `type` exits with status 3; that, or a call cut off after agent-manager started there, means the prompt may sit in the pane, so the quick bar drops it rather than type it again, as it does on this machine.

Tasks, file reservations, rename and review still need a calling session.

### SSH invocation

Every call runs `ssh` with:
- `BatchMode=yes`, so it never prompts. A host key ssh has not seen fails with "Host key verification failed", so the user connects once by hand before adding the host. The connection's error says so, and says when the key changed instead.
- `ForwardAgent=no`, `ForwardX11=no` and `ClearAllForwardings=yes`.
- `ConnectTimeout=5` and server-alive limits.
- A per-profile `ControlMaster`/`ControlPersist` socket under a short private directory, so a poll does not open a new connection each time. Not on Windows: its `ssh.exe` has no connection sharing, so each call there opens its own connection.

The remote command runs in the user's login shell (`exec "$SHELL" -lc ...`), so the `PATH` the user set up there finds the binary, wherever it was installed. A connection can name the binary instead, as an absolute path or one under `~/`, which skips the `PATH` lookup; `~/` expands against the remote `$HOME`. Every argument is single-quoted. The command prints a marker once the login profile has run, and the client reads only what follows it, so a profile that prints to stdout does not break the answer. The client strips control characters and escape sequences from every string a host answers with, drops rows whose ids are malformed, caps output at 8 MiB, and kills ssh's whole process group at the deadline. A stored connection is validated again before any call uses it. Each call has a deadline, and each host has one call in flight at a time. A host whose refresh failed is tried again after a wait that doubles from two seconds to thirty while it stays down.

## Pieces

| Piece | Where | Owns |
|---|---|---|
| Connection store | `internal/store/connections.go` | The `connections` table and its CRUD |
| Snapshot and `send --from` | `internal/sessioncmd`, `internal/cli` | The envelope type, `snapshot --json`, sending from outside the host |
| Headless manager | `internal/app`, `main.go` (`serve`) | Running the poller and after-turn work without a TUI; `--background` detaches one when none is awake |
| Connection values | `internal/remote/value` | A connection and its validation, `<connection>::<id>` refs, failure kinds and the retry wait, with no SSH, so the UI packages can use them |
| SSH client | `internal/remote` | The SSH command, snapshot, routed operations, starting the remote manager, and the rule that a session drives only the remote terminals it opened |
| UI | `internal/ui/connections`, `internal/ui/connections_adapter*.go`, `internal/ui/connections_effect.go` | The package: the list, each host's last refresh, remote rows, the selected row's screen and the connection dialog. The root adapters: the client, refreshes, rail and content painting, routing actions to `internal/remote` on the effect lane |
| MCP | `internal/mcpserver/remote.go` | Listing every connection with qualified ids and routing qualified ids to `internal/remote` |
| Opener records | `internal/store/remote_terminals.go` | Which local session opened each remote terminal |

## Platforms

The local machine can be Linux, macOS or Windows. The remote host must be Linux or macOS, since the remote command runs in a POSIX login shell. A host whose login shell is csh or tcsh fails every call with an error that says so. A Windows remote is not supported.

## Not in scope

These are refused on a remote row with a message naming the gap:
- Rename, move, delete, restart and fork.
- Diff review.
- The editor.
- Copying a reply, marking idle, cancelling an after-turn end, reordering.
- Kill all and revive all.
- File reservations and tasks, which stay per host.

Focus and attach on a remote row open an SSH attach in the terminal, not the embedded focus view. Inside it, the host's review and editor keys detach like its back key does, and the manager says they work only for sessions on this machine. The host's own manager never sees the request, since the attach takes it back off that host's tmux server.
