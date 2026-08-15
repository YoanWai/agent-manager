# Nested terminals design

Date: 2026-08-15
Status: approved

## Goal

A terminal can hang under the agent it belongs to, so a related shell sits with its work in the list. Nesting is optional. `m` moves a terminal into a session or back to a group. The list is one inline tree.

## Decisions

- A terminal is still a session whose tool has `shell = true`. Nesting is a parent pointer, not a new kind of row.
- Only a shell may have a parent. That parent is an agent. One level.
- Empty `parent_id` means the terminal is a sibling in its group.
- A child's `group_name` is always the parent's group.
- `T` on an agent, and MCP `create_terminal` with `nest` omitted or true, nest under that agent.
- MCP `nest` omitted means true. The argument is optional `*bool`; a plain `bool` would treat omit as false.
- The agent sets `nest: false` for a shared or unrelated shell. `m` fixes a wrong nest after the fact.
- Kill, archive, delete, restore, and revive on an agent include its terminals. The same keys on a child affect that child only.
- Restart and fork stay on the agent. Children stay put. Fork does not copy terminals.
- One list. The pinned Terminals block and the `terminal rows` setting go away. A stored `terminal_placement` value is ignored.

## Model

`sessions` gains `parent_id TEXT NOT NULL DEFAULT ''`.

Store graph rules, enforced in `PlaceSession`:

- `parent_id` empty is valid.
- A non-empty parent must exist, must not be the row itself, and must itself have an empty `parent_id`.
- The child's `group_name` is set to the parent's `group_name`.

Shell-versus-agent is a config fact, not a store column. `PlaceSession` accepts any parent that passes the graph rules. UI and `sessioncmd` refuse a shell as parent. Renaming an agent (`r`, tab through tools) that has children refuses a shell tool: move the terminals first.

`PlaceSession(id, group, parentID)` replaces `MoveSession(id, group)`. Sort order is assigned among siblings that share `(group_name, parent_id)`.

`Children(parentID)` returns the rows whose `parent_id` is that id, including archived, ordered by `sort_order, created_at`.

`CreateSession` writes `parent_id` and assigns `sort_order` in that sibling set.

`Delete`, `SetArchived`, and `UpdateStatus` stay one-id. Callers that mean "agent plus terminals" pass the set they already built for confirm. An orphan `parent_id` (parent row gone) paints as un-nested.

`ReorderSession` / `SwapSessionOrder` only swap rows that share `(group_name, parent_id)`.

Moving an agent to another group updates the agent's `group_name` and every child's `group_name`. `parent_id` stays.

Group rename, group move, and group archive already rewrite or cover those rows through `group_name`. They need no parent-specific writes.

## List

`rebuildRows` walks each group as it does today, then for each un-nested session emits that row and, when it is an agent, its children at `depth+1` before the next sibling. The existing `├─` / `╰─` guides cover the extra depth.

Children are always shown. `enter` still focuses. `F` still folds groups.

Search: a matching child also lists its parent, so the indent has a home.

`w` and group dots stay agent-only (`listedAgents`). Shells do not count as work.

`J` / `K` (`reorderSelected`): an un-nested row swaps with the next un-nested sibling in that group (children ride with the agent). A nested shell swaps only with siblings under the same parent. `visibleReorderTarget` uses `parent_id`, not "any session in the same group".

## Create

`T` (`openTerminal`):

| Cursor | Parent | Group | Directory |
| --- | --- | --- | --- |
| Agent | that agent | that agent's group | that row's directory (unchanged) |
| Group | empty | that group | group default path (unchanged) |
| Nested shell | that shell's parent | that parent's group | that shell's directory |
| Un-nested shell | empty | that shell's group | that shell's directory |

MCP `create_terminal`:

- `nest` omitted or `true`: parent is the caller, group is the caller's group. An explicit `group` that differs from the caller is an error (`set nest false to place in another group`). An explicit `group` that matches the caller is accepted.
- `nest: false`: `parent_id` empty; `group` and `directory` behave as they do today.
- `directory` is independent of nest.

