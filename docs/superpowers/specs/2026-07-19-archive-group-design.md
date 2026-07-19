# Archive a group

## Goal

Let the user archive an entire group as one action: the group's label, all its
descendant subgroups, and every session under it move out of the active tree and
into the archived view. Restoring reverses it symmetrically.

## Current state

- Sessions carry an `archived` flag (`store.Session.Archived`, `sessions.archived`
  column). `SetArchived(id, bool)` flips one session.
- Groups have no archived flag. A group's label is derived: the active view shows
  the full group skeleton (`groupClosure`), the archived view shows only groups
  that still hold archived sessions (`pathsWithSessions`). So today a group label
  never leaves the active tree, even when all its sessions are archived.
- Keys: `a` archive, `u` restore, `t` toggle archived view, `d` delete. Group
  delete (`prepareDelete`) already scopes to the whole subtree.

## Design

### Store

- Add column `archived INTEGER NOT NULL DEFAULT 0` to the `groups` table, plus a
  migration entry (ignored-on-duplicate, matching existing migrations).
- `Group` struct gains `Archived bool`; `Groups()` selects and scans it.
- `SetGroupArchived(path string, archived bool) error`: in a single transaction,
  set `archived` on the group and every descendant group (`name = path OR name
  LIKE path || '/%'`) and on every session in the subtree (same scoping as
  `SessionsInSubtree`). Empty path is rejected.
- Restore symmetry in `SetArchived(id, false)`: also clear `archived` on the
  session's ancestor groups, so a restored session always lands in a live group.
  (Archiving a single session leaves its group untouched, as today.)

### UI

- `refreshMsg` carries `archivedGroups map[string]bool`; the poller fills it from
  `Groups()`.
- `archiveSelected` / `restoreSelected` branch on `selectedRow().isGroup`: a group
  row calls `SetGroupArchived(path, true/false)`; a session row keeps `SetArchived`.
- `rebuildRows` filtering, driven by `archivedGroups`:
  - Active view: drop any path that is an archived group or a descendant of one.
  - Archived view: keep archived groups (even with zero sessions) in addition to
    the existing "groups holding archived sessions" rule.

### Tests

- Store: archive a nested group flips the whole subtree (group + subgroup +
  sessions); restore reverses it; restoring a lone session un-archives its
  ancestor groups.
- UI: `a` on a group row removes it from the active view and surfaces it under
  `t`; `u` there restores the whole subtree.

## Out of scope

Partial-subtree archive, and any change to delete or move behavior.
