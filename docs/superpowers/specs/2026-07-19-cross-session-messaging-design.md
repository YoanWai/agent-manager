# Cross-Session Awareness and Messaging

Date: 2026-07-19
Status: approved

## Goal

Every agent running inside agent-manager can see the other active sessions, read their screens, and hold two-way async conversations with them. The capability is self-teaching: the binary tells each session how to use it at launch, so it works for any supported tool (Claude Code, Codex, OpenCode, Grok Build, custom) on any machine with no per-user setup.

## CLI surface

Five new subcommands on the existing binary, alongside `rename`:

```
agent-manager sessions            list active sessions: id, name, tool, status, cwd, group
agent-manager peek <id|name>      print target session's current screen
agent-manager send <id|name> "…"  send a message to another session
agent-manager reply <msg-id> "…"  reply to a received message
agent-manager inbox [--all]       list my messages; default unread, --all includes seen
```

- Caller identity comes from `AGENT_MANAGER_SESSION_ID`, already present in every managed session's environment. `send`, `reply`, and `inbox` require it and fail with a clear error outside a managed session. `sessions` and `peek` work anywhere the config dir exists.
- Target resolution: exact id, unique id prefix, or exact name (case-insensitive). Ambiguous or unknown targets fail with the candidate list.
- `peek` shells out to `tmux capture-pane -p -t am_<id>` and strips ANSI escapes so agents get clean text.
- `inbox` prints each message as `[#<msg-id>] from <name> (<id>) <relative-time>: <body>` and marks printed unread messages as seen.
- `sessions` marks the caller's own row so an agent recognizes itself.

## Discovery

The launch directive in `internal/ui/form.go` grows a second line after the rename instruction:

```
You are one of several agent sessions managed by agent-manager. To list sibling sessions, read their screens, or message them: agent-manager sessions | peek <id> | send <id> "msg" | inbox.
```

Both `renameDirective` (embedded in the first prompt) and `deferredRenameDirective` (sent standalone) carry it. Injected messages self-describe the reply command, so a receiving agent knows what to do even without the primer.

Optional human sugar for Claude Code ships in the repo under `.claude/skills/`: `/rename-am` (rename this session from conversation context) and `/am-talk` (guide for messaging sibling sessions). The primer, not the skills, is the discovery mechanism.

## Data model

New table in `state.db`:

```sql
CREATE TABLE IF NOT EXISTS messages (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    from_session TEXT NOT NULL,
    to_session   TEXT NOT NULL,
    body         TEXT NOT NULL,
    created_at   INTEGER NOT NULL,
    delivered    INTEGER NOT NULL DEFAULT 0,
    seen         INTEGER NOT NULL DEFAULT 0
);
```

`delivered` records that the message was injected into the target's pane. `seen` records that the target printed it via `inbox`. A reply is a normal message row; `reply <msg-id>` resolves the original message's sender as the target, and its injected frame reads `in reply to #<msg-id>` so the receiver ties it back to their question.

Write ownership: the manager remains sole writer of `sessions`, `groups`, and `settings`. The `messages` table is shared between the manager process and subcommand processes. WAL mode already allows one writer plus concurrent readers across processes; subcommands open the database with `PRAGMA busy_timeout=3000` so brief write overlaps queue instead of erroring.

## Delivery flow

`send` does two things in order:

1. Insert the message row (durable, survives everything).
2. Attempt live injection: if `tmux has-session am_<target>` succeeds, send this text via the pane, then mark `delivered=1`:

```
[agent-manager message #<msg-id> from <sender-name> (<sender-id>)]: <body>
Reply with: agent-manager reply <msg-id> "<answer>". Then continue your prior task.
```

Injection reuses the proven `SendText` path: tools queue typed input mid-turn and process it when the current turn ends, so a busy agent finishes its work first and a reply never aborts in-flight work.

Poller backstop: each poll, for undelivered messages whose target session is alive and whose pane shows the tool's input box (same `ActivityRegion` readiness gate `maybeSendDirective` uses), the manager injects and marks delivered. This covers targets that were dead or booting at send time and mirrors the deferred-rename mechanism.

A message to an archived or deleted session stays undelivered and visible to the sender via `sessions` status; deleting a session deletes its messages both directions.

## Components touched

- `main.go`: subcommand dispatch for `sessions`, `peek`, `send`, `reply`, `inbox`.
- `internal/store/store.go`: `messages` table in `init`, plus `InsertMessage`, `MarkDelivered`, `MarkSeen`, `UnreadMessages`, `MessagesFor`, `UndeliveredMessages`, `Message(id)`, and message cleanup inside `Delete`.
- `internal/tmux/tmux.go`: existing `SendText`, `CapturePane`, `Exists` cover everything; subcommands construct their own `Driver`.
- `internal/ui/form.go`: primer line added to both rename directives.
- `internal/ui/poller.go`: undelivered-message redelivery step, modeled on `maybeSendDirective` and `applyPendingRename`.
- `.claude/skills/`: `rename-am` and `am-talk` skill files.

## Error handling

- Outside a managed session, `send`/`reply`/`inbox` fail: `not inside an agent-manager session (AGENT_MANAGER_SESSION_ID is unset)`.
- Unknown or ambiguous target: exit 1 with candidates.
- tmux missing or target pane gone at injection time: message stays queued, `send` still exits 0 and reports `queued (target not running, will deliver when it returns)` so agents distinguish delivered from queued.
- Database locked past busy_timeout: exit 1 with the sqlite error, message not silently dropped.
- Message body starting with `-` is rejected the same way session prompts are, avoiding flag parsing surprises.

## Testing

- Store: message CRUD round-trip, unread/seen transitions, cleanup on session delete, cross-process write with two open handles.
- CLI: target resolution (id, prefix, name, ambiguous, unknown), identity enforcement, inbox formatting and seen-marking, under `main_test.go` style table tests.
- Poller: redelivery gate honors alive + ready pane, marks delivered exactly once (fake tmux driver, as in existing poller tests).
- Directive: primer rides both rename directive variants.
- Manual end-to-end on a live isolated tmux socket (`env -u TMUX TMUX_TMPDIR=/tmp/amtest`): two sessions, send both directions, verify queue-then-resume behavior mid-turn.

## Out of scope (v1)

Group broadcast, cross-machine messaging, streaming chat, scrollback paging in `peek`, message threading beyond single reply context, MCP server exposure.