`CreateTerminalOptions` gains `Nest *bool`. `list_terminals` returns `parent_id` and the parent's name (empty when un-nested). Server instructions and the tool description say the new terminal nests under the caller unless `nest` is false.

## Move

`m` on a terminal lists groups and agents. Groups stay the tree they are today. Each live or dead, non-archived agent appears under its group.

- Terminal onto an agent: `PlaceSession(id, agent.Group, agent.ID)`.
- Terminal onto a group: `PlaceSession(id, group, "")`.
- Onto the current parent or the current un-nested group: no-op, close the picker.

`m` on an agent or a group lists groups only. Moving an agent runs `PlaceSession` on the agent and updates every child's `group_name`.

Help text for `m`: move a session to a group, or a terminal into a session.

## Follow

When the cursor is on an agent, kill / archive / delete / restore / revive collect `session` plus `Children(id)` into `confirm.sessions`. The confirm label names the extra terminals: `kill nested-terminals and 2 terminals? frees their RAM, v revives them.` Zero children keeps today's single-session wording.

The same keys on a child collect that child only.

`applyConfirmedArchived` writes every id in `confirm.sessions`. Today a non-group archive/restore writes only `sessions[0]`; that becomes a loop.

Group kill / archive / delete already include children through `group_name`. No extra collection.

Restart (`R`) and fork (`f`) stay single-session. Children are left running under the restarted or source agent. A fork starts with no terminals.

## Pinned block

Remove:

- the Terminals divider and the tail of `rebuildRows` that collected pinned shells
- `shellsPinned`, `pinnedShells`, `pinnedShell`, `treeRows` as a split of the rail
- Settings `terminal rows` and `terminal_placement` reads/writes
- pinned-only cases in `terminals_section_test.go`

Shells always sit in their group, with `❯` when resting. A leftover `terminal_placement` setting is ignored.

## Error handling

- `PlaceSession` to a missing parent, to self, or to a parent that already has a parent: error, no write.
- UI / MCP parent that is a shell: error naming it as a shell.
- `r` changing an agent with children to a shell tool: error, keep the current tool.
- `create_terminal` with `nest` true and a `group` other than the caller's: error telling the caller to set `nest` false.
- Confirm actions that include children fail on the first child error and leave the remaining rows as they are, same as today's multi-session kill.

## Testing

Store (`store_test.go`):

- `CreateSession` round-trip of `parent_id`
- `PlaceSession` nest, un-nest, reparent; child's group follows the parent
- reject self, missing parent, and a parent that already has a parent
- moving an agent updates children's `group_name`
- `ReorderSession` only swaps the sibling set
- `Children` includes archived
- deleting a parent leaves the child row (caller-owned follow); list paints it un-nested

UI:

- `T` parent from each cursor case in the create table
- `m` onto an agent nests; onto a group un-nests; agent picker hidden when moving an agent
- kill / archive / delete confirm on an agent includes children; on a child does not
- archive/restore persists every id in the confirm set
- `rebuildRows` indents children; search keeps the parent of a matching child
- `J`/`K` on an agent skips child rows; on a child stays inside that parent
- Settings has no `terminal rows`; a stored `terminal_placement=pinned` still paints inline

MCP / `sessioncmd`:

- omitted `nest` and `nest: true` set `parent_id` to the caller
- `nest: false` leaves `parent_id` empty
- `nest` true plus a different `group` errors
- `list_terminals` includes `parent_id` and parent name

## Docs

`docs/usage.md` Terminal tabs: shells live in the tree; `T` on an agent nests; `m` moves a terminal into a session or a group. Drop the pinned-block section and the settings mention. MCP table: `create_terminal` nests under the caller unless `nest` is false. `?` help line for `m` matches.

## Out of scope

- Nesting an agent under an agent.
- Folding an agent's children (`enter` stays focus).
- A dedicated "un-nest" key; `m` onto a group is that action.
- Cascading `Delete` inside the store.
